package runtime_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

func testWarehouse() runtime.Warehouse {
	return runtime.Warehouse{
		CatalogURI:    "jdbc:postgresql://catalog-db:5432/iceberg",
		WarehousePath: "s3://ontoaxis-warehouse/iceberg",
		Namespace:     "oa",
		CatalogUser:   "iceberg_catalog",
	}
}

func sinkSecrets() map[string]string {
	return map[string]string{"iceberg_catalog_password": "iceberg_pass"}
}

func TestBuildSinkConfigGolden(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)

	config, err := runtime.BuildSinkConfig(cfg, names, testWarehouse(), sinkSecrets())
	if err != nil {
		t.Fatalf("BuildSinkConfig() error = %v", err)
	}

	want := map[string]any{
		"connector.class":                      "org.apache.iceberg.connect.IcebergSinkConnector",
		"topics":                               "oa.fa1ffa66.2ff3a64e.public.customers,oa.fa1ffa66.2ff3a64e.public.orders",
		"iceberg.catalog":                      "oa",
		"iceberg.catalog.type":                 "jdbc",
		"iceberg.catalog.uri":                  "jdbc:postgresql://catalog-db:5432/iceberg",
		"iceberg.catalog.warehouse":            "s3://ontoaxis-warehouse/iceberg/oa/019c0002-0000-7000-8000-000000000003",
		"iceberg.catalog.jdbc.user":            "iceberg_catalog",
		"iceberg.catalog.jdbc.password":        "iceberg_pass",
		"iceberg.tables.dynamic-enabled":       "false",
		"iceberg.tables":                       "public.customers,public.orders",
		"iceberg.tables.auto-create-enabled":   "true",
		"iceberg.tables.evolve-schema-enabled": "true",
		"key.converter":                        "org.apache.kafka.connect.json.JsonConverter",
		"value.converter":                      "org.apache.kafka.connect.json.JsonConverter",
		"key.converter.schemas.enable":         "false",
		"value.converter.schemas.enable":       "false",
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("BuildSinkConfig() = %#v\nwant %#v", config, want)
	}

	topics, ok := config["topics"].(string)
	if !ok || strings.Contains(topics, "public.public") {
		t.Fatalf("topics = %v, want deterministic topic list", config["topics"])
	}
}

func TestBuildSinkConfigDeterministic(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)

	first, err := runtime.BuildSinkConfig(cfg, names, testWarehouse(), sinkSecrets())
	if err != nil {
		t.Fatalf("BuildSinkConfig() error = %v", err)
	}
	second, err := runtime.BuildSinkConfig(cfg, names, testWarehouse(), sinkSecrets())
	if err != nil {
		t.Fatalf("BuildSinkConfig() second call error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("BuildSinkConfig() is not deterministic")
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
		t.Fatalf("sink config JSON is not byte-identical")
	}
}

func TestBuildSinkConfigPasswordFallback(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)

	config, err := runtime.BuildSinkConfig(cfg, names, testWarehouse(), map[string]string{"password": "fallback_pass"})
	if err != nil {
		t.Fatalf("BuildSinkConfig() error = %v", err)
	}
	if config["iceberg.catalog.jdbc.password"] != "fallback_pass" {
		t.Errorf("password = %v, want fallback from password key", config["iceberg.catalog.jdbc.password"])
	}

	config, err = runtime.BuildSinkConfig(cfg, names, testWarehouse(), map[string]string{
		"password":                 "fallback_pass",
		"iceberg_catalog_password": "dedicated_pass",
	})
	if err != nil {
		t.Fatalf("BuildSinkConfig() error = %v", err)
	}
	if config["iceberg.catalog.jdbc.password"] != "dedicated_pass" {
		t.Errorf("password = %v, want dedicated iceberg_catalog_password", config["iceberg.catalog.jdbc.password"])
	}
}

func TestBuildSinkConfigRejectsMissingPassword(t *testing.T) {
	t.Parallel()

	cfg := mustSyncConfig(t)
	names := mustNames(t)
	_, err := runtime.BuildSinkConfig(cfg, names, testWarehouse(), map[string]string{"username": "cdc_user"})
	if !errors.Is(err, runtime.ErrSecretInvalid) {
		t.Fatalf("BuildSinkConfig() error = %v, want %v", err, runtime.ErrSecretInvalid)
	}
	if strings.Contains(err.Error(), "cdc_user") {
		t.Fatalf("BuildSinkConfig() error leaks secret value: %v", err)
	}
}

func TestBuildSinkConfigOmitsCatalogUserWhenUnset(t *testing.T) {
	t.Parallel()

	warehouse := testWarehouse()
	warehouse.CatalogUser = ""

	config, err := runtime.BuildSinkConfig(mustSyncConfig(t), mustNames(t), warehouse, sinkSecrets())
	if err != nil {
		t.Fatalf("BuildSinkConfig() error = %v", err)
	}
	if _, ok := config["iceberg.catalog.jdbc.user"]; ok {
		t.Errorf("catalog user key must be omitted when not configured: %v", config["iceberg.catalog.jdbc.user"])
	}
}

func TestBuildSinkConfigRequiresWarehouse(t *testing.T) {
	t.Parallel()

	_, err := runtime.BuildSinkConfig(mustSyncConfig(t), mustNames(t), runtime.Warehouse{}, sinkSecrets())
	if !errors.Is(err, runtime.ErrRuntimeNotConfigured) {
		t.Fatalf("BuildSinkConfig() error = %v, want %v", err, runtime.ErrRuntimeNotConfigured)
	}
}

func TestBuildSinkConfigRejectsInvalidDatasetID(t *testing.T) {
	t.Parallel()

	// ParseSyncConfig rejects malformed dataset ids already; BuildSinkConfig must
	// still defend the warehouse path when a SyncConfig is constructed directly.
	cfg := mustSyncConfig(t)
	cfg.DatasetID = "not-a-uuid"

	if _, err := runtime.BuildSinkConfig(cfg, mustNames(t), testWarehouse(), sinkSecrets()); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
		t.Fatalf("BuildSinkConfig() error = %v, want %v", err, runtime.ErrInvalidSyncConfig)
	}
}

func TestWarehouseValidate(t *testing.T) {
	t.Parallel()

	valid := testWarehouse()
	tests := []struct {
		name    string
		mutate  func(runtime.Warehouse) runtime.Warehouse
		wantErr bool
	}{
		{name: "valid", mutate: func(w runtime.Warehouse) runtime.Warehouse { return w }},
		{name: "empty catalog uri", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.CatalogURI = ""; return w }, wantErr: true},
		{name: "catalog uri without scheme", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.CatalogURI = "catalog-db:5432"; return w }, wantErr: true},
		{name: "empty warehouse path", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.WarehousePath = ""; return w }, wantErr: true},
		{name: "parent traversal", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.WarehousePath = "s3://bucket/../etc"; return w }, wantErr: true},
		{name: "relative warehouse path", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.WarehousePath = "warehouse/iceberg"; return w }, wantErr: true},
		{name: "invalid namespace", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.Namespace = "Bad-Namespace"; return w }, wantErr: true},
		{name: "local warehouse path", mutate: func(w runtime.Warehouse) runtime.Warehouse { w.WarehousePath = "/data/warehouse"; return w }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.mutate(valid).Validate()
			if tt.wantErr && !errors.Is(err, runtime.ErrRuntimeNotConfigured) {
				t.Fatalf("Validate() error = %v, want %v", err, runtime.ErrRuntimeNotConfigured)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestWarehouseDatasetLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(runtime.Warehouse) runtime.Warehouse
		want   string
	}{
		{
			name:   "uri warehouse",
			mutate: func(w runtime.Warehouse) runtime.Warehouse { return w },
			want:   "s3://ontoaxis-warehouse/iceberg/oa/" + testDatasetID,
		},
		{
			name: "trailing slash",
			mutate: func(w runtime.Warehouse) runtime.Warehouse {
				w.WarehousePath = "s3://ontoaxis-warehouse/iceberg/"
				return w
			},
			want: "s3://ontoaxis-warehouse/iceberg/oa/" + testDatasetID,
		},
		{
			name:   "local warehouse",
			mutate: func(w runtime.Warehouse) runtime.Warehouse { w.WarehousePath = "/data/warehouse"; return w },
			want:   "/data/warehouse/oa/" + testDatasetID,
		},
		{
			name:   "default namespace",
			mutate: func(w runtime.Warehouse) runtime.Warehouse { w.Namespace = ""; return w },
			want:   "s3://ontoaxis-warehouse/iceberg/oa/" + testDatasetID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			location, err := tt.mutate(testWarehouse()).DatasetLocation(testDatasetID)
			if err != nil {
				t.Fatalf("DatasetLocation() error = %v", err)
			}
			if location != tt.want {
				t.Fatalf("DatasetLocation() = %q, want %q", location, tt.want)
			}
		})
	}

	if _, err := testWarehouse().DatasetLocation("not-a-uuid"); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
		t.Fatalf("DatasetLocation(invalid) error = %v, want %v", err, runtime.ErrInvalidSyncConfig)
	}
}

func TestRedactConfig(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"connector.class":               "io.debezium.connector.postgresql.PostgresConnector",
		"database.password":             "plain-text-password",
		"iceberg.catalog.jdbc.password": "iceberg-password",
		"api_token":                     "token-value",
		"client_secret":                 "secret-value",
		"ssl.private_key":               "key-value",
		"database.user":                 "cdc_user",
	}
	redacted := runtime.RedactConfig(input)
	for _, key := range []string{"database.password", "iceberg.catalog.jdbc.password", "api_token", "client_secret", "ssl.private_key"} {
		if got := redacted[key]; got != "[REDACTED]" {
			t.Errorf("RedactConfig()[%q] = %v, want [REDACTED]", key, got)
		}
	}
	if redacted["database.user"] != "cdc_user" {
		t.Errorf("RedactConfig() must not redact non-sensitive keys: %v", redacted["database.user"])
	}
	for _, secret := range []string{"plain-text-password", "token-value", "secret-value", "key-value"} {
		if strings.Contains(redacted["connector.class"].(string), secret) {
			t.Errorf("RedactConfig() leaked %q", secret)
		}
	}
	if input["database.password"] != "plain-text-password" {
		t.Errorf("RedactConfig() must not mutate the input map")
	}
	if runtime.RedactConfig(nil) != nil {
		t.Errorf("RedactConfig(nil) = non-nil")
	}
}

func TestSensitiveKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"password", "database.password", "DB_PASSWORD", "api_token", "client_secret", "ssl.private_key", "credentials"} {
		if !runtime.SensitiveKey(key) {
			t.Errorf("SensitiveKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"connector.class", "database.hostname", "topics", "database.user"} {
		if runtime.SensitiveKey(key) {
			t.Errorf("SensitiveKey(%q) = true, want false", key)
		}
	}
}
