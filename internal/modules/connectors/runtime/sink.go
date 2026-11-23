package runtime

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	// IcebergSinkConnectorClass is the Apache Iceberg Kafka Connect sink connector.
	IcebergSinkConnectorClass = "org.apache.iceberg.connect.IcebergSinkConnector"
	// DefaultWarehouseNamespace is the Iceberg catalog namespace used when the
	// deployment does not configure one.
	DefaultWarehouseNamespace = "oa"
	// DebeziumTransformClass unwraps the Debezium envelope ({before, after,
	// source, op}) into the business row. It is Debezium's own SMT rather than
	// Iceberg's DebeziumTransform: the latter also writes a "_cdc" metadata
	// column, which marks every record as a CDC event and requires the Iceberg
	// table to declare identifier fields. This runtime does not carry primary
	// key metadata yet, so the plain unwrap keeps inserts working and avoids
	// blocking commits. Deletes are dropped until identifier fields exist.
	DebeziumTransformClass = "io.debezium.transforms.ExtractNewRecordState"
	// RegexRouterClass rewrites record topic names in flight. The sink uses it
	// to strip the runtime topic prefix so Iceberg static table routing sees
	// "<schema>.<table>" as configured in iceberg.tables.
	RegexRouterClass = "org.apache.kafka.connect.transforms.RegexRouter"
	// sinkTransformChain rewrites the topic before table routing. The CDC
	// envelope is flattened by the source connector, not here.
	sinkTransformChain = "stripTopicPrefix"
	// redactedValue replaces every secret-bearing config value in redacted views.
	redactedValue = "[REDACTED]"
	// sinkRoutePattern captures "<schema>.<table>" from a runtime topic.
	sinkRoutePattern = `\.([a-z_][a-z0-9_]*)\.([a-z_][a-z0-9_]*)$`
)

// Warehouse locates the Iceberg JDBC catalog and the Dataset storage area. Only
// CatalogUser is non-secret; the catalog password is resolved per submit.
type Warehouse struct {
	// CatalogURI is the JDBC URI of the Iceberg catalog database.
	CatalogURI string
	// WarehousePath is the warehouse root that contains Dataset directories.
	WarehousePath string
	// Namespace is the Iceberg catalog namespace generated for this runtime.
	Namespace string
	// CatalogUser is the JDBC user for the Iceberg catalog (non-secret).
	CatalogUser string
}

// Validate checks warehouse wiring. Failures mean the runtime is not configured;
// they never carry secret material.
func (w Warehouse) Validate() error {
	if strings.TrimSpace(w.CatalogURI) == "" ||
		!strings.Contains(w.CatalogURI, "://") ||
		strings.ContainsAny(w.CatalogURI, " \t\r\n") {
		return fmt.Errorf("%w: iceberg catalog uri is missing or invalid", ErrRuntimeNotConfigured)
	}
	path := strings.TrimSpace(w.WarehousePath)
	if path == "" || strings.ContainsAny(path, " \t\r\n") {
		return fmt.Errorf("%w: iceberg warehouse path is missing", ErrRuntimeNotConfigured)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return fmt.Errorf("%w: iceberg warehouse path must not contain parent segments", ErrRuntimeNotConfigured)
		}
	}
	if !strings.Contains(path, "://") && !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: iceberg warehouse path must be absolute or a URI", ErrRuntimeNotConfigured)
	}
	namespace := w.namespace()
	if len(namespace) > maxIdentifierLength || !identifierRegexp.MatchString(namespace) {
		return fmt.Errorf("%w: iceberg namespace must match %s", ErrRuntimeNotConfigured, identifierPattern)
	}
	return nil
}

// DatasetNamespace returns the Iceberg namespace that isolates one Dataset.
// The shared warehouse root plus a per-Dataset namespace keeps table identity
// unique without giving every Dataset its own catalog, so Trino's JDBC catalog
// can resolve tables by "<warehouse>/<namespace>/<table>".
func DatasetNamespace(datasetID, schema string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(datasetID))
	if err != nil {
		return "", fmt.Errorf("%w: key %q must be a uuid", ErrInvalidSyncConfig, "dataset_id")
	}
	normalizedSchema := strings.TrimSpace(schema)
	if !identifierRegexp.MatchString(normalizedSchema) {
		return "", fmt.Errorf("%w: key %q must match %s", ErrInvalidSyncConfig, "schema", identifierPattern)
	}
	compact := strings.ReplaceAll(parsed.String(), "-", "")
	return "ds_" + compact[:8] + "_" + normalizedSchema, nil
}

// root returns the shared warehouse root. Tables live under
// "<root>/<dataset namespace>/<table>" so every Dataset is isolated by
// namespace while the catalog keeps a single warehouse location.
func (w Warehouse) root() (string, error) {
	if err := w.Validate(); err != nil {
		return "", err
	}
	return strings.TrimRight(w.WarehousePath, "/"), nil
}

// DatasetLocation returns the storage location of one Dataset under the
// warehouse root. It is advisory metadata for the Dataset record; the sink
// binds iceberg.catalog.warehouse to the shared root and isolates tables by
// namespace.
func (w Warehouse) DatasetLocation(datasetID string) (string, error) {
	if err := w.Validate(); err != nil {
		return "", err
	}
	parsed, err := uuid.Parse(strings.TrimSpace(datasetID))
	if err != nil {
		return "", fmt.Errorf("%w: key %q must be a uuid", ErrInvalidSyncConfig, "dataset_id")
	}
	root := strings.TrimRight(w.WarehousePath, "/")
	location := root + "/" + w.namespace() + "/" + parsed.String()
	if !strings.HasPrefix(location, root+"/") || strings.Contains(location, "..") {
		return "", fmt.Errorf("%w: dataset storage location escapes the warehouse root", ErrInvalidSyncConfig)
	}
	return location, nil
}

func (w Warehouse) namespace() string {
	if namespace := strings.TrimSpace(w.Namespace); namespace != "" {
		return namespace
	}
	return DefaultWarehouseNamespace
}

// BuildSinkConfig builds the Iceberg sink connector config for the source tables.
// It routes each runtime topic back to its "<schema>.<table>" Iceberg table and
// binds the sink to the configured Dataset directory. The catalog password comes
// from the resolved secret; every other value is non-secret config.
func BuildSinkConfig(cfg SyncConfig, names Names, warehouse Warehouse, secrets map[string]string) (map[string]any, error) {
	root, err := warehouse.root()
	if err != nil {
		return nil, err
	}
	namespace, err := DatasetNamespace(cfg.DatasetID, cfg.Schema)
	if err != nil {
		return nil, err
	}
	password := secrets[SecretKeyIcebergCatalogPassword]
	if password == "" {
		password = secrets[SecretKeyPassword]
	}
	if password == "" {
		return nil, fmt.Errorf("%w: secret is missing key %q", ErrSecretInvalid, SecretKeyIcebergCatalogPassword)
	}
	topics := names.Topics(cfg.Schema, cfg.Tables)
	if len(topics) == 0 {
		return nil, fmt.Errorf("%w: key %q must not be empty", ErrInvalidSyncConfig, "tables")
	}
	config := map[string]any{
		"connector.class":                      IcebergSinkConnectorClass,
		"topics":                               strings.Join(topics, ","),
		"iceberg.catalog":                      warehouse.namespace(),
		"iceberg.catalog.type":                 "jdbc",
		"iceberg.catalog.uri":                  strings.TrimSpace(warehouse.CatalogURI),
		"iceberg.catalog.warehouse":            root,
		"iceberg.catalog.jdbc.password":        password,
		"iceberg.tables.dynamic-enabled":       "false",
		"iceberg.tables":                       staticTables(cfg, namespace),
		"iceberg.tables.auto-create-enabled":   "true",
		"iceberg.tables.evolve-schema-enabled": "true",
		// A 30s default commit timeout is too tight under a loaded local catalog:
		// the first commit can land without offsets and the task then replays the
		// same records, producing duplicate rows. Give the coordinator room to
		// include offsets in the first commit.
		"iceberg.control.commit.timeout-ms": "120000",
		"key.converter":                     "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":      "false",
		"value.converter":                   "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable":    "false",
		// The source connector emits schemaless Debezium envelopes and prefixes
		// every topic with "<tenant>.<connector>". Unwrap the envelope and strip
		// that prefix, otherwise the sink either writes envelope columns or
		// never matches a table in iceberg.tables.
		"transforms":                              sinkTransformChain,
		"transforms.stripTopicPrefix.type":        RegexRouterClass,
		"transforms.stripTopicPrefix.regex":       sinkRouteRegexp(names),
		"transforms.stripTopicPrefix.replacement": namespace + ".$2",
	}
	if user := strings.TrimSpace(warehouse.CatalogUser); user != "" {
		config["iceberg.catalog.jdbc.user"] = user
	}
	return config, nil
}

// staticTables returns the explicit Iceberg table list for one connector. The
// 1.12 connector no longer accepts route-regex/route-replace; each connector is
// scoped to its own explicit table set instead of dynamic record routing.
func staticTables(cfg SyncConfig, namespace string) string {
	tables := make([]string, 0, len(cfg.Tables))
	for _, table := range cfg.Tables {
		tables = append(tables, namespace+"."+table)
	}
	return strings.Join(tables, ",")
}

// sinkRouteRegexp matches the "<schema>.<table>" suffix appended after the
// deterministic topic prefix.
func sinkRouteRegexp(names Names) string {
	return "^" + regexp.QuoteMeta(names.TopicPrefix) + sinkRoutePattern
}

// SensitiveKey reports whether a Kafka Connect config key carries a credential.
func SensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"password", "secret", "token", "credential", "private_key"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

// RedactConfig returns a copy of config with every credential-bearing value
// replaced. It is used for logs, errors, and status projections.
func RedactConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	redacted := make(map[string]any, len(config))
	for key, value := range config {
		if SensitiveKey(key) {
			redacted[key] = redactedValue
			continue
		}
		redacted[key] = value
	}
	return redacted
}
