// Package migrations owns the embedded DDL for the pipeline schema.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/db"
)

//go:embed *.sql
var files embed.FS

const initFile = "00001_pipeline_init.sql"

// Statements returns the DDL statements in file order. Statements are
// separated by a line containing only "-- +statement"; splitting on marker
// lines (instead of semicolons) keeps SQL functions and literals safe.
func Statements() ([]string, error) {
	raw, err := files.ReadFile(initFile)
	if err != nil {
		return nil, fmt.Errorf("read migration %s: %w", initFile, err)
	}
	return splitStatements(string(raw)), nil
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
