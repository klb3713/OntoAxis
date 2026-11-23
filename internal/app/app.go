package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"

	ontologyapi "github.com/klb3713/ontoaxis/internal/modules/ontology/api"
	"github.com/klb3713/ontoaxis/internal/modules/tenancy"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
	"github.com/klb3713/ontoaxis/internal/platform/httpserver"
	"github.com/klb3713/ontoaxis/internal/platform/module"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

// AssemblyFactory builds the runtime composition once the shared pool, tracing
// and logger exist. It returns the modules to register plus the post-migration
// background runtimes and shutdown closers the App must own.
type AssemblyFactory func(ctx context.Context, pool *pgxpool.Pool, tracer trace.Tracer, logger *slog.Logger, metrics *observability.Metrics) (RuntimeAssembly, error)

type App struct {
	cfg        config.Config
	logger     *slog.Logger
	pool       *pgxpool.Pool
	server     *httpserver.Server
	telemetry  *observability.Telemetry
	background []BackgroundRuntime
	closers    []func()
}

// New constructs the platform, verifies module dependencies, runs migrations in
// deterministic order, runs post-migration runtime builders, and builds the HTTP
// server. It never registers business routes itself; modules own their own route
// registration.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger, factory AssemblyFactory) (*App, error) {
	telemetry, err := observability.SetupTelemetry(ctx, observability.TelemetryConfig{
		ServiceName:    "ontoaxis-core",
		ServiceVersion: "0.1.0",
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTLPEndpoint,
		SampleRatio:    cfg.OTELSampleRatio,
	}, logger)
	if err != nil {
		return nil, err
	}
	metrics := observability.NewMetrics(prometheus.NewRegistry())
	metrics.SetBuildInfo("0.1.0", cfg.Commit)
	pool, err := db.OpenWithTracer(ctx, cfg.DatabaseURL, db.NewQueryTracer("postgres", telemetry.Tracer(), metrics))
	if err != nil {
		_ = telemetry.Shutdown(context.Background())
		return nil, err
	}
	if err := db.MigratePlatform(ctx, cfg.DatabaseURL); err != nil {
		pool.Close()
		_ = telemetry.Shutdown(context.Background())
		return nil, err
	}

	// fail unwinds everything acquired so far, including the runtime assembly
	// closers and post-migration background runtimes, then releases the pool.
	var assembly RuntimeAssembly
	fail := func(err error) (*App, error) {
		assembly.closeAll()
		pool.Close()
		_ = telemetry.Shutdown(context.Background())
		return nil, err
	}

	if factory != nil {
		assembly, err = factory(ctx, pool, telemetry.Tracer(), logger, metrics)
		if err != nil {
			return fail(err)
		}
	}

	reg := module.NewRegistry()
	for _, m := range assembly.Modules {
		if err := reg.Register(m); err != nil {
			return fail(err)
		}
	}
	ordered, err := reg.Ordered()
	if err != nil {
		return fail(err)
	}
	for _, m := range ordered {
		if err := m.Migrate(ctx, pool); err != nil {
			return fail(fmt.Errorf("migrate module %s: %w", m.Name(), err))
		}
	}

	// Post-migration builders construct runtimes that depend on migrated stores
	// (the ingestion dispatcher/reconciler). They are started in Run.
	if assembly.PostMigrate != nil {
		background, err := assembly.PostMigrate(ctx)
		if err != nil {
			return fail(err)
		}
		assembly.Background = append(assembly.Background, background...)
	}

	var verifier authn.Verifier
	if cfg.OIDCJWKSURL != "" {
		verifier = authn.NewJWKSVerifier(cfg.OIDCIssuer, cfg.OIDCAudience, authn.NewRemoteKeySource(cfg.OIDCJWKSURL))
	}
	authenticator := authn.NewEdgeAuthenticator(authn.Config{
		TrustGateway: cfg.TrustGateway,
		Issuer:       cfg.OIDCIssuer,
		Audience:     cfg.OIDCAudience,
	}, verifier)
	authenticate := func(next http.Handler) http.Handler {
		return authenticator.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc, ok := authn.FromContext(r.Context())
			if !ok {
				httperr.Write(w, r, httperr.Unauthorized("tenant context is required"))
				return
			}
			r.Header.Set("X-Ontoaxis-Tenant-Id", tc.TenantID.String())
			r.Header.Set("X-Ontoaxis-User-Id", tc.UserID.String())
			routeCtx := tenancy.WithTenantID(r.Context(), tc.TenantID)
			routeCtx = ontologyapi.WithTenantID(routeCtx, tc.TenantID.String())
			next.ServeHTTP(w, r.WithContext(routeCtx))
		}))
	}
	srv := httpserver.New(httpserver.Options{
		ServiceName:     "ontoaxis-core",
		Version:         "0.1.0",
		HTTPAddr:        cfg.HTTPAddr,
		MetricsAddr:     cfg.MetricsAddr,
		ShutdownTimeout: cfg.ShutdownTimeout,
		Logger:          logger,
		DB:              pool,
		Modules:         ordered,
		Authenticate:    authenticate,
		EdgeAuth:        authenticator.EdgeAuthHandler(),
		Metrics:         metrics,
		Tracer:          telemetry.Tracer(),
		Commit:          cfg.Commit,
	})
	return &App{
		cfg:        cfg,
		logger:     logger,
		pool:       pool,
		server:     srv,
		telemetry:  telemetry,
		background: assembly.Background,
		closers:    assembly.Closers,
	}, nil
}

// Run starts the background runtimes, serves until ctx is cancelled, then stops
// the background runtimes, runs shutdown closers, flushes telemetry and closes
// the pool in that order.
func (a *App) Run(ctx context.Context) error {
	started := make([]BackgroundRuntime, 0, len(a.background))
	for _, bg := range a.background {
		if err := bg.Start(ctx); err != nil {
			a.stopBackground(started)
			a.shutdown()
			return fmt.Errorf("start background runtime: %w", err)
		}
		started = append(started, bg)
	}
	defer func() {
		a.stopBackground(started)
		a.shutdown()
	}()
	a.logger.Info("ontoaxis-core starting", "http_addr", a.cfg.HTTPAddr, "metrics_addr", a.cfg.MetricsAddr)
	return a.server.Run(ctx)
}

// stopBackground signals every started background runtime in reverse order and
// waits for those that expose Wait so workers exit before the pool closes.
func (a *App) stopBackground(started []BackgroundRuntime) {
	for i := len(started) - 1; i >= 0; i-- {
		started[i].Stop()
		if waiter, ok := started[i].(interface{ Wait() }); ok {
			waiter.Wait()
		}
	}
}

// shutdown runs the assembly closers in reverse order, flushes telemetry and
// closes the pool.
func (a *App) shutdown() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		if a.closers[i] != nil {
			a.closers[i]()
		}
	}
	_ = a.telemetry.Shutdown(context.Background())
	a.pool.Close()
}

// Server exposes the HTTP server for tests.
func (a *App) Server() *httpserver.Server { return a.server }

// Pool exposes the pool for integration tests and readiness checks.
func (a *App) Pool() *pgxpool.Pool { return a.pool }

// ShutdownTimeout is kept here for tests that need the configured value.
func (a *App) ShutdownTimeout() time.Duration { return a.cfg.ShutdownTimeout }
