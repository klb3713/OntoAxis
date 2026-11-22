package authz

import (
	"os"
	"strings"
	"testing"
)

func clearAuthzEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AUTH_DISABLED",
		"ONTOAXIS_AUTHZ_DISABLED",
		"ONTOAXIS_AUTHZ_FAIL_OPEN",
		EnvironmentOpenFGAURL,
		EnvironmentOpenFGAStore,
		EnvironmentOpenFGAModel,
		EnvironmentOpenFGAToken,
		EnvironmentOpenFGATime,
		EnvironmentDecisionTime,
		EnvironmentPolicyDir,
		EnvironmentAuditSink,
		EnvironmentSourceCommit,
	} {
		t.Setenv(key, "")
	}
}

func TestLoadConfigRequiresOpenFGA(t *testing.T) {
	clearAuthzEnv(t)
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), EnvironmentOpenFGAURL) {
		t.Fatalf("expected missing OpenFGA URL error, got %v", err)
	}
}

func TestLoadConfigValid(t *testing.T) {
	clearAuthzEnv(t)
	t.Setenv(EnvironmentOpenFGAURL, "http://127.0.0.1:18090")
	t.Setenv(EnvironmentOpenFGAStore, "store-test")
	t.Setenv(EnvironmentOpenFGAModel, "model-test")
	t.Setenv(EnvironmentOpenFGATime, "150ms")
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.OpenFGATimeout.Milliseconds() != 150 {
		t.Fatalf("unexpected timeout: %s", cfg.OpenFGATimeout)
	}
	if cfg.DecisionTimeout != DefaultDecisionTimeout {
		t.Fatalf("unexpected decision timeout: %s", cfg.DecisionTimeout)
	}
}

func TestLoadConfigRejectsFailOpenAndBadValues(t *testing.T) {
	clearAuthzEnv(t)
	t.Setenv(EnvironmentOpenFGAURL, "http://127.0.0.1:18090")
	t.Setenv(EnvironmentOpenFGAStore, "store-test")
	t.Setenv(EnvironmentOpenFGAModel, "model-test")
	for _, key := range []string{"AUTH_DISABLED", "ONTOAXIS_AUTHZ_DISABLED", "ONTOAXIS_AUTHZ_FAIL_OPEN"} {
		t.Run(key, func(t *testing.T) {
			clearAuthzEnv(t)
			t.Setenv(EnvironmentOpenFGAURL, "http://127.0.0.1:18090")
			t.Setenv(EnvironmentOpenFGAStore, "store-test")
			t.Setenv(EnvironmentOpenFGAModel, "model-test")
			t.Setenv(key, "true")
			if _, err := LoadConfigFromEnv(); err == nil {
				t.Fatalf("expected %s=true to be rejected", key)
			}
		})
	}
	clearAuthzEnv(t)
	t.Setenv(EnvironmentOpenFGAURL, "http://127.0.0.1:18090")
	t.Setenv(EnvironmentOpenFGAStore, "store-test")
	t.Setenv(EnvironmentOpenFGAModel, "model-test")
	t.Setenv(EnvironmentDecisionTime, "100ms")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("decision timeout shorter than OpenFGA timeout must be rejected")
	}
}

func TestLoadConfigPolicyDir(t *testing.T) {
	clearAuthzEnv(t)
	dir := t.TempDir()
	t.Setenv(EnvironmentOpenFGAURL, "http://127.0.0.1:18090")
	t.Setenv(EnvironmentOpenFGAStore, "store-test")
	t.Setenv(EnvironmentOpenFGAModel, "model-test")
	t.Setenv(EnvironmentPolicyDir, dir)
	if _, err := LoadConfigFromEnv(); err != nil {
		t.Fatalf("valid policy dir rejected: %v", err)
	}
	t.Setenv(EnvironmentPolicyDir, dir+"/missing")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("missing policy dir must be rejected")
	}
	_ = os.RemoveAll(dir)
}
