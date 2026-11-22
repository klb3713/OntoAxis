package main

import (
	"testing"
)

func clearWorkerEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
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
		"ONTOAXIS_DATABASE_URL",
	} {
		t.Setenv(key, "")
	}
}

func TestRunRejectsUnknownMode(t *testing.T) {
	clearWorkerEnv(t)
	if err := run([]string{"bogus"}); err == nil {
		t.Fatal("run accepted unknown mode")
	}
}

func TestRunRebuildRequiresTenant(t *testing.T) {
	clearWorkerEnv(t)
	if err := run([]string{"rebuild"}); err == nil {
		t.Fatal("run rebuild without --tenant succeeded")
	}
}

func TestRunRebuildRejectsInvalidTenant(t *testing.T) {
	clearWorkerEnv(t)
	if err := run([]string{"rebuild", "--tenant", "not-a-uuid"}); err == nil {
		t.Fatal("run accepted invalid tenant")
	}
}
