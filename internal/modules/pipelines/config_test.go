package pipelines

import (
	"testing"
	"time"
)

func TestLoadRuntimeConfigDefaultsAndOverrides(t *testing.T) {
	t.Setenv("ONTOAXIS_KESTRA_URL", "")
	t.Setenv("ONTOAXIS_KESTRA_TIMEOUT", "3s")
	t.Setenv("ONTOAXIS_PIPELINE_RECONCILE_THROTTLE", "500ms")
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "5")
	cfg, err := LoadRuntimeConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KestraTimeout != 3*time.Second || cfg.ReconcileThrottle != 500*time.Millisecond || cfg.RetryMaxAttempts != 5 {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.KestraURL != "" {
		t.Fatal("empty Kestra URL must stay fail-closed")
	}
}

func TestLoadRuntimeConfigRejectsInvalidValues(t *testing.T) {
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "0")
	if _, err := LoadRuntimeConfigFromEnv(); err == nil {
		t.Fatal("invalid retry attempts must fail")
	}
	t.Setenv("ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS", "3")
	t.Setenv("ONTOAXIS_KESTRA_TIMEOUT", "not-a-duration")
	if _, err := LoadRuntimeConfigFromEnv(); err == nil {
		t.Fatal("invalid duration must fail")
	}
}
