// Package migrations embeds the dataset catalog DDL. Statements are executed
// through platform/db.ExecStatements, so every statement must be idempotent.
package migrations

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Statements returns all DDL statements in filename order. Comments are
// removed by the caller-visible split because the embedded SQL intentionally
// avoids semicolons inside comments and string literals.
func Statements() ([]string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded dataset migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var statements []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		raw, err := files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read dataset migration %s: %w", entry.Name(), err)
		}
		statements = append(statements, splitStatements(string(raw))...)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("no dataset migration statements found")
	}
	return statements, nil
}

// MustStatements is a convenience for tests and module startup.
func MustStatements() []string {
	statements, err := Statements()
	if err != nil {
		panic(err)
	}
	return statements
}

func splitStatements(raw string) []string {
	parts := strings.Split(raw, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		statements = append(statements, part)
	}
	return statements
}
