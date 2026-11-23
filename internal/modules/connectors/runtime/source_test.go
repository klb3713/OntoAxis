package runtime_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

func mustSyncConfig(t *testing.T) runtime.SyncConfig {
	t.Helper()
	cfg, err := runtime.ParseSyncConfig(validSyncConfigMap())
	if err != nil {
		t.Fatalf("ParseSyncConfig() error = %v", err)
	}
	return cfg
}

func mustNames(t *testing.T) runtime.Names {
	t.Helper()
	names, err := runtime.NewNaming().NamesFor(testTenantA, testConnA)
	if err != nil {
		t.Fatalf("NamesFor() error = %v", err)
	}
	return names
}

func sourceSecrets() map[string]string {
	return map[string]string{"username": "cdc_user", "password": "cdc_pass"}
}

func TestBuildSourceConfigGolden(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)

	config, err := runtime.BuildSourceConfig(cfg, names, sourceSecrets())
	if err != nil {
		t.Fatalf("BuildSourceConfig() error = %v", err)
	}

	want := map[string]any{
		"connector.class":                                "io.debezium.connector.postgresql.PostgresConnector",
		"plugin.name":                                    "pgoutput",
		"topic.prefix":                                   "oa.fa1ffa66.2ff3a64e",
		"slot.name":                                      "oa_fa1ffa66_2ff3a64e_slot",
		"publication.name":                               "oa_fa1ffa66_2ff3a64e_pub",
		"table.include.list":                             "public.customers,public.orders",
		"database.hostname":                              "source-db.internal",
		"database.port":                                  5432,
		"database.dbname":                                "app",
		"database.user":                                  "cdc_user",
		"database.password":                              "cdc_pass",
		"snapshot.mode":                                  "initial",
		"decimal.handling.mode":                          "string",
		"tombstones.on.delete":                           "true",
		"publication.autocreate.mode":                    "filtered",
		"key.converter":                                  "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":                   "false",
		"value.converter":                                "org.apache.kafka.connect.json.JsonConverter",
		"value.converter.schemas.enable":                 "false",
		"transforms":                                     "unwrapDebezium",
		"transforms.unwrapDebezium.type":                 "io.debezium.transforms.ExtractNewRecordState",
		"transforms.unwrapDebezium.drop.tombstones":      "true",
		"transforms.unwrapDebezium.delete.handling.mode": "drop",
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("BuildSourceConfig() = %#v\nwant %#v", config, want)
	}

	if got := config["transforms.unwrapDebezium.type"]; got != "io.debezium.transforms.ExtractNewRecordState" {
		t.Fatalf("unwrap type = %v, want Debezium ExtractNewRecordState", got)
	}

	if _, ok := config["topic_prefix"]; ok {
		t.Error("source config must not expose a user-overridable topic_prefix")
	}
}

func TestBuildSourceConfigDeterministic(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)

	first, err := runtime.BuildSourceConfig(cfg, names, sourceSecrets())
	if err != nil {
		t.Fatalf("BuildSourceConfig() error = %v", err)
	}
	second, err := runtime.BuildSourceConfig(cfg, names, sourceSecrets())
	if err != nil {
		t.Fatalf("BuildSourceConfig() second call error = %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("BuildSourceConfig() is not deterministic: %#v != %#v", first, second)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("source config JSON is not byte-identical: %s != %s", firstJSON, secondJSON)
	}
}

func TestBuildSourceConfigExplicitSlotAndPublication(t *testing.T) {
	t.Parallel()

	raw := validSyncConfigMap()
	raw["slot_name"] = "custom_slot"
	raw["publication_name"] = "custom_publication"
	cfg, err := runtime.ParseSyncConfig(raw)
	if err != nil {
		t.Fatalf("ParseSyncConfig() error = %v", err)
	}

	config, err := runtime.BuildSourceConfig(cfg, mustNames(t), sourceSecrets())
	if err != nil {
		t.Fatalf("BuildSourceConfig() error = %v", err)
	}
	if config["slot.name"] != "custom_slot" {
		t.Errorf("slot.name = %v, want custom_slot", config["slot.name"])
	}
	if config["publication.name"] != "custom_publication" {
		t.Errorf("publication.name = %v, want custom_publication", config["publication.name"])
	}
}

func TestBuildSourceConfigRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)
	tests := []struct {
		name    string
		secrets map[string]string
	}{
		{name: "nil secrets", secrets: nil},
		{name: "empty secrets", secrets: map[string]string{}},
		{name: "missing password", secrets: map[string]string{"username": "cdc_user"}},
		{name: "missing username", secrets: map[string]string{"password": "cdc_pass"}},
		{name: "empty password", secrets: map[string]string{"username": "cdc_user", "password": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := runtime.BuildSourceConfig(cfg, names, tt.secrets)
			if !errors.Is(err, runtime.ErrSecretInvalid) {
				t.Fatalf("BuildSourceConfig() error = %v, want %v", err, runtime.ErrSecretInvalid)
			}
			if strings.Contains(err.Error(), "cdc_pass") {
				t.Fatalf("BuildSourceConfig() error leaks secret value: %v", err)
			}
		})
	}
}

func TestSourceTopics(t *testing.T) {
	t.Parallel()

	topics := runtime.SourceTopics(mustSyncConfig(t), mustNames(t))
	want := []string{
		"oa.fa1ffa66.2ff3a64e.public.customers",
		"oa.fa1ffa66.2ff3a64e.public.orders",
	}
	if !reflect.DeepEqual(topics, want) {
		t.Fatalf("SourceTopics() = %v, want %v", topics, want)
	}
}
