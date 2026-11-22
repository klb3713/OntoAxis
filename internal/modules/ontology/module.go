// Package ontology implements the Ontology metadata module. It owns object
// type, property, link type and dataset binding management and never manages
// projects or project resources, which belong to the tenancy module.
package ontology

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/api"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/repo"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

// Module is the Ontology module lifecycle implementation.
type Module struct {
	pool     *pgxpool.Pool
	service  service.Service
	handler  *api.Handler
	guard    authz.DecisionGuard
	datasets api.DatasetReader
}

// New creates an uninitialized Ontology module. The shared database pool is
// supplied later by the module lifecycle through Migrate.
func New() *Module { return &Module{} }

// SetDecisionGuard injects the authorization decision guard used by the HTTP
// API. It must be called before RegisterRoutes; a nil guard preserves legacy
// behavior for tests that do not exercise authorization.
func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) { m.guard = guard }

// SetDatasetReader installs the tenant-scoped dataset loader used by the
// binding route to reject cross-tenant dataset IDs.
func (m *Module) SetDatasetReader(reader api.DatasetReader) { m.datasets = reader }

// Name returns the stable module name.
func (m *Module) Name() string { return "ontology" }

// Dependencies declares that ontology must be registered after tenancy.
func (m *Module) Dependencies() []string { return []string{"tenancy"} }

// Migrate applies the idempotent ontology DDL and captures the shared pool for
// the later route-registration phase.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("ontology: database pool is nil")
	}
	if err := db.ExecStatements(ctx, pool, migrations.Statements()...); err != nil {
		return fmt.Errorf("ontology: migrate: %w", err)
	}
	m.pool = pool
	m.service = nil
	m.handler = nil
	return nil
}

// RegisterRoutes builds the service graph once the pool has been captured and
// mounts the module routes on the shared router.
func (m *Module) RegisterRoutes(r chi.Router) {
	if m.handler == nil {
		if m.pool != nil {
			m.service = service.New(repo.New(m.pool))
		}
		m.handler = api.NewHandler(m.service, api.WithDecisionGuard(m.guard), api.WithDatasetReader(m.datasets))
	}
	m.handler.Register(r)
}

// Service exposes the ontology service for in-process consumers such as
// objects and actions. It is available after RegisterRoutes has built the
// service graph.
func (m *Module) Service() service.Service {
	if m == nil {
		return nil
	}
	return m.service
}

// Ready checks that the module has a usable database pool.
func (m *Module) Ready(ctx context.Context) error {
	if m.pool == nil {
		return errors.New("ontology: database pool is not initialized")
	}
	return m.pool.Ping(ctx)
}
