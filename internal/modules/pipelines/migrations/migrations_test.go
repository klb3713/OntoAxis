package migrations_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/migrations"
)

var (
	createPolicyPattern = regexp.MustCompile(`(?is)^CREATE POLICY\s+([A-Za-z0-9_"]+)`)
	dropPolicyPattern   = regexp.MustCompile(`(?is)^DROP POLICY IF EXISTS\s+([A-Za-z0-9_"]+)`)
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
		// 00002_pipeline_execution.sql: execution/run bookkeeping columns.
		"ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS execution_id text",
		"ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS failure_code text",
		"ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS attempt_count integer NOT NULL DEFAULT 0",
		"ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS last_reconciled_at timestamptz",
		"CREATE INDEX IF NOT EXISTS runs_tenant_status_started_idx",
		"CREATE TABLE IF NOT EXISTS pipeline.run_inputs",
		"CREATE TABLE IF NOT EXISTS pipeline.run_outputs",
		"CREATE TABLE IF NOT EXISTS pipeline.idempotency_keys",
		"REFERENCES pipeline.runs (id) ON DELETE CASCADE",
		"PRIMARY KEY (run_id, dataset_id)",
		"relation_catalog text NOT NULL",
		"relation_schema text NOT NULL",
		"relation_table text NOT NULL",
		"PRIMARY KEY (tenant_id, operation, key_hash)",
		"expires_at timestamptz NOT NULL",
		"CREATE INDEX IF NOT EXISTS run_outputs_run_idx",
		"CREATE INDEX IF NOT EXISTS idempotency_keys_expires_idx",
		// RLS is enabled and forced for every table added by 00002.
		"run_inputs_tenant_isolation",
		"run_inputs_platform_admin",
		"run_outputs_tenant_isolation",
		"run_outputs_platform_admin",
		"idempotency_keys_tenant_isolation",
		"idempotency_keys_platform_admin",
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

func TestStatementsLoadEveryMigrationFile(t *testing.T) {
	statements, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	joined := strings.Join(statements, "\n")

	// Both embedded migrations must be loaded, in filename order: the 00001
	// schema creation precedes the 00002 execution additions.
	initial := strings.Index(joined, "CREATE TABLE IF NOT EXISTS pipeline.pipelines")
	execution := strings.Index(joined, "CREATE TABLE IF NOT EXISTS pipeline.run_inputs")
	if initial < 0 {
		t.Fatal("00001_pipeline_init.sql statements are missing")
	}
	if execution < 0 {
		t.Fatal("00002_pipeline_execution.sql statements are missing")
	}
	if initial > execution {
		t.Fatal("migrations must be applied in filename order (00001 before 00002)")
	}
	// 00002 must alter the existing runs table instead of recreating it.
	if got := strings.Count(joined, "CREATE TABLE IF NOT EXISTS pipeline.runs "); got != 1 {
		t.Fatalf("pipeline.runs must be created exactly once, found %d CREATE TABLE statements", got)
	}
	for _, fragment := range []string{
		"ALTER TABLE pipeline.runs ADD COLUMN IF NOT EXISTS execution_id",
		"CREATE TABLE IF NOT EXISTS pipeline.idempotency_keys",
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("statements missing %q", fragment)
		}
	}
}

func TestStatementsAreIdempotentForDoubleApply(t *testing.T) {
	statements, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	droppedPolicies := make(map[string]bool)
	for index, raw := range statements {
		statement := strings.TrimSpace(raw)
		upper := strings.ToUpper(statement)
		switch {
		case strings.HasPrefix(upper, "CREATE TABLE"):
			if !strings.Contains(upper, "IF NOT EXISTS") {
				t.Errorf("statement %d: CREATE TABLE must be IF NOT EXISTS", index+1)
			}
		case strings.HasPrefix(upper, "CREATE INDEX"):
			if !strings.Contains(upper, "IF NOT EXISTS") {
				t.Errorf("statement %d: CREATE INDEX must be IF NOT EXISTS", index+1)
			}
		case strings.HasPrefix(upper, "ALTER TABLE") && strings.Contains(upper, "ADD COLUMN"):
			if !strings.Contains(upper, "IF NOT EXISTS") {
				t.Errorf("statement %d: ADD COLUMN must be IF NOT EXISTS", index+1)
			}
		case strings.HasPrefix(upper, "DROP POLICY"):
			match := dropPolicyPattern.FindStringSubmatch(statement)
			if match == nil {
				t.Errorf("statement %d: unparseable DROP POLICY: %q", index+1, statement)
				continue
			}
			droppedPolicies[strings.Trim(match[1], `"`)] = true
		case strings.HasPrefix(upper, "CREATE POLICY"):
			match := createPolicyPattern.FindStringSubmatch(statement)
			if match == nil {
				t.Errorf("statement %d: unparseable CREATE POLICY: %q", index+1, statement)
				continue
			}
			name := strings.Trim(match[1], `"`)
			if !droppedPolicies[name] {
				t.Errorf("statement %d: CREATE POLICY %q is not preceded by DROP POLICY IF EXISTS", index+1, name)
			}
		}
	}
}

func TestUpStatementsNeverDropExistingData(t *testing.T) {
	statements, err := migrations.Statements()
	if err != nil {
		t.Fatalf("Statements: %v", err)
	}
	joined := strings.ToUpper(strings.Join(statements, "\n"))
	for _, destructive := range []string{"DROP TABLE", "TRUNCATE", "DELETE FROM", "DROP COLUMN"} {
		if strings.Contains(joined, destructive) {
			t.Errorf("up migration must be additive only, found %q", destructive)
		}
	}
}

func TestDownStatementsRollBackInReverseOrder(t *testing.T) {
	down := migrations.DownStatements
	want := []string{
		"DROP TABLE IF EXISTS pipeline.idempotency_keys",
		"DROP TABLE IF EXISTS pipeline.run_outputs",
		"DROP TABLE IF EXISTS pipeline.run_inputs",
		"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS last_reconciled_at",
		"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS attempt_count",
		"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS failure_code",
		"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS execution_id",
	}
	if len(down) != len(want) {
		t.Fatalf("DownStatements length = %d, want %d", len(down), len(want))
	}
	for index, fragment := range want {
		if !strings.Contains(down[index], fragment) {
			t.Errorf("DownStatements[%d] = %q, want it to contain %q", index, down[index], fragment)
		}
	}
}
