package search

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

// Migrate creates the search schema and its tables. It is idempotent and safe
// to run on every process start.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("search migration: nil pool")
	}
	statements, err := migrationStatements()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("search migration: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("search migration: exec: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("search migration: commit: %w", err)
	}
	return nil
}

func migrationStatements() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("search migration: read dir: %w", err)
	}
	var statements []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("search migration: read %s: %w", entry.Name(), err)
		}
		statements = append(statements, splitSQLStatements(string(content))...)
	}
	return statements, nil
}

// splitSQLStatements strips goose up/down markers and splits on semicolons.
// The search migrations intentionally contain only simple DDL.
func splitSQLStatements(content string) []string {
	var lines []string
	inDown := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "-- +goose Down"):
			inDown = true
			continue
		case strings.HasPrefix(trimmed, "-- +goose Up"):
			continue
		case strings.HasPrefix(trimmed, "-- +goose"):
			continue
		}
		if inDown {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		lines = append(lines, line)
	}
	var statements []string
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
