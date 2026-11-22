// Package pipelines is the Stage 1 vertical slice for Pipeline definitions,
// versions, run entry points and schedules. DAG execution and cron firing are
// delegated to Kestra; Stage 1 only provides the adapter boundary and a local
// in-memory implementation.
package pipelines

import (
	"context"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/api"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/migrations"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	platformmodule "github.com/klb3713/ontoaxis/internal/platform/module"
)

// Service is re-exported at the module root for callers that follow the
// ARCHITECTURE naming pipelines.Service.
type Service = service.Service

// ScheduleService is re-exported at the module root.
type ScheduleService = service.ScheduleService

// Module implements platform Module for the pipelines domain.
type Module struct {
	guard     authz.DecisionGuard
	pool      *pgxpool.Pool
	store     service.Repository
	pipelines service.Service
	schedules service.ScheduleService
	api       *api.API
	configErr error
}

// Option customises module wiring. Production passes only a pool and obtains
// the Postgres store plus the in-memory Kestra skeleton.
type Option func(*options)

type options struct {
	store           service.Repository
	adapter         kestra.Adapter
	datasetResolver service.DatasetResolver
	tenantResolver  api.TenantResolver
	runtime         *RuntimeConfig
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

// WithRuntimeConfig injects the pipeline runtime settings. When KestraURL is
// non-empty and no adapter was supplied, a fail-closed HTTP adapter is built.
func WithRuntimeConfig(cfg RuntimeConfig) Option {
	return func(o *options) { copied := cfg; o.runtime = &copied }
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
	// No Kestra adapter means publish/trigger fail closed with 503 (design
	// D12/D15). The memory adapter exists for tests and explicit local use via
	// WithKestraAdapter, never as a silent production fallback.
	if o.tenantResolver == nil {
		o.tenantResolver = api.RejectAllTenantResolver{}
	}
	var configErr error
	if o.adapter == nil && o.runtime != nil {
		if o.runtime.KestraURL != "" {
			adapter, adapterErr := kestra.NewHTTPAdapter(kestra.HTTPConfig{
				BaseURL:     o.runtime.KestraURL,
				Timeout:     o.runtime.KestraTimeout,
				APIToken:    o.runtime.KestraAPIToken,
				Environment: o.runtime.Environment,
				MaxAttempts: o.runtime.RetryMaxAttempts,
				BaseDelay:   100 * time.Millisecond,
				HTTPClient:  nil,
				Metrics:     o.runtime.Metrics,
			})
			if adapterErr != nil {
				configErr = fmt.Errorf("pipeline kestra adapter: %w", adapterErr)
			} else {
				o.adapter = adapter
			}
		}
	}
	serviceOpts := []service.Option{
		service.WithKestraAdapter(o.adapter),
		service.WithDatasetResolver(o.datasetResolver),
	}
	if o.runtime != nil {
		serviceOpts = append(serviceOpts,
			service.WithDBTCatalog(o.runtime.DBTCatalog),
			service.WithEnvironment(o.runtime.Environment),
			service.WithReconcileThrottle(o.runtime.ReconcileThrottle),
			service.WithRecoveryTimeout(o.runtime.RunRecoveryTimeout),
			service.WithMetrics(o.runtime.Metrics),
		)
	}
	manager := service.New(o.store, serviceOpts...)
	return &Module{
		pool:      pool,
		store:     o.store,
		pipelines: manager,
		schedules: manager,
		api:       api.New(manager, manager, o.tenantResolver),
		configErr: configErr,
	}
}

// Name returns the module name.
func (m *Module) SetDecisionGuard(guard authz.DecisionGuard) {
	m.guard = guard
	if m.api != nil {
		m.api.SetDecisionGuard(guard)
	}
}

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
	if m.configErr != nil {
		return m.configErr
	}
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
