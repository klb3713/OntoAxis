package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ontologyapi "github.com/klb3713/ontoaxis/internal/modules/ontology/api"
	"github.com/klb3713/ontoaxis/internal/modules/tenancy"
	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/config"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
	"github.com/klb3713/ontoaxis/internal/platform/httpserver"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

// ModuleFactory builds the process modules once the shared pool exists.
type ModuleFactory func(*pgxpool.Pool) []module.Module

type App struct {
	cfg    config.Config
	logger *slog.Logger
	pool   *pgxpool.Pool
	server *httpserver.Server
}

// New constructs the platform, verifies module dependencies, runs migrations in
// deterministic order, and builds the HTTP server. It never registers business
// routes itself; modules own their own route registration.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger, factory ModuleFactory) (*App, error) {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.MigratePlatform(ctx, cfg.DatabaseURL); err != nil {
		pool.Close()
		return nil, err
	}
	var modules []module.Module
	if factory != nil {
		modules = factory(pool)
	}
	reg := module.NewRegistry()
	for _, m := range modules {
		if err := reg.Register(m); err != nil {
			pool.Close()
			return nil, err
		}
	}
	ordered, err := reg.Ordered()
	if err != nil {
		pool.Close()
		return nil, err
	}
	for _, m := range ordered {
		if err := m.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, fmt.Errorf("migrate module %s: %w", m.Name(), err)
		}
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
	})
	return &App{cfg: cfg, logger: logger, pool: pool, server: srv}, nil
}

func (a *App) Run(ctx context.Context) error {
	defer a.pool.Close()
	a.logger.Info("ontoaxis-core starting", "http_addr", a.cfg.HTTPAddr, "metrics_addr", a.cfg.MetricsAddr)
	return a.server.Run(ctx)
}

// Server exposes the HTTP server for tests.
func (a *App) Server() *httpserver.Server { return a.server }

// Pool exposes the pool for integration tests and readiness checks.
func (a *App) Pool() *pgxpool.Pool { return a.pool }

// ShutdownTimeout is kept here for tests that need the configured value.
func (a *App) ShutdownTimeout() time.Duration { return a.cfg.ShutdownTimeout }
