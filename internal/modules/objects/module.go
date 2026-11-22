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
	"github.com/klb3713/ontoaxis/internal/platform/authz"
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
	pool             *pgxpool.Pool
	service          service.Service
	guard            authz.DecisionGuard
	objectTypeReader api.ObjectTypeReader
	linkReader       api.LinkReader
	search           api.SearchBackend
	searchEnabled    bool
}

func New(pool *pgxpool.Pool, opts ...service.Option) *Module {
	m := &Module{pool: pool}
	if pool != nil {
		m.service = service.New(repo.NewOutbox(pool), opts...)
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

func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) { m.guard = guard }

func (m *Module) SetObjectTypeReader(reader api.ObjectTypeReader) { m.objectTypeReader = reader }

func (m *Module) SetLinkReader(reader api.LinkReader) { m.linkReader = reader }

// SetSearch wires the object search backend and the authorization guard used by
// the search route. When enabled is false the search route is not registered.
func (m *Module) SetSearch(backend api.SearchBackend, enabled bool) {
	m.search = backend
	m.searchEnabled = enabled
}

// SearchStartupError reports a fail-closed misconfiguration of the search route.
// It returns nil when search is disabled or fully configured.
func (m *Module) SearchStartupError() error {
	if m == nil || !m.searchEnabled {
		return nil
	}
	if m.search == nil {
		return fmt.Errorf("objects: search is enabled but no search backend is configured")
	}
	if m.guard == nil {
		return fmt.Errorf("objects: search is enabled but no authorization guard is configured")
	}
	return nil
}

func (m *Module) RegisterRoutes(r chi.Router) {
	if r == nil || m.service == nil {
		return
	}
	r.Route("/api/v1/objects", api.NewHandler(m.service,
		api.WithDecisionGuard(m.guard),
		api.WithObjectTypeReader(m.objectTypeReader),
		api.WithLinkReader(m.linkReader),
		api.WithSearch(m.search, m.guard, m.searchEnabled),
	).Register)
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
