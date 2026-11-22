package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	connectorsruntime "github.com/klb3713/ontoaxis/internal/modules/connectors/runtime"
	ingestionruntime "github.com/klb3713/ontoaxis/internal/modules/ingestion/runtime"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/module"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

// syncStarterFunc adapts the ingestion service to the connectors SyncStarter
// port. It resolves the service lazily because modules are built before Migrate.
type syncStarterFunc func(ctx context.Context, tenantID, connectorID string) (string, error)

func (f syncStarterFunc) StartSync(ctx context.Context, tenantID, connectorID string) (string, error) {
	return f(ctx, tenantID, connectorID)
}

// BackgroundRuntime is a long-lived worker unit owned by the App. Start launches
// its goroutines bound to ctx; Stop signals them to exit. When the runtime also
// exposes Wait() the App blocks on it after Stop so shutdown is ordered before
// the database pool is closed.
type BackgroundRuntime interface {
	Start(ctx context.Context) error
	Stop()
}

// RuntimeAssembly is the full runtime composition produced by
// BuildRuntimeAssembly: the modules to register, the post-migration background
// runtimes, and the shutdown closers the App owns. PostMigrate is invoked after
// every module has migrated so it can build runtimes that depend on migrated
// stores (the ingestion dispatcher/reconciler).
type RuntimeAssembly struct {
	Modules     []module.Module
	Background  []BackgroundRuntime
	Closers     []func()
	PostMigrate func(ctx context.Context) ([]BackgroundRuntime, error)
}

// closeAll runs the registered shutdown closers in reverse order. It is safe to
// call on a zero-value assembly.
func (a RuntimeAssembly) closeAll() {
	for i := len(a.Closers) - 1; i >= 0; i-- {
		if a.Closers[i] != nil {
			a.Closers[i]()
		}
	}
}

// BuildRuntimeModules builds the modules registered by the process, including
// the platform authorization module and the wired business modules. It is a thin
// convenience wrapper over BuildRuntimeAssembly for callers that only need the
// module list; callers that must own the authorization/ingestion lifecycle
// (the App) must call BuildRuntimeAssembly directly.
func BuildRuntimeModules(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, tracer trace.Tracer, logger *slog.Logger) ([]module.Module, error) {
	assembly, err := BuildRuntimeAssembly(ctx, pool, cfg, tracer, logger)
	if err != nil {
		return nil, err
	}
	return assembly.Modules, nil
}

// BuildRuntimeAssembly is the production composition root. It fails fast on any
// invalid required configuration and never falls back to an unauthenticated or
// fail-open process:
//
//   - authorization: ONTOAXIS_AUTHZ_* is required; authz.New runs its startup
//     OpenFGA check and the resulting guard is injected into every business
//     module that exposes SetDecisionGuard.
//   - pipelines: ONTOAXIS_KESTRA_*/pipeline durations are parsed; invalid values
//     abort startup.
//   - connectors: ONTOAXIS_CONNECT_*/ICEBERG env is parsed; a nil runtime is
//     allowed (sync marked unavailable).
//   - search: ONTOAXIS_SEARCH_* is parsed; when enabled the backend and guard
//     must be present or startup fails.
//   - actions: construction is fail-closed; a broken execution engine aborts.
func BuildRuntimeAssembly(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, tracer trace.Tracer, logger *slog.Logger) (RuntimeAssembly, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Fail-fast environment parsing happens before any network call or module
	// construction so configuration errors surface immediately and cheaply.
	pipelineRuntime, err := pipelines.LoadRuntimeConfigFromEnv()
	if err != nil {
		return RuntimeAssembly{}, fmt.Errorf("load pipeline runtime config: %w", err)
	}
	connectRuntime, err := connectorsruntime.NewFromEnv()
	if err != nil {
		return RuntimeAssembly{}, fmt.Errorf("initialize connectors runtime: %w", err)
	}
	searchCfg, err := search.LoadConfigFromEnv()
	if err != nil {
		return RuntimeAssembly{}, fmt.Errorf("load search config: %w", err)
	}
	authzCfg, err := authz.LoadConfigFromEnv()
	if err != nil {
		return RuntimeAssembly{}, fmt.Errorf("load authorization config: %w", err)
	}
	authzCfg.SourceCommit = cfg.Commit

	// Authorization platform module. authz.New fails closed and performs its
	// startup store/model check, so a bad upstream never yields an allow-all guard.
	authorizer, err := authz.New(ctx, authz.Options{Config: authzCfg, Tracer: tracer})
	if err != nil {
		return RuntimeAssembly{}, fmt.Errorf("initialize authorization: %w", err)
	}
	assembly := RuntimeAssembly{
		Closers: []func(){func() { _ = authorizer.Close() }},
	}
	authzModule := authz.NewModule(authorizer)
	guard := authzModule.Guard()

	business := buildBusinessModules(pool, cfg, pipelines.WithRuntimeConfig(pipelineRuntime))

	// Actions module fails closed; refuse to start a process whose execution
	// engine could not be constructed (missing/invalid Temporal configuration).
	if err := business.actions.InitError(); err != nil {
		assembly.closeAll()
		return RuntimeAssembly{}, fmt.Errorf("initialize actions module: %w", err)
	}

	injectDecisionGuard(business, guard)
	if err := business.actions.RequireDecisionGuard(); err != nil {
		return RuntimeAssembly{}, err
	}
	business.actions.SetAuditSink(slogAuditSink{logger: logger})

	// Authoritative resource readers used by the guards to build tenant-scoped
	// facts. They read the module's service lazily because ontology only builds
	// its service graph during route registration.
	business.objects.SetObjectTypeReader(objectTypeFactReader{service: func() ontologyObjectType {
		svc := business.ontology.Service()
		if svc == nil {
			return nil
		}
		return svc
	}})
	business.ontology.SetDatasetReader(datasetFactReader{service: func() datasetGetter {
		svc := business.datasets.Service()
		if svc == nil {
			return nil
		}
		return svc
	}})
	business.ingestion.SetConnectorReader(connectorReader{service: func() connectorsGetter {
		svc := business.connectors.Service()
		if svc == nil {
			return nil
		}
		return svc
	}})

	// Connectors sync runtime. A nil runtime is allowed: the module still starts
	// and StartSync reports the adapter as unavailable.
	business.connectors.SetSyncRuntime(connectRuntime)

	// Search backend + guard wiring. When enabled, a missing backend or guard is
	// a fail-closed startup error rather than a silently 404 route.
	backend, err := newSearchBackend(searchCfg)
	if err != nil {
		assembly.closeAll()
		return RuntimeAssembly{}, fmt.Errorf("initialize search backend: %w", err)
	}
	business.objects.SetSearch(backend, searchCfg.Enabled)
	if err := business.objects.SearchStartupError(); err != nil {
		assembly.closeAll()
		return RuntimeAssembly{}, fmt.Errorf("search startup: %w", err)
	}

	// Authorization is registered first (a platform module, not part of the nine
	// business modules); the business modules keep their fixed order.
	assembly.Modules = append([]module.Module{authzModule}, business.list()...)
	assembly.Closers = append(assembly.Closers, business.actions.Close)

	// The ingestion dispatcher/reconciler needs the migrated ingestion store and
	// connectors service, so it is constructed after migrations. A nil snapshot
	// verifier is passed: jobs then stay running instead of being falsely marked
	// complete, which is documented as an integration gap (a real verifier is
	// supplied by the stage-2 search/sync integration).
	if connectRuntime != nil {
		jobRuntime := ingestionruntime.NewConnectorJobRuntime(connectRuntime)
		assembly.PostMigrate = func(ctx context.Context) ([]BackgroundRuntime, error) {
			ingestionRuntime, err := business.ingestion.NewRuntime(jobRuntime, nil, ingestionruntime.Options{Logger: logger})
			if err != nil {
				return nil, fmt.Errorf("build ingestion runtime: %w", err)
			}
			business.connectors.SetSyncStarter(syncStarterFunc(func(ctx context.Context, tenantID, connectorID string) (string, error) {
				svc := business.ingestion.Service()
				if svc == nil {
					return "", errors.New("ingestion service is not initialized")
				}
				return svc.StartSync(ctx, tenantID, connectorID)
			}))
			return []BackgroundRuntime{ingestionRuntime}, nil
		}
	}

	return assembly, nil
}

// injectDecisionGuard installs the authorization guard on every business module.
func injectDecisionGuard(b *businessModules, guard authz.DecisionGuard) {
	b.datasets.SetDecisionGuard(guard)
	b.tenancy.SetDecisionGuard(guard)
	b.connectors.SetDecisionGuard(guard)
	b.ingestion.SetDecisionGuard(guard)
	b.ontology.SetDecisionGuard(guard)
	b.objects.SetDecisionGuard(guard)
	b.pipelines.SetDecisionGuard(guard)
	b.query.SetDecisionGuard(guard)
	b.actions.SetDecisionGuard(guard)
}

// slogAuditSink records API audit projections as structured JSON log lines.
// A write failure is surfaced to the caller; the approval endpoint then fails
// closed before the side effect.
type slogAuditSink struct{ logger *slog.Logger }

func (s slogAuditSink) Emit(_ context.Context, event string, fields map[string]any) error {
	if s.logger == nil {
		return fmt.Errorf("audit logger is not configured")
	}
	args := make([]any, 0, len(fields)*2+2)
	args = append(args, "event", event)
	for key, value := range fields {
		args = append(args, key, value)
	}
	s.logger.Info("action.audit", args...)
	return nil
}
