package search

import (
	"strings"
	"testing"
	"time"
)

var searchConfigEnvKeys = []string{
	"ONTOAXIS_SEARCH_ENABLED",
	"ONTOAXIS_SEARCH_URL",
	"ONTOAXIS_SEARCH_API_KEY",
	"ONTOAXIS_SEARCH_TIMEOUT",
	"ONTOAXIS_SEARCH_INDEX_VERSION",
	"ONTOAXIS_SEARCH_SYNC_WINDOW",
	"ONTOAXIS_SEARCH_POLL_INTERVAL",
	"ONTOAXIS_SEARCH_CONSUMER_BATCH",
	"ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS",
	"ONTOAXIS_SEARCH_HTTP_ADDR",
	"ONTOAXIS_SEARCH_METRICS_ADDR",
}

func clearSearchConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range searchConfigEnvKeys {
		t.Setenv(key, "")
	}
}

func setValidSearchConfigEnv(t *testing.T) {
	t.Helper()
	clearSearchConfigEnv(t)
	t.Setenv("ONTOAXIS_SEARCH_ENABLED", "true")
	t.Setenv("ONTOAXIS_SEARCH_URL", "http://127.0.0.1:7700")
	t.Setenv("ONTOAXIS_SEARCH_API_KEY", "test-master-key")
}

func TestLoadConfigDefaultsWhenDisabled(t *testing.T) {
	clearSearchConfigEnv(t)
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv error = %v", err)
	}
	want := DefaultConfig()
	if cfg != want {
		t.Fatalf("config = %+v, want defaults %+v", cfg, want)
	}
	if cfg.Enabled {
		t.Fatal("search must default to disabled")
	}
}

func TestLoadConfigValidEnabled(t *testing.T) {
	setValidSearchConfigEnv(t)
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv error = %v", err)
	}
	if !cfg.Enabled || cfg.URL != "http://127.0.0.1:7700" || cfg.APIKey != "test-master-key" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Timeout != 1500*time.Millisecond || cfg.IndexVersion != "v1" || cfg.SyncWindow != 5*time.Second || cfg.PollInterval != 500*time.Millisecond {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.ConsumerBatch != 50 || cfg.MaxAttempts != 5 || cfg.HTTPAddr != ":8081" || cfg.MetricsAddr != ":9094" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "missing url", key: "ONTOAXIS_SEARCH_URL", value: "", want: "URL is required"},
		{name: "missing api key", key: "ONTOAXIS_SEARCH_API_KEY", value: "", want: "API_KEY is required"},
		{name: "bad url", key: "ONTOAXIS_SEARCH_URL", value: "not a url", want: "URL must use http or https"},
		{name: "bad scheme", key: "ONTOAXIS_SEARCH_URL", value: "ftp://example.com", want: "URL must use http or https"},
		{name: "zero timeout", key: "ONTOAXIS_SEARCH_TIMEOUT", value: "0s", want: "TIMEOUT"},
		{name: "huge timeout", key: "ONTOAXIS_SEARCH_TIMEOUT", value: "11s", want: "TIMEOUT"},
		{name: "bad version", key: "ONTOAXIS_SEARCH_INDEX_VERSION", value: "1", want: "INDEX_VERSION"},
		{name: "short sync", key: "ONTOAXIS_SEARCH_SYNC_WINDOW", value: "500ms", want: "SYNC_WINDOW"},
		{name: "long sync", key: "ONTOAXIS_SEARCH_SYNC_WINDOW", value: "61s", want: "SYNC_WINDOW"},
		{name: "short poll", key: "ONTOAXIS_SEARCH_POLL_INTERVAL", value: "50ms", want: "POLL_INTERVAL"},
		{name: "long poll", key: "ONTOAXIS_SEARCH_POLL_INTERVAL", value: "11s", want: "POLL_INTERVAL"},
		{name: "zero batch", key: "ONTOAXIS_SEARCH_CONSUMER_BATCH", value: "0", want: "CONSUMER_BATCH"},
		{name: "huge batch", key: "ONTOAXIS_SEARCH_CONSUMER_BATCH", value: "501", want: "CONSUMER_BATCH"},
		{name: "zero attempts", key: "ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS", value: "0", want: "MAX_ATTEMPTS"},
		{name: "huge attempts", key: "ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS", value: "21", want: "MAX_ATTEMPTS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidSearchConfigEnv(t)
			t.Setenv(tt.key, tt.value)
			_, err := LoadConfigFromEnv()
			if err == nil {
				t.Fatalf("LoadConfigFromEnv() = nil, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigParsesMalformedEnv(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"ONTOAXIS_SEARCH_ENABLED", "maybe"},
		{"ONTOAXIS_SEARCH_TIMEOUT", "soon"},
		{"ONTOAXIS_SEARCH_CONSUMER_BATCH", "many"},
	}
	for _, tt := range tests {
		setValidSearchConfigEnv(t)
		t.Setenv(tt.key, tt.value)
		if _, err := LoadConfigFromEnv(); err == nil {
			t.Fatalf("%s=%q accepted, want error", tt.key, tt.value)
		}
	}
}

func TestConfigValidateDirect(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled config without URL/API key validated")
	}
	cfg.URL = "https://search.example.com"
	cfg.APIKey = "key"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}
