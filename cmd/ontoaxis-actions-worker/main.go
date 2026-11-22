// Command ontoaxis-actions-worker runs the Temporal actions worker. It hosts
// the ActionExecutionWorkflow, its activities, and the worker health, readiness
// and metrics endpoints. The core API binary only submits, cancels and
// describes executions; steps run here.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdkclient "go.temporal.io/sdk/client"

	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
	actionservice "github.com/klb3713/ontoaxis/internal/modules/actions/service"
	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
	actionworker "github.com/klb3713/ontoaxis/internal/modules/actions/worker"
	objectsrepo "github.com/klb3713/ontoaxis/internal/modules/objects/repo"
	objectservice "github.com/klb3713/ontoaxis/internal/modules/objects/service"
	"github.com/klb3713/ontoaxis/internal/platform/db"
)

const (
	envDatabaseURL  = "ONTOAXIS_DATABASE_URL"
	envHTTPAddr     = "ONTOAXIS_ACTIONS_WORKER_HTTP_ADDR"
	defaultHTTPAddr = ":18086"
	shutdownTimeout = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ontoaxis-actions-worker: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := actiontemporal.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	var store actiontemporal.ExecutionStore
	var effects actiontemporal.EffectApplier = actiontemporal.NoopEffectApplier{}
	if dsn := strings.TrimSpace(os.Getenv(envDatabaseURL)); dsn != "" {
		pool, openErr := db.Open(ctx, dsn)
		if openErr != nil {
			return fmt.Errorf("open database: %w", openErr)
		}
		defer pool.Close()
		store = repo.New(pool)
		effects = actiontemporal.ObjectEffectApplier{
			Objects:   objectservice.New(objectsrepo.NewOutbox(pool)),
			TestHooks: strings.TrimSpace(os.Getenv("ONTOAXIS_ACTIONS_TEST_HOOKS")) == "1",
		}
	} else {
		logger.Warn("database URL is not configured; execution state writes will fail", "env", envDatabaseURL)
	}
	if cfg.Mode == actionservice.ExecutionModeTemporal && store == nil {
		return fmt.Errorf("%w: %s is required in temporal mode", actionservice.ErrEngineUnavailable, envDatabaseURL)
	}

	activities := actiontemporal.NewActivities(store, effects)
	activities.Audit = func(_ context.Context, event actiontemporal.AuditEvent) {
		logger.Info("action.audit",
			"event", event.Name,
			"tenant_id", event.TenantID,
			"execution_id", event.ExecutionID,
			"step_index", event.StepIndex,
			"status", event.Status,
			"failure_code", event.FailureCode,
		)
	}

	var temporalClient sdkclient.Client
	if cfg.Mode == actionservice.ExecutionModeTemporal {
		temporalClient, err = actiontemporal.Dial(cfg)
		if err != nil {
			return err
		}
		defer temporalClient.Close()
	}

	metrics := actionworker.NewMetrics(prometheus.NewRegistry())
	actionsWorker := actionworker.New(cfg, temporalClient, activities,
		actionworker.WithLogger(logger),
		actionworker.WithMetrics(metrics),
	)

	httpAddr := strings.TrimSpace(os.Getenv(envHTTPAddr))
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}
	server := &http.Server{
		Addr:              httpAddr,
		Handler:           actionsWorker.HTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() {
		logger.Info("actions worker http server listening", "addr", httpAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	if cfg.Mode == actionservice.ExecutionModeTemporal {
		go func() {
			errCh <- actionsWorker.Run(ctx)
		}()
	} else {
		logger.Warn("actions worker is running in noop mode; only health and metrics endpoints are served")
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	return nil
}
