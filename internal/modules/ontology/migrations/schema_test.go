package migrations

import (
	"strings"
	"testing"
)

func TestStatementsCreateOwnSchemaAndTenantIsolation(t *testing.T) {
	statements := Statements()
	if len(statements) == 0 {
		t.Fatal("Statements() must not be empty")
	}
	joined := strings.Join(statements, "\n")
	required := []string{
		"CREATE SCHEMA IF NOT EXISTS ontology",
		"CREATE TABLE IF NOT EXISTS ontology.object_types",
		"CREATE TABLE IF NOT EXISTS ontology.properties",
		"CREATE TABLE IF NOT EXISTS ontology.link_types",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"current_setting('app.tenant_id', true)::uuid",
		"current_setting('app.is_platform_admin', true) = 'true'",
		"UNIQUE (tenant_id, name)",
		"UNIQUE (tenant_id, object_type_id, name)",
		"one_to_one",
		"one_to_many",
		"many_to_many",
	}
	for _, fragment := range required {
		if !strings.Contains(joined, fragment) {
			t.Errorf("migration DDL missing %q", fragment)
		}
	}
	if strings.Contains(joined, "core.projects") || strings.Contains(joined, "core.project_resources") {
		t.Fatal("ontology migration must not reference tenancy project tables")
	}
}

func TestEveryStatementIsIdempotentOrPolicyReplacement(t *testing.T) {
	for index, statement := range Statements() {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			t.Fatalf("statement %d is empty", index)
		}
		if strings.HasPrefix(trimmed, "CREATE TABLE") || strings.HasPrefix(trimmed, "CREATE INDEX") || strings.HasPrefix(trimmed, "CREATE SCHEMA") {
			if !strings.Contains(trimmed, "IF NOT EXISTS") {
				t.Errorf("statement %d is not idempotent: %s", index, trimmed)
			}
		}
	}
}

func TestDownStatementsAreScopedToOntologySchema(t *testing.T) {
	down := DownStatements()
	if len(down) != 1 {
		t.Fatalf("DownStatements() length = %d, want 1", len(down))
	}
	if !strings.Contains(down[0], "DROP SCHEMA IF EXISTS ontology CASCADE") {
		t.Fatalf("unexpected down statement: %s", down[0])
	}
}
