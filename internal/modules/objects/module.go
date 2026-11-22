package objects

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/objects/api"
	"github.com/klb3713/ontoaxis/internal/modules/objects/repo"
	"github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	moduleName       = "objects"
	initMigrationSQL = "migrations/20261122180000_objects_init.sql"
)

// Module owns object instances and links in the object_data schema.
type Module struct {
	pool    *pgxpool.Pool
	service service.Service
}

func New(pool *pgxpool.Pool, opts ...service.Option) *Module {
	m := &Module{pool: pool}
	if pool != nil {
		m.service = service.New(repo.New(pool), opts...)
	}
	return m
}

func (m *Module) Name() string { return moduleName }

func (m *Module) Dependencies() []string { return []string{"ontology"} }

func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("objects: nil database pool")
	}
	statements, err := MigrationStatements()
	if err != nil {
		return err
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		return fmt.Errorf("objects: apply migrations: %w", err)
	}
	return nil
}

func (m *Module) RegisterRoutes(r chi.Router) {
	if r == nil || m.service == nil {
		return
	}
	r.Route("/api/v1/objects", api.NewHandler(m.service).Register)
}

func (m *Module) Ready(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("objects: database pool is not configured")
	}
	if err := m.pool.Ping(ctx); err != nil {
		return fmt.Errorf("objects: database ping failed: %w", err)
	}
	return nil
}

// Service exposes the object service for in-process consumers such as query.
func (m *Module) Service() service.Service {
	if m == nil {
		return nil
	}
	return m.service
}

// MigrationStatements parses the embedded goose Up section into executable
// statements without requiring goose at runtime.
func MigrationStatements() ([]string, error) {
	data, err := migrationFS.ReadFile(initMigrationSQL)
	if err != nil {
		return nil, fmt.Errorf("objects: read migration: %w", err)
	}
	up, err := migrationSection(string(data), "-- +goose Up", "-- +goose Down")
	if err != nil {
		return nil, err
	}
	statements := splitSQLStatements(up)
	if len(statements) == 0 {
		return nil, fmt.Errorf("objects: migration contains no statements")
	}
	return statements, nil
}

func migrationSection(content, startMarker, endMarker string) (string, error) {
	start := strings.Index(content, startMarker)
	if start < 0 {
		return "", fmt.Errorf("objects: migration is missing %q marker", startMarker)
	}
	rest := content[start+len(startMarker):]
	if end := strings.Index(rest, endMarker); end >= 0 {
		rest = rest[:end]
	}
	return rest, nil
}

func splitSQLStatements(section string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteString("\n")
	}
	var statements []string
	for _, part := range strings.Split(cleaned.String(), ";") {
		if stmt := strings.TrimSpace(part); stmt != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}
