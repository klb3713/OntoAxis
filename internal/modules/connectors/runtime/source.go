package runtime

import (
	"fmt"
	"strings"
)

const (
	// DebeziumPostgresConnectorClass is the Debezium PostgreSQL source connector.
	DebeziumPostgresConnectorClass = "io.debezium.connector.postgresql.PostgresConnector"
	// debeziumPluginName selects Postgres logical decoding via pgoutput.
	debeziumPluginName = "pgoutput"
	// debeziumSnapshotMode initial takes an initial consistent snapshot only.
	debeziumSnapshotMode = "initial"
	// debeziumDecimalHandlingMode keeps numerics lossless as strings.
	debeziumDecimalHandlingMode = "string"
	// debeziumPublicationAutocreateMode creates a filtered publication if absent.
	debeziumPublicationAutocreateMode = "filtered"
)

// BuildSourceConfig builds the Debezium PostgreSQL source connector config. The
// result is deterministic for the same SyncConfig, Names, and credentials; the
// password comes from the resolved secret and never from connector config.
func BuildSourceConfig(cfg SyncConfig, names Names, secrets map[string]string) (map[string]any, error) {
	username := strings.TrimSpace(secrets[SecretKeyUsername])
	if username == "" {
		return nil, fmt.Errorf("%w: secret is missing key %q", ErrSecretInvalid, SecretKeyUsername)
	}
	password := secrets[SecretKeyPassword]
	if password == "" {
		return nil, fmt.Errorf("%w: secret is missing key %q", ErrSecretInvalid, SecretKeyPassword)
	}
	slotName := cfg.SlotName
	if slotName == "" {
		slotName = names.DefaultSlotName()
	}
	publicationName := cfg.PublicationName
	if publicationName == "" {
		publicationName = names.DefaultPublicationName()
	}
	tables := make([]string, 0, len(cfg.Tables))
	for _, table := range cfg.Tables {
		tables = append(tables, cfg.Schema+"."+table)
	}
	return map[string]any{
		"connector.class":                DebeziumPostgresConnectorClass,
		"plugin.name":                    debeziumPluginName,
		"topic.prefix":                   names.TopicPrefix,
		"slot.name":                      slotName,
		"publication.name":               publicationName,
		"publication.autocreate.mode":    debeziumPublicationAutocreateMode,
		"table.include.list":             strings.Join(tables, ","),
		"database.hostname":              cfg.Host,
		"database.port":                  cfg.Port,
		"database.dbname":                cfg.Database,
		"database.user":                  username,
		"database.password":              password,
		"snapshot.mode":                  debeziumSnapshotMode,
		"tombstones.on.delete":           "true",
		"decimal.handling.mode":          debeziumDecimalHandlingMode,
		"key.converter":                  "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":   "false",
		"value.converter":                "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable": "false",
	}, nil
}

// SourceTopics returns the source topics produced for the configured tables.
func SourceTopics(cfg SyncConfig, names Names) []string {
	return names.Topics(cfg.Schema, cfg.Tables)
}
