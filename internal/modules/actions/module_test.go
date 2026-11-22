package actions

import "testing"

func TestMigrationStatements(t *testing.T) {
	statements, err := MigrationStatements()
	if err != nil {
		t.Fatalf("migration statements: %v", err)
	}
	if len(statements) < 4 {
		t.Fatalf("statements = %d, want >= 4", len(statements))
	}
}

func TestModuleNameAndDependencies(t *testing.T) {
	m := New(nil)
	if m.Name() != "actions" {
		t.Fatalf("name = %q", m.Name())
	}
	deps := m.Dependencies()
	if len(deps) != 1 || deps[0] != "ontology" {
		t.Fatalf("dependencies = %v", deps)
	}
}
