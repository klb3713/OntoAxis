// Package pipelines is the Stage 1 vertical slice for Pipeline definitions,
// versions, run entry points and schedules. DAG execution and cron firing are
// delegated to Kestra; Stage 1 only provides the adapter boundary and a local
// in-memory implementation.
package pipelines

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/api"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	platformmodule "github.com/klb3713/ontoaxis/internal/platform/module"
)

// Service is re-exported at the module root for callers that follow the
// ARCHITECTURE naming pipelines.Service.
type Service = service.Service

// ScheduleService is re-exported at the module root.
type ScheduleService = service.ScheduleService

// Module implements platform Module for the pipelines domain.
type Module struct {
	pool      *pgxpool.Pool
	store     service.Repository
	pipelines service.Service
	schedules service.ScheduleService
	api       *api.API
}

// Option customises module wiring. Production passes only a pool and obtains
// the Postgres store plus the in-memory Kestra skeleton.
type Option func(*options)

type options struct {
	store           service.Repository
	adapter         kestra.Adapter
	datasetResolver service.DatasetResolver
	tenantResolver  api.TenantResolver
}

// WithStore overrides the repository (used by tests and integration harnesses).
func WithStore(store service.Repository) Option {
	return func(o *options) { o.store = store }
}

// WithKestraAdapter overrides the execution adapter.
func WithKestraAdapter(adapter kestra.Adapter) Option {
	return func(o *options) { o.adapter = adapter }
}

// WithDatasetResolver injects the datasets module consumer adapter.
func WithDatasetResolver(resolver service.DatasetResolver) Option {
	return func(o *options) { o.datasetResolver = resolver }
}

// WithTenantResolver injects the request tenant resolver.
func WithTenantResolver(resolver api.TenantResolver) Option {
	return func(o *options) { o.tenantResolver = resolver }
}

// New constructs the module. It does not open connections; the app owns the
// shared pool.
func New(pool *pgxpool.Pool, opts ...Option) *Module {
	o := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.store == nil && pool != nil {
		o.store = repo.NewPostgresStore(pool)
	}
	if o.adapter == nil {
		o.adapter = kestra.NewMemoryAdapter()
	}
	if o.tenantResolver == nil {
		o.tenantResolver = api.RejectAllTenantResolver{}
	}
	manager := service.New(o.store,
		service.WithKestraAdapter(o.adapter),
		service.WithDatasetResolver(o.datasetResolver),
	)
	return &Module{
		pool:      pool,
		store:     o.store,
		pipelines: manager,
		schedules: manager,
		api:       api.New(manager, manager, o.tenantResolver),
	}
}

// Name returns the module name.
func (m *Module) Name() string { return "pipelines" }

// Dependencies declares the public dataset service dependency.
func (m *Module) Dependencies() []string { return []string{"datasets"} }

// Migrate applies the idempotent pipeline schema DDL through db.ExecStatements.
func (m *Module) Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		pool = m.pool
	}
	if pool == nil {
		return fmt.Errorf("migrate pipelines: nil pool")
	}
	return migrations.Migrate(ctx, pool)
}

// RegisterRoutes mounts /api/v1/pipelines on the platform router.
func (m *Module) RegisterRoutes(r chi.Router) {
	m.api.Register(r)
}

// Ready checks repository connectivity.
func (m *Module) Ready(ctx context.Context) error {
	if m.store == nil {
		return fmt.Errorf("pipelines: repository is not configured")
	}
	return m.store.Ping(ctx)
}

// Services exposes both public service interfaces for future in-process
// callers without leaking the concrete Manager type.
func (m *Module) Services() (service.Service, service.ScheduleService) {
	return m.pipelines, m.schedules
}

var _ platformmodule.Module = (*Module)(nil)
