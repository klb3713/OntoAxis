// Package ingestion assembles the ingestion domain, service, repo, API, and
// migrations into a platform module.
package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/api"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/repo"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

// Module implements the ingestion platform capability.
type Module struct {
	mu    sync.RWMutex
	store service.Store
	svc   service.Service
	opts  []service.Option
}

// New builds an ingestion module. Pass service.WithConnectorReader to inject the
// connectors service dependency before Migrate.
func New(opts ...service.Option) *Module {
	return &Module{opts: opts}
}

// NewWithStore builds an ingestion module with an already constructed store.
// It is used by tests and by integrations that own store construction.
func NewWithStore(store service.Store, opts ...service.Option) *Module {
	return &Module{store: store, opts: opts}
}

// Name returns the stable module name.
func (m *Module) Name() string { return "ingestion" }

// Dependencies returns the modules ingestion depends on.
func (m *Module) Dependencies() []string { return []string{"connectors"} }

// Migrate applies idempotent ingestion DDL and wires the postgres store.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("ingestion: nil database pool")
	}
	if err := migrations.Up(ctx, pool); err != nil {
		return fmt.Errorf("ingestion: apply migrations: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		m.store = repo.New(pool)
	}
	m.svc = service.NewService(m.store, m.opts...)
	return nil
}

// RegisterRoutes mounts /api/v1/ingestion routes.
func (m *Module) RegisterRoutes(r chi.Router) {
	api.NewHandler(m.Service()).Register(r)
}

// Ready reports whether the module has a usable service.
func (m *Module) Ready(context.Context) error {
	if m.Service() == nil {
		return errors.New("ingestion module is not initialized")
	}
	return nil
}

// Service exposes the ingestion service for integration wiring. It returns nil
// until Migrate or NewWithStore has supplied a store.
func (m *Module) Service() service.Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc == nil && m.store != nil {
		m.svc = service.NewService(m.store, m.opts...)
	}
	return m.svc
}
