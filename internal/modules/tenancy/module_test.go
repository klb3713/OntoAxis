package tenancy

import (
	"context"
	"strings"
	"testing"
)

func TestModuleIdentityAndReadiness(t *testing.T) {
	m := New(nil)
	if m.Name() != "tenancy" {
		t.Fatalf("Name() = %q, want tenancy", m.Name())
	}
	if deps := m.Dependencies(); len(deps) != 0 {
		t.Fatalf("Dependencies() = %v, want empty", deps)
	}
	if err := m.Ready(context.Background()); err == nil {
		t.Fatal("Ready() with nil pool error = nil, want error")
	}
}

func TestMigrationStatementsAreIdempotentDDL(t *testing.T) {
	statements, err := MigrationStatements()
	if err != nil {
		t.Fatalf("MigrationStatements: %v", err)
	}
	if len(statements) < 10 {
		t.Fatalf("MigrationStatements returned %d statements, want at least 10", len(statements))
	}
	joined := strings.Join(statements, ";\n")
	for _, fragment := range []string{
		"CREATE SCHEMA IF NOT EXISTS core",
		"CREATE TABLE IF NOT EXISTS core.organizations",
		"CREATE TABLE IF NOT EXISTS core.workspaces",
		"CREATE TABLE IF NOT EXISTS core.memberships",
		"CREATE TABLE IF NOT EXISTS core.projects",
		"CREATE TABLE IF NOT EXISTS core.project_resources",
		"CREATE UNIQUE INDEX IF NOT EXISTS",
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("migration statements missing %q", fragment)
		}
	}
	for _, statement := range statements {
		if strings.Contains(statement, "+goose") {
			t.Fatalf("migration statement contains goose marker: %q", statement)
		}
	}
}

func TestMigrateRejectsNilPool(t *testing.T) {
	m := New(nil)
	if err := m.Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate(nil pool) error = nil, want error")
	}
}
