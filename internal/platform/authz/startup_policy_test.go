package authz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type failingStartupWriter struct{}

func (failingStartupWriter) Write([]byte) (int, error) { return 0, errors.New("audit sink down") }

func startupConfig() Config {
	return Config{
		OpenFGAApiURL:   "http://openfga.invalid",
		OpenFGAStoreID:  "store-test",
		OpenFGAModelID:  "model-test",
		OpenFGATimeout:  100 * time.Millisecond,
		DecisionTimeout: 200 * time.Millisecond,
		AuditSink:       "stdout",
		SourceCommit:    "test-commit",
	}
}

func TestNewEmitsPolicyChange(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New(context.Background(), Options{Config: startupConfig(), FGAClient: &fakeFGA{}, AuditWriter: &buf}); err != nil {
		t.Fatalf("New: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("audit lines = %d, want 1", len(lines))
	}
	var envelope struct {
		Authz map[string]any `json:"authz"`
	}
	if err := json.Unmarshal(lines[0], &envelope); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if envelope.Authz["event_type"] != "authz.policy_change" {
		t.Fatalf("event_type = %v", envelope.Authz["event_type"])
	}
	policy, _ := envelope.Authz["policy"].(map[string]any)
	if policy["model_digest"] == "" || policy["bundle_digest"] == "" {
		t.Fatalf("policy digests missing: %v", policy)
	}
}

func TestNewFailsWhenPolicyChangeAuditFails(t *testing.T) {
	_, err := New(context.Background(), Options{Config: startupConfig(), FGAClient: &fakeFGA{}, AuditWriter: &failingStartupWriter{}})
	if err == nil {
		t.Fatal("New must fail when policy_change audit write fails")
	}
}
