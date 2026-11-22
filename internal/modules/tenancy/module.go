package tenancy

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	moduleName       = "tenancy"
	initMigrationSQL = "migrations/20261122172700_tenancy_init_tables.sql"
)

// Module is the tenancy business module. It owns the core schema tables for
// organizations, workspaces, memberships, projects and project resources.
type Module struct {
	pool    *pgxpool.Pool
	service Service
	guard   authz.DecisionGuard
}

// ModuleOption configures optional module dependencies. Options are additive so
// existing call sites keep compiling unchanged.
type ModuleOption func(*Module)

// WithGuard injects the authorization decision guard used by the module HTTP
// API. It exists so tests and embedders can wire the guard without changing
// application wiring; a nil guard preserves the legacy allow-all behavior.
func WithGuard(guard authz.DecisionGuard) ModuleOption {
	return func(m *Module) { m.guard = guard }
}

// New creates the tenancy module using a shared pgx pool.
func New(pool *pgxpool.Pool, opts ...ModuleOption) *Module {
	m := &Module{pool: pool}
	if pool != nil {
		m.service = NewService(NewPostgresStore(pool))
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func newModuleWithService(pool *pgxpool.Pool, svc Service, opts ...ModuleOption) *Module {
	m := &Module{pool: pool, service: svc}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Name returns the stable module name used by the registry.
func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) { m.guard = guard }

func (m *Module) Name() string { return moduleName }

// Dependencies returns no business-module dependencies. Tenancy is the root of
// the module dependency graph.
func (m *Module) Dependencies() []string { return nil }

// Migrate applies the embedded, idempotent core-schema DDL through the shared
// platform helper. It never opens its own database connection.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("tenancy: nil database pool")
	}
	statements, err := MigrationStatements()
	if err != nil {
		return err
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		return fmt.Errorf("tenancy: apply migrations: %w", err)
	}
	return nil
}

// RegisterRoutes mounts the tenancy REST API below /api/v1/tenancy.
func (m *Module) RegisterRoutes(r chi.Router) {
	if r == nil || m.service == nil {
		return
	}
	a := newAPI(m.service, WithDecisionGuard(m.guard))
	r.Route("/api/v1/tenancy", a.register)
}

// Ready reports whether the module can serve traffic.
func (m *Module) Ready(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("tenancy: database pool is not configured")
	}
	if err := m.pool.Ping(ctx); err != nil {
		return fmt.Errorf("tenancy: database ping failed: %w", err)
	}
	return nil
}

// MigrationStatements returns the Up statements parsed from the embedded
// migration file. The parser deliberately keeps this module independent from
// goose at runtime while the file keeps goose-compatible markers for future
// platform tooling.
func MigrationStatements() ([]string, error) {
	data, err := migrationFS.ReadFile(initMigrationSQL)
	if err != nil {
		return nil, fmt.Errorf("tenancy: read migration: %w", err)
	}
	up, err := migrationSection(string(data), "-- +goose Up", "-- +goose Down")
	if err != nil {
		return nil, err
	}
	statements := splitSQLStatements(up)
	if len(statements) == 0 {
		return nil, fmt.Errorf("tenancy: migration contains no statements")
	}
	return statements, nil
}

func migrationSection(content, startMarker, endMarker string) (string, error) {
	start := strings.Index(content, startMarker)
	if start < 0 {
		return "", fmt.Errorf("tenancy: migration is missing %q marker", startMarker)
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
