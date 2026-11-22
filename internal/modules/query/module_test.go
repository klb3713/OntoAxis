package query

import (
	"context"
	"testing"
)

func TestMigrationStatements(t *testing.T) {
	statements, err := MigrationStatements()
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	if len(statements) < 5 {
		t.Fatalf("statements = %d, want >= 5", len(statements))
	}
}

func TestModuleContract(t *testing.T) {
	m := New(nil, "", "", 0, nil)
	if m.Name() != "query" {
		t.Fatalf("name = %q", m.Name())
	}
	deps := m.Dependencies()
	if len(deps) != 2 || deps[0] != "datasets" || deps[1] != "objects" {
		t.Fatalf("dependencies = %v", deps)
	}
	if err := m.Ready(context.Background()); err == nil {
		t.Fatal("expected ready error with nil pool")
	}
}
