package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"

	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

const shutdownTimeout = 15 * time.Second

// Mode selects what the search worker process does.
type Mode string

// Worker modes.
const (
	ModeConsume Mode = "consume"
	ModeRebuild Mode = "rebuild"
	ModeReplay  Mode = "replay"
	ModeMigrate Mode = "migrate"
)

// ParseMode validates a command-line mode.
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeConsume, ModeRebuild, ModeReplay, ModeMigrate:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("unknown search worker mode %q (want consume, rebuild, replay or migrate)", value)
	}
}

// Config combines search runtime configuration with worker-only settings.
// The command layer populates this from ONTOAXIS_SEARCH_* variables and flags.
type Config struct {
	Search       search.Config
	DatabaseURL  string
	Mode         Mode
	Tenant       uuid.UUID
	ObjectTypeID *uuid.UUID
	Verify       bool
	ReplayLimit  int
}

// Run executes the selected mode until it finishes or ctx is canceled.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	telemetry, err := observability.SetupTelemetry(ctx, observability.TelemetryConfig{
		ServiceName:  "ontoaxis-search",
		OTLPEndpoint: strings.TrimSpace(os.Getenv("ONTOAXIS_OTEL_ENDPOINT")),
	}, logger)
	if err != nil {
		return fmt.Errorf("initialize search worker telemetry: %w", err)
	}
	defer func() {
		if shutdownErr := telemetry.Shutdown(context.Background()); shutdownErr != nil {
			logger.Warn("search worker telemetry shutdown failed", "error", shutdownErr)
		}
	}()

	mode := cfg.Mode
	if mode == "" {
		mode = ModeConsume
	}
	switch mode {
	case ModeConsume:
		return runConsume(ctx, cfg, logger)
	case ModeRebuild:
		return runRebuild(ctx, cfg, logger)
	case ModeReplay:
		return runReplay(ctx, cfg, logger)
	case ModeMigrate:
		return runMigrate(ctx, cfg, logger)
	default:
		return fmt.Errorf("unknown search worker mode %q", mode)
	}
}

func openPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("ONTOAXIS_DATABASE_URL is required")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return pool, nil
}

func newClient(cfg search.Config) (*search.Client, error) {
	return search.NewClient(cfg.URL, cfg.APIKey,
		search.WithTimeout(cfg.Timeout),
		search.WithWaitTimeout(30*time.Second),
		search.WithTracer(otel.Tracer("ontoaxis-search")),
	)
}

func runMigrate(ctx context.Context, cfg Config, logger *slog.Logger) error {
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := search.Migrate(ctx, pool); err != nil {
		return err
	}
	logger.Info("search schema migrated")
	return nil
}

func runRebuild(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if !cfg.Search.Enabled {
		return fmt.Errorf("search is disabled; set ONTOAXIS_SEARCH_ENABLED=true")
	}
	if cfg.Tenant == uuid.Nil {
		return fmt.Errorf("--tenant is required for rebuild")
	}
	if err := cfg.Search.Validate(); err != nil {
		return err
	}
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := search.Migrate(ctx, pool); err != nil {
		return err
	}
	client, err := newClient(cfg.Search)
	if err != nil {
		return err
	}
	metrics := NewMetrics()
	rebuilder := search.NewRebuilder(pool, client, cfg.Search, metrics, logger, otel.Tracer("ontoaxis-search"))
	summary, err := rebuilder.Rebuild(ctx, cfg.Tenant, cfg.ObjectTypeID, cfg.Verify)
	if err != nil {
		return err
	}
	logger.Info("search rebuild complete",
		"tenant_id", summary.TenantID.String(),
		"index_uid", summary.IndexUID,
		"documents", summary.Documents,
		"duration", summary.Duration.String(),
		"verified", summary.Verified,
	)
	return nil
}

func runReplay(ctx context.Context, cfg Config, logger *slog.Logger) error {
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := search.Migrate(ctx, pool); err != nil {
		return err
	}
	metrics := NewMetrics()
	consumer := search.NewConsumer(pool, nil, nil, cfg.Search, metrics, logger, otel.Tracer("ontoaxis-search"))
	count, err := consumer.ReplayDead(ctx, cfg.ReplayLimit)
	if err != nil {
		return err
	}
	logger.Info("search dead-letter replay complete", "events", count)
	return nil
}

func runConsume(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if !cfg.Search.Enabled {
		return fmt.Errorf("search is disabled; set ONTOAXIS_SEARCH_ENABLED=true")
	}
	if err := cfg.Search.Validate(); err != nil {
		return err
	}
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := search.Migrate(ctx, pool); err != nil {
		return err
	}
	client, err := newClient(cfg.Search)
	if err != nil {
		return err
	}
	metrics := NewMetrics()
	consumer := search.NewConsumer(
		pool,
		search.ClientApplier{Client: client, WaitTimeout: 30 * time.Second},
		search.NewPostgresObjectReader(pool),
		cfg.Search,
		metrics,
		logger,
		otel.Tracer("ontoaxis-search"),
	)
	server := NewServer(pool, client, metrics)

	httpServer := &http.Server{
		Addr:              cfg.Search.HTTPAddr,
		Handler:           server.HTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpErr := make(chan error, 2)
	go func() {
		logger.Info("search worker http server listening", "addr", cfg.Search.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- fmt.Errorf("search worker http server: %w", err)
		}
	}()

	var metricsServer *http.Server
	if cfg.Search.MetricsAddr != "" && cfg.Search.MetricsAddr != cfg.Search.HTTPAddr {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry(), promhttp.HandlerOpts{}))
		metricsServer = &http.Server{
			Addr:              cfg.Search.MetricsAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			logger.Info("search worker metrics server listening", "addr", cfg.Search.MetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				httpErr <- fmt.Errorf("search worker metrics server: %w", err)
			}
		}()
	}

	consumeErr := make(chan error, 1)
	go func() {
		consumeErr <- consumeLoop(ctx, consumer, cfg.Search, logger)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("search worker shutdown signal received")
	case runErr = <-httpErr:
	case runErr = <-consumeErr:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown search worker http server: %w", err)
	}
	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil && runErr == nil {
			runErr = fmt.Errorf("shutdown search worker metrics server: %w", err)
		}
	}
	return runErr
}

func consumeLoop(ctx context.Context, consumer *search.Consumer, cfg search.Config, logger *slog.Logger) error {
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	batch := cfg.ConsumerBatch
	if batch <= 0 {
		batch = 50
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		processed, err := consumer.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("search consumer batch failed", "error", err)
		}
		if err == nil && processed >= batch {
			continue // drain a busy backlog without waiting for the poll tick
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
