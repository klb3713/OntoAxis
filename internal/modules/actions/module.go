package actions

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/actions/api"
	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

//go:embed migrations/*.sql
var migrationFS embed.FS

const moduleName = "actions"

type Module struct {
	pool    *pgxpool.Pool
	service service.Service
	guard   authz.DecisionGuard
	audit   api.AuditSink
	initErr error
	health  func(context.Context) error
	closeFn func()
}

func New(pool *pgxpool.Pool, opts ...service.Option) *Module {
	m := &Module{pool: pool}
	if pool == nil {
		return m
	}
	cfg, err := actiontemporal.LoadConfigFromEnv()
	if err != nil {
		m.initErr = fmt.Errorf("actions: execution engine configuration: %w", err)
		return m
	}
	var adapter service.Adapter
	if cfg.Mode == service.ExecutionModeNoop {
		adapter = service.NoopAdapter{}
	} else {
		temporalAdapter, err := actiontemporal.NewAdapter(cfg)
		if err != nil {
			m.initErr = fmt.Errorf("actions: connect execution engine: %w", err)
			return m
		}
		adapter = temporalAdapter
		m.health = temporalAdapter.Health
		m.closeFn = temporalAdapter.Close
	}
	m.service = service.New(repo.New(pool), adapter, opts...)
	return m
}

// NewWithAdapter builds the module around an explicit execution adapter. It is
// used by tests and by the integration assembly when the adapter lifecycle is
// owned elsewhere.
func NewWithAdapter(pool *pgxpool.Pool, adapter service.Adapter, opts ...service.Option) *Module {
	m := &Module{pool: pool}
	if pool == nil || adapter == nil {
		m.initErr = fmt.Errorf("actions: database pool and execution adapter are required")
		return m
	}
	m.service = service.New(repo.New(pool), adapter, opts...)
	m.health = adapter.Health
	return m
}

// Close releases the execution engine client when the module owns one.
func (m *Module) Close() {
	if m.closeFn != nil {
		m.closeFn()
	}
}

// InitError reports a fail-closed construction error (missing engine config or
// unreachable Temporal). Readiness surfaces it to the process supervisor.
func (m *Module) InitError() error { return m.initErr }

func (m *Module) Name() string { return moduleName }

func (m *Module) Dependencies() []string { return []string{"ontology"} }

func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("actions: nil database pool")
	}
	statements, err := MigrationStatements()
	if err != nil {
		return err
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		return fmt.Errorf("actions: apply migrations: %w", err)
	}
	return nil
}

func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) { m.guard = guard }

// SetAuditSink injects the API-side audit sink. Production assembly must set
// one so approval decisions are fail-closed on audit failure.
func (m *Module) SetAuditSink(sink api.AuditSink) { m.audit = sink }

// RequireDecisionGuard is called by the runtime assembly after guard injection.
// A module that would serve business routes without an authorization decision
// point must not start.
func (m *Module) RequireDecisionGuard() error {
	if m.guard == nil {
		return fmt.Errorf("actions: decision guard is required")
	}
	return nil
}

func (m *Module) RegisterRoutes(r chi.Router) {
	if r == nil || m.service == nil {
		return
	}
	r.Route("/api/v1/actions", api.NewHandler(m.service, api.WithDecisionGuard(m.guard), api.WithAuditSink(m.audit)).Register)
}

func (m *Module) Ready(ctx context.Context) error {
	if m.initErr != nil {
		return m.initErr
	}
	if m.service == nil {
		return fmt.Errorf("actions: service is not configured")
	}
	if m.pool == nil {
		return fmt.Errorf("actions: database pool is not configured")
	}
	if m.health != nil {
		if err := m.health(ctx); err != nil {
			return fmt.Errorf("actions: execution engine not ready: %w", err)
		}
	}
	if err := m.pool.Ping(ctx); err != nil {
		return fmt.Errorf("actions: database ping failed: %w", err)
	}
	return nil
}

func MigrationStatements() ([]string, error) {
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("actions: list migrations: %w", err)
	}
	sort.Strings(entries)
	var statements []string
	for _, name := range entries {
		if strings.HasSuffix(name, ".down.sql") {
			continue
		}
		data, err := migrationFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("actions: read migration %s: %w", name, err)
		}
		up, err := migrationSection(string(data), "-- +goose Up", "-- +goose Down")
		if err != nil {
			return nil, err
		}
		part := splitSQLStatements(up)
		if len(part) == 0 {
			return nil, fmt.Errorf("actions: migration %s contains no statements", name)
		}
		statements = append(statements, part...)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("actions: no migrations found")
	}
	return statements, nil
}

func migrationSection(content, startMarker, endMarker string) (string, error) {
	start := strings.Index(content, startMarker)
	if start < 0 {
		return "", fmt.Errorf("actions: migration is missing %q marker", startMarker)
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
