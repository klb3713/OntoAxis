// Package worker runs the Temporal actions worker: it registers the execution
// workflow and its activities, and serves the worker-level health, readiness
// and metrics endpoints.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	sdkclient "go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	actionservice "github.com/klb3713/ontoaxis/internal/modules/actions/service"
	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
)

// HealthCheck verifies the execution engine is reachable. It is injectable so
// the readiness endpoint can be tested without a Temporal server.
type HealthCheck func(ctx context.Context) error

// Option customizes a Worker.
type Option func(*Worker)

// WithLogger sets the worker logger.
func WithLogger(logger *slog.Logger) Option {
	return func(w *Worker) {
		if logger != nil {
			w.logger = logger
		}
	}
}

// WithMetrics replaces the worker metrics (for example to share a registry).
func WithMetrics(metrics *Metrics) Option {
	return func(w *Worker) {
		if metrics != nil {
			w.metrics = metrics
		}
	}
}

// WithHealthCheck replaces the Temporal readiness check.
func WithHealthCheck(check HealthCheck) Option {
	return func(w *Worker) {
		w.health = check
	}
}

// Worker owns the Temporal worker registration and its HTTP endpoints.
type Worker struct {
	cfg        actiontemporal.Config
	client     sdkclient.Client
	activities *actiontemporal.Activities
	metrics    *Metrics
	logger     *slog.Logger
	health     HealthCheck
}

// New builds a worker. In noop mode the client may be nil; temporal mode
// requires a client.
func New(cfg actiontemporal.Config, client sdkclient.Client, activities *actiontemporal.Activities, opts ...Option) *Worker {
	worker := &Worker{
		cfg:        cfg,
		client:     client,
		activities: activities,
		metrics:    NewMetrics(nil),
		logger:     slog.Default(),
		health:     defaultHealthCheck(cfg, client),
	}
	for _, opt := range opts {
		opt(worker)
	}
	return worker
}

func defaultHealthCheck(cfg actiontemporal.Config, client sdkclient.Client) HealthCheck {
	return func(ctx context.Context) error {
		if cfg.Mode == actionservice.ExecutionModeNoop {
			return nil
		}
		if client == nil {
			return fmt.Errorf("%w: temporal client is not configured", actionservice.ErrEngineUnavailable)
		}
		if _, err := client.CheckHealth(ctx, &sdkclient.CheckHealthRequest{}); err != nil {
			return fmt.Errorf("%w: temporal health check: %v", actionservice.ErrEngineUnavailable, err)
		}
		return nil
	}
}

// Config returns the worker configuration.
func (w *Worker) Config() actiontemporal.Config {
	return w.cfg
}

// Metrics returns the worker metrics.
func (w *Worker) Metrics() *Metrics {
	return w.metrics
}

// Ready reports whether the execution engine is reachable.
func (w *Worker) Ready(ctx context.Context) error {
	if w.health == nil {
		return nil
	}
	return w.health(ctx)
}

// Run registers the workflow and activities and blocks until the context is
// cancelled or the worker stops.
func (w *Worker) Run(ctx context.Context) error {
	if w.cfg.Mode != actionservice.ExecutionModeTemporal {
		return fmt.Errorf("%w: actions worker requires execution mode %q, got %q", actionservice.ErrEngineUnavailable, actionservice.ExecutionModeTemporal, w.cfg.Mode)
	}
	if w.client == nil {
		return fmt.Errorf("%w: temporal client is required to run the actions worker", actionservice.ErrEngineUnavailable)
	}
	temporalWorker := sdkworker.New(w.client, w.cfg.TaskQueue, sdkworker.Options{})
	temporalWorker.RegisterWorkflowWithOptions(actiontemporal.ActionExecutionWorkflow, workflow.RegisterOptions{
		Name: actiontemporal.WorkflowName,
	})
	if w.activities != nil {
		temporalWorker.RegisterActivity(w.activities)
	}
	interrupt := make(chan any, 1)
	go func() {
		<-ctx.Done()
		select {
		case interrupt <- struct{}{}:
		default:
		}
	}()
	w.logger.Info("actions worker started",
		"namespace", w.cfg.Namespace,
		"task_queue", w.cfg.TaskQueue,
		"mode", string(w.cfg.Mode),
	)
	if err := temporalWorker.Run(interrupt); err != nil {
		return fmt.Errorf("run actions worker: %w", err)
	}
	w.logger.Info("actions worker stopped", "task_queue", w.cfg.TaskQueue)
	return nil
}

// HTTPHandler serves /healthz, /readyz and /metrics.
func (w *Worker) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, http.StatusOK, map[string]any{
			"status":     "ok",
			"mode":       string(w.cfg.Mode),
			"task_queue": w.cfg.TaskQueue,
		})
	})
	mux.HandleFunc("/readyz", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := w.Ready(ctx); err != nil {
			writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"mode":   string(w.cfg.Mode),
			})
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{
			"status": "ready",
			"mode":   string(w.cfg.Mode),
		})
	})
	if w.metrics != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(w.metrics.Registry(), promhttp.HandlerOpts{}))
	}
	return mux
}

func writeJSON(rw http.ResponseWriter, status int, payload any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(payload)
}
