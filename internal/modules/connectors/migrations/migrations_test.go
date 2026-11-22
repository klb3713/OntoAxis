package migrations_test

import (
	"strings"
	"testing"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/migrations"
)

func TestStatementsAreIdempotentAndComplete(t *testing.T) {
	t.Parallel()
	joined := strings.Join(migrations.Statements, "\n")
	for _, required := range []string{
		"CREATE SCHEMA IF NOT EXISTS connector",
		"CREATE TABLE IF NOT EXISTS connector.connectors",
		"CREATE UNIQUE INDEX IF NOT EXISTS connectors_tenant_name_uq",
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"connectors_tenant_isolation",
		"connectors_platform_admin",
		"tenant_id = current_setting('app.tenant_id', true)::uuid",
		"connector_type IN ('postgres', 'mysql', 's3', 'rest_api', 'kafka')",
		"status IN ('active', 'paused', 'error')",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("migration statements missing %q", required)
		}
	}
	if strings.Contains(joined, "DROP TABLE") {
		t.Fatal("up statements must not drop tables")
	}
}

func TestDownStatementsReverseOrder(t *testing.T) {
	t.Parallel()
	if len(migrations.DownStatements) == 0 {
		t.Fatal("down statements must not be empty")
	}
	joined := strings.Join(migrations.DownStatements, "\n")
	if !strings.Contains(joined, "DROP TABLE IF EXISTS connector.connectors") {
		t.Fatal("down statements must drop the connector table")
	}
	if !strings.Contains(joined, "DROP SCHEMA IF EXISTS connector") {
		t.Fatal("down statements must drop the connector schema")
	}
	last := migrations.DownStatements[len(migrations.DownStatements)-1]
	if !strings.Contains(last, "DROP SCHEMA") {
		t.Fatalf("last down statement = %q, want schema drop", last)
	}
}
