// Package datasets implements the OntoAxis dataset catalog module.
//
// The module owns the dataset schema and exposes a tenant-scoped HTTP API at
// /api/v1/datasets. It stores only Ontology-level metadata; physical table,
// branch and commit operations remain the responsibility of the Iceberg/Nessie
// control plane.
package datasets

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/api"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/repo"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

var _ module.Module = (*Module)(nil)

// Option customises module construction, primarily for tests.
type Option func(*Module)

// WithRepository injects a repository implementation instead of the default
// PostgreSQL store.
func WithRepository(repository service.Repository) Option {
	return func(m *Module) {
		m.repository = repository
	}
}

// WithTenantResolver injects the tenant resolver used by the HTTP layer.
func WithTenantResolver(resolver api.TenantResolver) Option {
	return func(m *Module) {
		m.tenantResolver = resolver
	}
}

// Module is the dataset catalog module implementation.
type Module struct {
	repository     service.Repository
	tenantResolver api.TenantResolver
	service        service.Service
	guard          authz.DecisionGuard
}

// New builds a dataset catalog module. Dependencies are injected by the
// composition root; tests can supply an in-memory repository.
func New(options ...Option) *Module {
	m := &Module{}
	for _, option := range options {
		option(m)
	}
	return m
}

// Name returns the stable module name used by the module registry.
func (m *Module) Name() string { return "datasets" }

// Dependencies declares the modules that must be initialised before datasets.
func (m *Module) Dependencies() []string { return []string{"tenancy"} }

// Migrate applies the dataset schema idempotently through platform/db. It also
// installs the PostgreSQL repository unless a repository was injected.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("datasets: database pool is required")
	}
	statements, err := migrations.Statements()
	if err != nil {
		return fmt.Errorf("datasets: load migrations: %w", err)
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		return fmt.Errorf("datasets: apply migrations: %w", err)
	}
	if m.repository == nil {
		m.repository = repo.NewPostgresStore(pool)
	}
	if m.service == nil {
		m.service = service.New(m.repository)
	}
	return nil
}

// SetDecisionGuard injects the authorization guard used by the HTTP layer.
func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) {
	m.guard = guard
}

// RegisterRoutes mounts the dataset catalog HTTP API.
func (m *Module) RegisterRoutes(r chi.Router) {
	if m.service == nil {
		m.service = service.New(m.repository)
	}
	handler := api.NewHandler(m.service, m.tenantResolver, api.WithDecisionGuard(m.guard))
	handler.RegisterRoutes(r)
}

// Service exposes the dataset service for in-process consumers such as
// pipeline dataset reference validation.
func (m *Module) Service() service.Service {
	if m == nil {
		return nil
	}
	return m.service
}

// Ready reports whether the module's storage is reachable.
func (m *Module) Ready(ctx context.Context) error {
	if m.repository == nil {
		return fmt.Errorf("datasets: repository is not configured")
	}
	return m.repository.Ping(ctx)
}
