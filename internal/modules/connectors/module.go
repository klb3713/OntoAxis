// Package connectors assembles the connectors domain, service, repo, API, and
// migrations into a platform module.
package connectors

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/api"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/repo"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

// Module implements the connectors platform capability.
type Module struct {
	mu    sync.RWMutex
	store service.Store
	svc   service.Service
	opts  []service.Option
}

// New builds a connectors module. Optional service adapters and test clocks can
// be supplied before Migrate/store initialization.
func New(opts ...service.Option) *Module {
	return &Module{opts: opts}
}

// NewWithStore builds a connectors module with an already constructed store.
// It is used by tests and by integrations that own store construction.
func NewWithStore(store service.Store, opts ...service.Option) *Module {
	return &Module{store: store, opts: opts}
}

// Name returns the stable module name.
func (m *Module) Name() string { return "connectors" }

// Dependencies returns the modules connectors depends on.
func (m *Module) Dependencies() []string { return []string{"tenancy"} }

// Migrate applies idempotent connector DDL and wires the postgres store.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("connectors: nil database pool")
	}
	if err := migrations.Up(ctx, pool); err != nil {
		return fmt.Errorf("connectors: apply migrations: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		m.store = repo.New(pool)
	}
	m.svc = service.NewService(m.store, m.opts...)
	return nil
}

// RegisterRoutes mounts /api/v1/connectors.
func (m *Module) RegisterRoutes(r chi.Router) {
	api.NewHandler(m.Service()).Register(r)
}

// Ready reports whether the module has a usable service.
func (m *Module) Ready(context.Context) error {
	if m.Service() == nil {
		return errors.New("connectors module is not initialized")
	}
	return nil
}

// Service exposes the connectors service for integration wiring. It returns nil
// until Migrate or NewWithStore has supplied a store.
func (m *Module) Service() service.Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc == nil && m.store != nil {
		m.svc = service.NewService(m.store, m.opts...)
	}
	return m.svc
}
