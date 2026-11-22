package runtime_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

const testDatasetID = "019c0002-0000-7000-8000-000000000003"

func validSyncConfigMap() map[string]any {
	return map[string]any{
		"host":       "source-db.internal",
		"port":       float64(5432),
		"database":   "app",
		"schema":     "public",
		"tables":     []string{"orders", "customers"},
		"dataset_id": testDatasetID,
	}
}

func TestParseSyncConfigValid(t *testing.T) {
	t.Parallel()

	cfg, err := runtime.ParseSyncConfig(validSyncConfigMap())
	if err != nil {
		t.Fatalf("ParseSyncConfig() error = %v", err)
	}
	if cfg.Host != "source-db.internal" {
		t.Errorf("Host = %q", cfg.Host)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", cfg.Port)
	}
	if cfg.Database != "app" {
		t.Errorf("Database = %q", cfg.Database)
	}
	if cfg.Schema != "public" {
		t.Errorf("Schema = %q, want public", cfg.Schema)
	}
	wantTables := []string{"customers", "orders"}
	if !reflect.DeepEqual(cfg.Tables, wantTables) {
		t.Errorf("Tables = %v, want %v (sorted, deduplicated)", cfg.Tables, wantTables)
	}
	if cfg.DatasetID != testDatasetID {
		t.Errorf("DatasetID = %q, want %q", cfg.DatasetID, testDatasetID)
	}
}

func TestParseSyncConfigDefaultsSchema(t *testing.T) {
	t.Parallel()

	raw := validSyncConfigMap()
	delete(raw, "schema")

	cfg, err := runtime.ParseSyncConfig(raw)
	if err != nil {
		t.Fatalf("ParseSyncConfig() error = %v", err)
	}
	if cfg.Schema != "public" {
		t.Errorf("Schema = %q, want default %q", cfg.Schema, "public")
	}
}

func TestParseSyncConfigOptionalSlotAndPublication(t *testing.T) {
	t.Parallel()

	raw := validSyncConfigMap()
	raw["slot_name"] = "custom_slot"
	raw["publication_name"] = "custom_publication"

	cfg, err := runtime.ParseSyncConfig(raw)
	if err != nil {
		t.Fatalf("ParseSyncConfig() error = %v", err)
	}
	if cfg.SlotName != "custom_slot" {
		t.Errorf("SlotName = %q", cfg.SlotName)
	}
	if cfg.PublicationName != "custom_publication" {
		t.Errorf("PublicationName = %q", cfg.PublicationName)
	}
}

func TestParseSyncConfigAcceptsSupportedPortTypes(t *testing.T) {
	t.Parallel()

	values := []any{5432, int32(5432), int64(5432), float32(5432), float64(5432), json.Number("5432")}
	for _, value := range values {
		raw := validSyncConfigMap()
		raw["port"] = value
		cfg, err := runtime.ParseSyncConfig(raw)
		if err != nil {
			t.Fatalf("ParseSyncConfig(port=%T) error = %v", value, err)
		}
		if cfg.Port != 5432 {
			t.Errorf("ParseSyncConfig(port=%T) Port = %d", value, cfg.Port)
		}
	}
}

func TestParseSyncConfigRejectsForbiddenTopicPrefix(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"topic_prefix", "topic.prefix", "topicPrefix", "TOPIC.PREFIX"} {
		raw := validSyncConfigMap()
		raw[key] = "attacker-controlled"
		if _, err := runtime.ParseSyncConfig(raw); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
			t.Errorf("ParseSyncConfig(%q) error = %v, want %v", key, err, runtime.ErrInvalidSyncConfig)
		}
	}
}

func TestParseSyncConfigRejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	raw := validSyncConfigMap()
	raw["jdbc_url"] = "jdbc:postgresql://attacker/db"
	if _, err := runtime.ParseSyncConfig(raw); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
		t.Fatalf("ParseSyncConfig() error = %v, want %v", err, runtime.ErrInvalidSyncConfig)
	}
}

func TestParseSyncConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	mutate := map[string]func(map[string]any){
		"missing host":          func(m map[string]any) { delete(m, "host") },
		"empty host":            func(m map[string]any) { m["host"] = "" },
		"host with whitespace":  func(m map[string]any) { m["host"] = "db host" },
		"missing port":          func(m map[string]any) { delete(m, "port") },
		"zero port":             func(m map[string]any) { m["port"] = 0 },
		"negative port":         func(m map[string]any) { m["port"] = -1 },
		"port over max":         func(m map[string]any) { m["port"] = 65536 },
		"fractional port":       func(m map[string]any) { m["port"] = 5432.5 },
		"port as string":        func(m map[string]any) { m["port"] = "5432" },
		"missing database":      func(m map[string]any) { delete(m, "database") },
		"invalid database":      func(m map[string]any) { m["database"] = "-app" },
		"invalid schema case":   func(m map[string]any) { m["schema"] = "Public" },
		"invalid schema char":   func(m map[string]any) { m["schema"] = "public schema" },
		"empty tables":          func(m map[string]any) { m["tables"] = []string{} },
		"missing tables":        func(m map[string]any) { delete(m, "tables") },
		"uppercase table":       func(m map[string]any) { m["tables"] = []string{"Orders"} },
		"table injection":       func(m map[string]any) { m["tables"] = []string{"orders;drop table x"} },
		"table wrong type":      func(m map[string]any) { m["tables"] = []any{1} },
		"missing dataset id":    func(m map[string]any) { delete(m, "dataset_id") },
		"invalid dataset id":    func(m map[string]any) { m["dataset_id"] = "not-a-uuid" },
		"dataset id wrong type": func(m map[string]any) { m["dataset_id"] = 42 },
	}
	for name, mutate := range mutate {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw := validSyncConfigMap()
			mutate(raw)
			if _, err := runtime.ParseSyncConfig(raw); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
				t.Fatalf("ParseSyncConfig() error = %v, want %v", err, runtime.ErrInvalidSyncConfig)
			}
		})
	}
}

func TestParseSyncConfigNilMap(t *testing.T) {
	t.Parallel()

	if _, err := runtime.ParseSyncConfig(nil); !errors.Is(err, runtime.ErrInvalidSyncConfig) {
		t.Fatalf("ParseSyncConfig(nil) error = %v, want %v", err, runtime.ErrInvalidSyncConfig)
	}
}
