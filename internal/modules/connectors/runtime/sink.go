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

// DatasetLocation returns the storage location of one Dataset under the
// warehouse root. It is the value bound to iceberg.catalog.warehouse.
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
	location, err := warehouse.DatasetLocation(cfg.DatasetID)
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
		"iceberg.catalog.warehouse":            location,
		"iceberg.catalog.jdbc.password":        password,
		"iceberg.tables.dynamic-enabled":       "false",
		"iceberg.tables":                       staticTables(cfg),
		"iceberg.tables.auto-create-enabled":   "true",
		"iceberg.tables.evolve-schema-enabled": "true",
		"key.converter":                        "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":         "false",
		"value.converter":                      "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable":       "false",
	}
	if user := strings.TrimSpace(warehouse.CatalogUser); user != "" {
		config["iceberg.catalog.jdbc.user"] = user
	}
	return config, nil
}

// staticTables returns the explicit Iceberg table list for one connector. The
// 1.12 connector no longer accepts route-regex/route-replace; each connector is
// scoped to its own explicit table set instead of dynamic record routing.
func staticTables(cfg SyncConfig) string {
	tables := make([]string, 0, len(cfg.Tables))
	for _, table := range cfg.Tables {
		tables = append(tables, cfg.Schema+"."+table)
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
