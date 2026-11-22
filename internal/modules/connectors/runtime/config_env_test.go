package runtime_test

import (
	"errors"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
)

func TestLoadConfigFromEnvUnconfigured(t *testing.T) {
	t.Setenv(runtime.EnvConnectURL, "")

	cfg, ok, err := runtime.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if ok {
		t.Fatalf("LoadConfigFromEnv() ok = true, want false when connect url is empty")
	}
	if cfg.ConnectURL != "" || cfg.Warehouse.CatalogURI != "" {
		t.Fatalf("LoadConfigFromEnv() = %+v, want zero config", cfg)
	}
}

func TestLoadConfigFromEnvConfigured(t *testing.T) {
	t.Setenv(runtime.EnvConnectURL, "http://127.0.0.1:18083")
	t.Setenv(runtime.EnvConnectTimeout, "30s")
	t.Setenv(runtime.EnvConnectRetryMax, "3")
	t.Setenv(runtime.EnvConnectVerifyMaxRows, "1000")
	t.Setenv(runtime.EnvIcebergCatalogURI, "jdbc:postgresql://catalog-db:5432/iceberg")
	t.Setenv(runtime.EnvIcebergWarehouse, "s3://ontoaxis-warehouse/iceberg")
	t.Setenv(runtime.EnvIcebergNamespace, "oa")
	t.Setenv(runtime.EnvIcebergCatalogUser, "iceberg_catalog")

	cfg, ok, err := runtime.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if !ok {
		t.Fatalf("LoadConfigFromEnv() ok = false, want true")
	}
	if cfg.ConnectURL != "http://127.0.0.1:18083" {
		t.Errorf("ConnectURL = %q", cfg.ConnectURL)
	}
	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("ConnectTimeout = %v, want 30s", cfg.ConnectTimeout)
	}
	if cfg.ConnectRetryMax != 3 {
		t.Errorf("ConnectRetryMax = %d, want 3", cfg.ConnectRetryMax)
	}
	if cfg.VerifyMaxRows != 1000 {
		t.Errorf("VerifyMaxRows = %d, want 1000", cfg.VerifyMaxRows)
	}
	if cfg.Warehouse.CatalogURI != "jdbc:postgresql://catalog-db:5432/iceberg" {
		t.Errorf("Warehouse.CatalogURI = %q", cfg.Warehouse.CatalogURI)
	}
	if cfg.Warehouse.WarehousePath != "s3://ontoaxis-warehouse/iceberg" {
		t.Errorf("Warehouse.WarehousePath = %q", cfg.Warehouse.WarehousePath)
	}
	if cfg.Warehouse.Namespace != "oa" {
		t.Errorf("Warehouse.Namespace = %q", cfg.Warehouse.Namespace)
	}
	if cfg.Warehouse.CatalogUser != "iceberg_catalog" {
		t.Errorf("Warehouse.CatalogUser = %q", cfg.Warehouse.CatalogUser)
	}
}

func TestLoadConfigFromEnvInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "timeout not a duration", env: runtime.EnvConnectTimeout, value: "soon"},
		{name: "timeout not positive", env: runtime.EnvConnectTimeout, value: "0s"},
		{name: "retry not a number", env: runtime.EnvConnectRetryMax, value: "many"},
		{name: "retry negative", env: runtime.EnvConnectRetryMax, value: "-1"},
		{name: "retry too large", env: runtime.EnvConnectRetryMax, value: "11"},
		{name: "verify rows not a number", env: runtime.EnvConnectVerifyMaxRows, value: "many"},
		{name: "verify rows negative", env: runtime.EnvConnectVerifyMaxRows, value: "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(runtime.EnvConnectURL, "http://127.0.0.1:18083")
			t.Setenv(tt.env, tt.value)
			if _, _, err := runtime.LoadConfigFromEnv(); err == nil {
				t.Fatalf("LoadConfigFromEnv() error = nil, want rejection for %s=%q", tt.env, tt.value)
			}
		})
	}
}

func TestNewFromEnvUnconfigured(t *testing.T) {
	t.Setenv(runtime.EnvConnectURL, "")

	rt, err := runtime.NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}
	if rt != nil {
		t.Fatalf("NewFromEnv() = %v, want nil runtime when unconfigured", rt)
	}
}

func TestNewFromEnvConfigured(t *testing.T) {
	t.Setenv(runtime.EnvConnectURL, "http://127.0.0.1:18083")
	t.Setenv(runtime.EnvConnectVerifyMaxRows, "1000")
	t.Setenv(runtime.EnvIcebergCatalogURI, "jdbc:postgresql://catalog-db:5432/iceberg")
	t.Setenv(runtime.EnvIcebergWarehouse, "s3://ontoaxis-warehouse/iceberg")
	t.Setenv(runtime.EnvIcebergNamespace, "oa")

	rt, err := runtime.NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}
	if rt == nil {
		t.Fatal("NewFromEnv() = nil, want configured runtime")
	}
	if got, want := rt.VerifyMaxRows(), 1000; got != want {
		t.Fatalf("VerifyMaxRows() = %d, want %d", got, want)
	}
}

func TestNewFromEnvRequiresWarehouse(t *testing.T) {
	t.Setenv(runtime.EnvConnectURL, "http://127.0.0.1:18083")
	for _, name := range []string{runtime.EnvIcebergCatalogURI, runtime.EnvIcebergWarehouse, runtime.EnvIcebergNamespace, runtime.EnvIcebergCatalogUser} {
		t.Setenv(name, "")
	}

	if _, err := runtime.NewFromEnv(); !errors.Is(err, runtime.ErrRuntimeNotConfigured) {
		t.Fatalf("NewFromEnv() error = %v, want %v", err, runtime.ErrRuntimeNotConfigured)
	}
}
