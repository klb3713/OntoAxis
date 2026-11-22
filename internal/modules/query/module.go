package query

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	objects "github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/modules/query/api"
	"github.com/klb3713/ontoaxis/internal/modules/query/repo"
	"github.com/klb3713/ontoaxis/internal/modules/query/service"
	"github.com/klb3713/ontoaxis/internal/modules/query/trino"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	moduleName       = "query"
	initMigrationSQL = "migrations/20261122190000_query_init.sql"
)

type Module struct {
	pool     *pgxpool.Pool
	service  service.Service
	executor service.Executor
	guard    authz.DecisionGuard
}

// New constructs the query module. An empty trinoURL keeps the saved-query API
// working while execution fails closed with 503 until an engine is configured.
func New(pool *pgxpool.Pool, trinoURL, trinoUser string, trinoTimeout time.Duration, objects objects.Service) *Module {
	m := &Module{pool: pool}
	if pool != nil {
		if strings.TrimSpace(trinoURL) != "" {
			m.executor = trino.New(trinoURL, trinoUser, trinoTimeout)
		}
		m.service = service.New(repo.New(pool), m.executor, objects)
	}
	return m
}

func (m *Module) Name() string { return moduleName }

func (m *Module) Dependencies() []string { return []string{"datasets", "objects"} }

func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("query: nil database pool")
	}
	statements, err := MigrationStatements()
	if err != nil {
		return err
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		return fmt.Errorf("query: apply migrations: %w", err)
	}
	return nil
}

func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) { m.guard = guard }

func (m *Module) RegisterRoutes(r chi.Router) {
	if r == nil || m.service == nil {
		return
	}
	r.Route("/api/v1/query", api.NewHandler(m.service, api.WithDecisionGuard(m.guard)).Register)
}

func (m *Module) Ready(ctx context.Context) error {
	if m.pool == nil {
		return fmt.Errorf("query: database pool is not configured")
	}
	return m.pool.Ping(ctx)
}

func MigrationStatements() ([]string, error) {
	data, err := migrationFS.ReadFile(initMigrationSQL)
	if err != nil {
		return nil, fmt.Errorf("query: read migration: %w", err)
	}
	up, err := migrationSection(string(data), "-- +goose Up", "-- +goose Down")
	if err != nil {
		return nil, err
	}
	statements := splitSQLStatements(up)
	if len(statements) == 0 {
		return nil, fmt.Errorf("query: migration contains no statements")
	}
	return statements, nil
}

func migrationSection(content, startMarker, endMarker string) (string, error) {
	start := strings.Index(content, startMarker)
	if start < 0 {
		return "", fmt.Errorf("query: migration is missing %q marker", startMarker)
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
