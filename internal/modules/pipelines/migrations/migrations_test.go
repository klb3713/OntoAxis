package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/migrations"
)

func TestStatementsCoverAllPipelineTables(t *testing.T) {
	statements, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	if len(statements) == 0 {
		t.Fatal("expected migration statements")
	}
	joined := strings.Join(statements, "\n")
	for _, fragment := range []string{
		"CREATE SCHEMA IF NOT EXISTS pipeline",
		"CREATE TABLE IF NOT EXISTS pipeline.pipelines",
		"CREATE TABLE IF NOT EXISTS pipeline.versions",
		"CREATE TABLE IF NOT EXISTS pipeline.runs",
		"CREATE TABLE IF NOT EXISTS pipeline.schedules",
		"UNIQUE (tenant_id, name)",
		"UNIQUE (tenant_id, pipeline_id, version)",
		"UNIQUE (tenant_id, pipeline_id, cron)",
		"pipeline_type IN ('batch', 'faster', 'incremental')",
		"status IN ('draft', 'published', 'archived')",
		"status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')",
		"started_at DESC",
		"pipelines_tenant_created_idx",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"current_setting('app.tenant_id', true)::uuid",
		"current_setting('app.is_platform_admin', true) = 'true'",
		"FOR ALL",
		"WITH CHECK",
	} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("migration statements missing %q", fragment)
		}
	}
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			t.Error("empty migration statement")
		}
		if strings.HasPrefix(statement, "--") {
			t.Errorf("comment leaked into statement: %q", statement)
		}
	}
}

func TestStatementsAreRepeatable(t *testing.T) {
	first, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	second, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("statement count changed: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("statement %d changed between calls", i+1)
		}
	}
}

func TestMigrateRequiresPool(t *testing.T) {
	if err := migrations.Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate with nil pool should fail")
	}
}
