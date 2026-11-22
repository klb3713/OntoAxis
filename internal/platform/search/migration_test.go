package search

import (
	"strings"
	"testing"
)

func TestMigrationStatementsIncludeSchemaAndIdempotentTables(t *testing.T) {
	statements, err := migrationStatements()
	if err != nil {
		t.Fatalf("migrationStatements error = %v", err)
	}
	if len(statements) < 3 {
		t.Fatalf("got %d statements, want at least 3: %#v", len(statements), statements)
	}
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"CREATE SCHEMA IF NOT EXISTS search",
		"CREATE TABLE IF NOT EXISTS search.outbox_attempt",
		"CREATE TABLE IF NOT EXISTS search.index_state",
		"event_id uuid PRIMARY KEY",
		"PRIMARY KEY (tenant_id, index_version)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("migrations missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "+goose Down") || strings.Contains(joined, "DROP TABLE") {
		t.Fatalf("down migration leaked into statements:\n%s", joined)
	}
}

func TestSplitSQLStatementsIgnoresCommentsAndDown(t *testing.T) {
	content := `-- +goose Up
-- a comment
CREATE TABLE a (id int);
CREATE TABLE b (id int);

-- +goose Down
DROP TABLE b;
DROP TABLE a;
`
	statements := splitSQLStatements(content)
	if len(statements) != 2 {
		t.Fatalf("statements = %#v, want 2", statements)
	}
	if statements[0] != "CREATE TABLE a (id int)" || statements[1] != "CREATE TABLE b (id int)" {
		t.Fatalf("statements = %#v", statements)
	}
}
