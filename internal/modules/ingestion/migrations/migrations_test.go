package migrations_test

import (
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/migrations"
)

func TestStatementsCoverSchema(t *testing.T) {
	t.Parallel()
	joined := strings.Join(migrations.Statements, "\n")
	for _, fragment := range []string{
		"CREATE SCHEMA IF NOT EXISTS ingestion",
		"CREATE TABLE IF NOT EXISTS ingestion.sync_jobs",
		"CREATE TABLE IF NOT EXISTS ingestion.sync_runs",
		"tenant_id uuid NOT NULL",
		"connector_id uuid NOT NULL",
		"sync_jobs_status_check",
		"sync_runs_status_check",
		"sync_jobs_tenant_connector_created_idx",
		"sync_runs_tenant_job_started_idx",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"sync_jobs_tenant_isolation",
		"sync_runs_tenant_isolation",
		"app.is_platform_admin",
	} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("ingestion migrations missing fragment %q", fragment)
		}
	}
}

func TestDownStatementsOrder(t *testing.T) {
	t.Parallel()
	if len(migrations.DownStatements) != 3 {
		t.Fatalf("expected 3 down statements, got %d", len(migrations.DownStatements))
	}
	if !strings.Contains(migrations.DownStatements[0], "ingestion.sync_runs") {
		t.Fatalf("first down statement must drop sync_runs: %q", migrations.DownStatements[0])
	}
	if !strings.Contains(migrations.DownStatements[1], "ingestion.sync_jobs") {
		t.Fatalf("second down statement must drop sync_jobs: %q", migrations.DownStatements[1])
	}
	if !strings.Contains(migrations.DownStatements[2], "SCHEMA") {
		t.Fatalf("last down statement must drop schema: %q", migrations.DownStatements[2])
	}
}
