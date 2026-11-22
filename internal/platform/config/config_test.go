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

func TestLoadActionTemporalConfig(t *testing.T) {
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://x")
	t.Setenv("ONTOAXIS_ACTIONS_EXECUTION_MODE", "noop")
	t.Setenv("ONTOAXIS_TEMPORAL_HOST_PORT", "temporal:7233")
	t.Setenv("ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT", "45m")
	t.Setenv("ONTOAXIS_ACTIONS_WORKFLOW_TIMEOUT", "10m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActionsExecutionMode != "noop" || cfg.TemporalHostPort != "temporal:7233" {
		t.Fatalf("unexpected actions config: %+v", cfg)
	}
	if cfg.ActionsApprovalTimeout.Minutes() != 45 || cfg.ActionsWorkflowTimeout.Minutes() != 10 {
		t.Fatalf("unexpected action timeouts: %+v", cfg)
	}
}

func TestLoadRejectsInvalidActionMode(t *testing.T) {
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://x")
	t.Setenv("ONTOAXIS_ACTIONS_EXECUTION_MODE", "bogus")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid action mode error")
	}
}

func TestLoadPipelineRuntimeConfig(t *testing.T) {
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://x")
	t.Setenv("ONTOAXIS_KESTRA_URL", "http://kestra:8080")
	t.Setenv("ONTOAXIS_KESTRA_TIMEOUT", "15s")
	t.Setenv("ONTOAXIS_PIPELINE_DBT_CATALOG", "trino")
	t.Setenv("ONTOAXIS_PIPELINE_RECONCILE_THROTTLE", "3s")
	t.Setenv("ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT", "90s")
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "4")
	t.Setenv("ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL", "12h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.KestraURL != "http://kestra:8080" || cfg.KestraTimeout != 15*time.Second || cfg.PipelineDBTCatalog != "trino" {
		t.Fatalf("unexpected pipeline config: %+v", cfg)
	}
	if cfg.PipelineReconcileThrottle != 3*time.Second || cfg.PipelineRunRecoveryTimeout != 90*time.Second || cfg.PipelineRetryMaxAttempts != 4 || cfg.PipelineIdempotencyTTL != 12*time.Hour {
		t.Fatalf("unexpected pipeline tunables: %+v", cfg)
	}
}

func TestLoadRejectsBadPipelineTunable(t *testing.T) {
	t.Setenv("ONTOAXIS_DATABASE_URL", "postgres://x")
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for zero retry attempts")
	}
}
