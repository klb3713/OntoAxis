package migrations

import (
	"strings"
	"testing"
)

func TestStatementsCoverDatasetSchema(t *testing.T) {
	statements, err := Statements()
	if err != nil {
		t.Fatalf("Statements() error = %v", err)
	}
	if len(statements) == 0 {
		t.Fatal("Statements() returned no statements")
	}
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"CREATE SCHEMA IF NOT EXISTS dataset",
		"CREATE TABLE IF NOT EXISTS dataset.datasets",
		"CREATE TABLE IF NOT EXISTS dataset.dataset_branches",
		"CREATE TABLE IF NOT EXISTS dataset.dataset_transactions",
		"CREATE TABLE IF NOT EXISTS dataset.dataset_views",
		"CREATE TABLE IF NOT EXISTS dataset.dataset_files",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"current_setting('app.tenant_id', true)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("migrations missing %q", want)
		}
	}
}

func TestStatementsAreIdempotent(t *testing.T) {
	statements, err := Statements()
	if err != nil {
		t.Fatalf("Statements() error = %v", err)
	}
	for _, statement := range statements {
		if strings.HasPrefix(statement, "CREATE TABLE") && !strings.Contains(statement, "IF NOT EXISTS") {
			t.Errorf("non-idempotent table statement: %q", statement)
		}
		if strings.HasPrefix(statement, "CREATE INDEX") && !strings.Contains(statement, "IF NOT EXISTS") {
			t.Errorf("non-idempotent index statement: %q", statement)
		}
		if strings.HasPrefix(statement, "CREATE POLICY") {
			// Policies are rebuilt after DROP POLICY IF EXISTS in the previous
			// statement, so CREATE POLICY itself is safe to retry.
			if !strings.Contains(statement, "tenant_id = current_setting('app.tenant_id', true)::uuid") &&
				!strings.Contains(statement, "current_setting('app.is_platform_admin', true) = 'true'") {
				t.Errorf("policy statement lacks expected predicate: %q", statement)
			}
		}
	}
}

func TestSplitStatements(t *testing.T) {
	raw := "SELECT 1;\n\n SELECT 2 ; "
	got := splitStatements(raw)
	if len(got) != 2 {
		t.Fatalf("splitStatements() len = %d, want 2: %#v", len(got), got)
	}
	if got[0] != "SELECT 1" || got[1] != "SELECT 2" {
		t.Fatalf("splitStatements() = %#v", got)
	}
}
