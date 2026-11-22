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
	ingestionruntime "github.com/klb3713/ontoaxis/internal/modules/ingestion/runtime"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

// Module implements the ingestion platform capability.
type Module struct {
	mu         sync.RWMutex
	guard      authz.DecisionGuard
	connectors api.ConnectorReader
	store      service.Store
	svc        service.Service
	opts       []service.Option
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
func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guard = guard
}

func (m *Module) SetConnectorReader(reader api.ConnectorReader) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectors = reader
	m.opts = append(m.opts, service.WithConnectorReader(reader))
	if m.store != nil {
		m.svc = service.NewService(m.store, m.opts...)
	}
}

func (m *Module) RegisterRoutes(r chi.Router) {
	m.mu.RLock()
	guard := m.guard
	connectors := m.connectors
	m.mu.RUnlock()
	api.NewHandler(m.Service(), api.WithDecisionGuard(guard), api.WithConnectorReader(connectors)).Register(r)
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

// NewRuntime builds the ingestion dispatcher/reconciler runtime over this
// module's store and configured connector reader. The application owns its
// lifecycle (Start/Stop) so a missing Kafka Connect profile leaves the module
// serving HTTP without background workers. jobRuntime and verifier are supplied
// by integration wiring; a nil verifier keeps jobs running rather than guessing
// completion.
func (m *Module) NewRuntime(jobRuntime ingestionruntime.JobRuntime, verifier ingestionruntime.SnapshotVerifier, opts ingestionruntime.Options) (*ingestionruntime.Runtime, error) {
	m.mu.RLock()
	store := m.store
	reader := m.connectors
	m.mu.RUnlock()
	if store == nil {
		return nil, fmt.Errorf("ingestion: runtime requires an initialized store")
	}
	runtimeStore, ok := store.(ingestionruntime.RuntimeStore)
	if !ok {
		return nil, fmt.Errorf("ingestion: store does not implement the runtime port")
	}
	if reader == nil {
		return nil, fmt.Errorf("ingestion: runtime requires the connectors module")
	}
	return ingestionruntime.NewRuntime(runtimeStore, reader, jobRuntime, verifier, opts)
}
