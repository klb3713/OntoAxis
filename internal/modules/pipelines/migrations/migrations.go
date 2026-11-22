// Package migrations owns the embedded DDL for the pipeline schema.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/db"
)

//go:embed *.sql
var files embed.FS

// Statements returns the DDL statements for every embedded *.sql migration in
// filename order (00001_pipeline_init.sql, 00002_pipeline_execution.sql, ...).
// Statements are separated by a line containing only "-- +statement"; splitting
// on marker lines (instead of semicolons) keeps SQL functions, dollar-quoted
// blocks and string literals safe.
func Statements() ([]string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded pipeline migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var statements []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		raw, err := files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read pipeline migration %s: %w", entry.Name(), err)
		}
		statements = append(statements, splitStatements(string(raw))...)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("no pipeline migration statements found")
	}
	return statements, nil
}

// DownStatements exists for explicit local rollback tooling and migration
// tests. Migrate never runs them: module migrations are cumulative and the
// design intentionally does not execute a reverse migration automatically
// (D13/D15). The statements only remove objects added by 00002 and are ordered
// to drop dependants before their prerequisites.
var DownStatements = []string{
	"DROP TABLE IF EXISTS pipeline.idempotency_keys",
	"DROP TABLE IF EXISTS pipeline.run_outputs",
	"DROP TABLE IF EXISTS pipeline.run_inputs",
	"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS last_reconciled_at",
	"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS attempt_count",
	"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS failure_code",
	"ALTER TABLE pipeline.runs DROP COLUMN IF EXISTS execution_id",
}

func splitStatements(raw string) []string {
	var statements []string
	var current strings.Builder
	flush := func() {
		statement := strings.TrimSpace(current.String())
		if statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "-- +statement" {
			flush()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()
	return statements
}

// Migrate applies the embedded DDL. Every statement is idempotent, so the
// migration is safe to run on every process start.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	statements, err := Statements()
	if err != nil {
		return err
	}
	if pool == nil {
		return fmt.Errorf("migrate pipeline schema: nil pool")
	}
	return db.ExecStatements(ctx, pool, statements...)
}
