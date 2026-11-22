package config

import (
	"testing"
	"time"
)

func TestLoadDefaultsAndEnvironment(t *testing.T) {
	t.Setenv("ONTOAXIS_CONFIG", "")
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("ONTOAXIS_HTTP_ADDR", ":18080")
	t.Setenv("ONTOAXIS_LOG_LEVEL", "debug")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":18080" || cfg.LogLevel != "debug" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.MetricsAddr != ":9090" || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("defaults not preserved: %+v", cfg)
	}
	if cfg.Environment != "local" || cfg.OTELSampleRatio != 1.0 {
		t.Fatalf("telemetry defaults not preserved: %+v", cfg)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("ONTOAXIS_CONFIG", "")
	t.Setenv("ONTOAXIS_DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing database URL error")
	}
}

func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("ONTOAXIS_CONFIG", "")
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://localhost/db")
	t.Setenv("ONTOAXIS_SHUTDOWN_TIMEOUT", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestLoadRejectsInvalidOTELSampleRatio(t *testing.T) {
	t.Setenv("ONTOAXIS_CONFIG", "")
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://localhost/db")
	t.Setenv("ONTOAXIS_OTEL_SAMPLE_RATIO", "2")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid sample ratio error")
	}
}
