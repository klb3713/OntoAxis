package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

const (
	// DefaultDispatchInterval is how often the dispatcher looks for queued jobs.
	DefaultDispatchInterval = time.Second
	// DefaultDispatchBatch caps how many jobs one dispatch cycle processes.
	DefaultDispatchBatch = 25
	// DefaultLeaseDuration is how long a claim protects a queued job.
	DefaultLeaseDuration = 30 * time.Second
	// DefaultMaxAttempts bounds runtime submissions across dispatcher claims.
	DefaultMaxAttempts = 5
	// DefaultBackoffBase is the first retry delay; later delays double.
	DefaultBackoffBase = time.Second
	// maxBackoff caps exponential backoff even with a custom base.
	maxBackoff = time.Minute
)

// DispatcherOptions configures the dispatcher. Zero values select the
// documented defaults; tests inject clocks, tickers, sleeps and randomness.
type DispatcherOptions struct {
	Interval      time.Duration
	MaxBatch      int
	LeaseDuration time.Duration
	MaxAttempts   int
	BackoffBase   time.Duration
	LeaseOwner    string
	Clock         func() time.Time
	Sleep         func(ctx context.Context, d time.Duration) error
	Ticker        TickerFactory
	Rand          func() float64
	Classifier    Classifier
	Logger        *slog.Logger
	Metrics       Metrics
}

// Dispatcher claims queued sync jobs and submits them to the connector runtime.
// It is safe to run in several instances: claims are atomic and guarded by a
// lease.
type Dispatcher struct {
	store      RuntimeStore
	connectors ConnectorReader
	jobRuntime JobRuntime
	opts       DispatcherOptions
	logger     *slog.Logger
}

// NewDispatcher builds a dispatcher. It fails when a required port is missing
// so a misconfigured module never starts silently.
func NewDispatcher(store RuntimeStore, connectors ConnectorReader, jobRuntime JobRuntime, opts DispatcherOptions) (*Dispatcher, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: dispatcher store is nil", ErrInvalidConfig)
	}
	if connectors == nil {
		return nil, fmt.Errorf("%w: dispatcher connector reader is nil", ErrInvalidConfig)
	}
	if jobRuntime == nil {
		return nil, fmt.Errorf("%w: dispatcher job runtime is nil", ErrInvalidConfig)
	}
	opts = normalizeDispatcherOptions(opts)
	logger := opts.Logger
	if logger == nil {
		logger = discardLogger()
	}
	return &Dispatcher{store: store, connectors: connectors, jobRuntime: jobRuntime, opts: opts, logger: logger}, nil
}

func normalizeDispatcherOptions(opts DispatcherOptions) DispatcherOptions {
	if opts.Interval <= 0 {
		opts.Interval = DefaultDispatchInterval
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = DefaultDispatchBatch
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = DefaultLeaseDuration
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = DefaultBackoffBase
	}
	if strings.TrimSpace(opts.LeaseOwner) == "" {
		opts.LeaseOwner = defaultLeaseOwner()
	}
	opts.Clock = clockOrNow(opts.Clock)
	if opts.Sleep == nil {
		opts.Sleep = SleepContext
	}
	if opts.Ticker == nil {
		opts.Ticker = DefaultTickerFactory
	}
	if opts.Rand == nil {
		opts.Rand = randFloat64
	}
	if opts.Classifier == nil {
		opts.Classifier = DefaultClassify
	}
	return opts
}

// Run dispatches queued jobs until ctx is done. The first cycle runs
// immediately, later cycles follow the configured ticker.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := d.opts.Ticker(d.opts.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := d.DispatchOnce(ctx); err != nil && ctx.Err() == nil {
			d.logger.Warn("ingestion dispatch cycle failed", "error", Redact(err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
		}
	}
}

// DispatchOnce claims and dispatches up to MaxBatch queued jobs. Per-job
// failures are handled internally (retried or marked failed); the returned
// error reports storage or context failures for the cycle.
func (d *Dispatcher) DispatchOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var errs []error
	seen := make(map[string]struct{}, d.opts.MaxBatch)
	for i := 0; i < d.opts.MaxBatch; i++ {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		job, err := d.store.ClaimQueued(ctx, d.opts.LeaseOwner, d.opts.Clock().Add(d.opts.LeaseDuration))
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) {
				errs = append(errs, fmt.Errorf("claim queued sync job: %w", err))
			}
			break
		}
		// A job left queued (for example because a dependency read failed) has
		// its lease released and would be claimable again immediately. Process
		// each job at most once per cycle so a failure cannot spin the batch.
		if _, duplicate := seen[job.ID]; duplicate {
			d.releaseLease(ctx, job, d.logger.With("job_id", job.ID, "tenant_id", job.TenantID))
			break
		}
		seen[job.ID] = struct{}{}
		if err := d.dispatchClaimed(ctx, job); err != nil {
			errs = append(errs, err)
			if ctx.Err() != nil {
				break
			}
		}
	}
	return errors.Join(errs...)
}

// dispatchClaimed handles a job already protected by this dispatcher's lease.
// The lease is released on every exit path except a successful submission,
// where the state transition itself clears it.
func (d *Dispatcher) dispatchClaimed(ctx context.Context, job *domain.SyncJob) error {
	logger := d.logger.With("job_id", job.ID, "tenant_id", job.TenantID, "connector_id", job.ConnectorID)
	defer d.releaseLease(ctx, job, logger)

	connector, err := d.connectors.GetConnector(ctx, job.TenantID, job.ConnectorID)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, connectordomain.ErrNotFound) {
			return d.failJob(ctx, job, "connector_not_found: the connector no longer exists", job.Attempt, logger)
		}
		// A transient connector read failure must not burn a submission
		// attempt: release the lease and leave the job queued for the next cycle.
		logger.Warn("load connector for dispatch failed", "error", Redact(err.Error()))
		return nil
	}
	if connector == nil {
		return d.failJob(ctx, job, "connector_not_found: the connector reader returned no connector", job.Attempt, logger)
	}
	if connector.Status != connectordomain.StatusActive {
		return d.failJob(ctx, job, fmt.Sprintf("connector_not_active: connector is %s", connector.Status), job.Attempt, logger)
	}

	active, err := d.store.FindActiveJobByConnector(ctx, job.TenantID, job.ConnectorID, job.ID)
	switch {
	case err == nil:
		if olderActiveJob(active, job) {
			return d.failJob(ctx, job, "connector_sync_conflict: another sync job is queued or running for this connector", job.Attempt, logger)
		}
	case errors.Is(err, domain.ErrNotFound):
		// No competing sync job for this connector.
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Warn("check active sync job for dispatch failed", "error", Redact(err.Error()))
		return nil
	}

	if job.Attempt >= d.opts.MaxAttempts {
		return d.failJob(ctx, job, "submit_retries_exhausted: the attempt limit was already consumed", job.Attempt, logger)
	}
	return d.submitWithRetries(ctx, job, connector, logger)
}

// submitWithRetries submits to the connector runtime, retrying retryable
// failures with exponential backoff and jitter. The submission must be
// idempotent for one connector (the connector runtime adapter creates-or-gets
// by connector-derived name, design D3), because an ambiguous failure may have
// created the runtime resource already.
func (d *Dispatcher) submitWithRetries(ctx context.Context, job *domain.SyncJob, connector *connectordomain.Connector, logger *slog.Logger) error {
	for attempt := job.Attempt + 1; attempt <= d.opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.store.RenewLease(ctx, job.ID, d.opts.LeaseOwner, d.opts.Clock().Add(d.opts.LeaseDuration)); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				logger.Info("dispatch lease lost before submit; leaving the job to its new owner")
				return nil
			}
			return fmt.Errorf("renew dispatch lease: %w", err)
		}

		started := d.opts.Clock()
		runtimeRef, submitErr := d.jobRuntime.Submit(ctx, job.TenantID, connector)
		d.observeSubmit(d.opts.Clock().Sub(started), submitErr)
		if submitErr == nil {
			return d.markRunning(ctx, job, runtimeRef, attempt, logger)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		class := d.opts.Classifier(submitErr)
		message := Redact(submitErr.Error())
		d.observeRetry("submit", class)
		logger.Warn("connector runtime submit failed", "attempt", attempt, "class", string(class), "error", message)

		if class == ClassPermanent {
			return d.failJob(ctx, job, "submit_permanent_failure: "+message, attempt, logger)
		}
		if attempt >= d.opts.MaxAttempts {
			return d.failJob(ctx, job, "submit_retries_exhausted: "+message, attempt, logger)
		}
		stats := map[string]any{
			domain.StatsPhase:     domain.PhaseSubmitting,
			domain.StatsAttempts:  attempt,
			domain.StatsLastError: message,
		}
		if err := d.store.RecordAttempt(ctx, job.TenantID, job.ID, attempt, message, stats); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				logger.Info("sync job left the queued state while retrying; stopping dispatch")
				return nil
			}
			return fmt.Errorf("record dispatch attempt: %w", err)
		}
		if err := d.opts.Sleep(ctx, d.backoff(attempt)); err != nil {
			return err
		}
	}
	return nil
}

// markRunning persists the successful submission. If the job left the queued
// state meanwhile (typically a cancel won the race), the freshly created
// runtime resource is stopped so no orphan keeps running.
func (d *Dispatcher) markRunning(ctx context.Context, job *domain.SyncJob, runtimeRef string, attempt int, logger *slog.Logger) error {
	if strings.TrimSpace(runtimeRef) == "" {
		return d.failJob(ctx, job, "submit_invalid_runtime_ref: the connector runtime returned an empty reference", attempt, logger)
	}
	if err := d.store.MarkRunningRuntime(ctx, job, runtimeRef, attempt); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			logger.Info("sync job left the queued state after submit; stopping the orphan runtime", "runtime_ref", runtimeRef)
			d.stopRuntime(ctx, job.TenantID, runtimeRef, logger)
			return nil
		}
		return fmt.Errorf("mark sync job running: %w", err)
	}
	logger.Info("sync job submitted to the connector runtime", "runtime_ref", runtimeRef, "attempt", attempt)
	return nil
}

// failJob records a permanent dispatch failure and consumes the lease. A job
// that already reached another state is left untouched.
func (d *Dispatcher) failJob(ctx context.Context, job *domain.SyncJob, failure string, attempts int, logger *slog.Logger) error {
	message := Redact(failure)
	stats := map[string]any{
		domain.StatsPhase:     domain.PhaseSubmitting,
		domain.StatsAttempts:  attempts,
		domain.StatsLastError: message,
	}
	if err := d.store.MarkJobFailed(ctx, job.TenantID, job.ID, message, stats); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			logger.Info("sync job is no longer dispatchable; failure not recorded", "reason", message)
			return nil
		}
		return fmt.Errorf("mark sync job failed: %w", err)
	}
	logger.Warn("sync job dispatch failed", "attempts", attempts, "reason", message)
	return nil
}

// releaseLease always uses a detached, bounded context: cleanup must run even
// when the dispatch context was cancelled.
func (d *Dispatcher) releaseLease(ctx context.Context, job *domain.SyncJob, logger *slog.Logger) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeCleanupTimeout)
	defer cancel()
	if err := d.store.ReleaseLease(cleanupCtx, job.ID, d.opts.LeaseOwner); err != nil {
		logger.Warn("release dispatch lease failed", "error", Redact(err.Error()))
	}
}

// stopRuntime best-effort stops an orphaned runtime resource.
func (d *Dispatcher) stopRuntime(ctx context.Context, tenantID, runtimeRef string, logger *slog.Logger) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeCleanupTimeout)
	defer cancel()
	if err := d.jobRuntime.Stop(cleanupCtx, tenantID, runtimeRef); err != nil {
		logger.Warn("stop orphaned connector runtime failed", "runtime_ref", runtimeRef, "error", Redact(err.Error()))
	}
}

// backoff returns the retry delay for the failed attempt: base doubled per
// attempt with ±20% jitter, capped at maxBackoff.
func (d *Dispatcher) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := d.opts.BackoffBase
	for i := 1; i < attempt; i++ {
		if delay >= maxBackoff {
			break
		}
		delay *= 2
	}
	if delay > maxBackoff {
		delay = maxBackoff
	}
	jitter := 1.0 + (clampUnit(d.opts.Rand())*2-1)*0.2
	duration := time.Duration(float64(delay) * jitter)
	if duration < 0 {
		return 0
	}
	return duration
}

func (d *Dispatcher) observeSubmit(duration time.Duration, err error) {
	if d.opts.Metrics == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failed"
	}
	d.opts.Metrics.ObserveSubmit(duration, result)
}

func (d *Dispatcher) observeRetry(operation string, class ErrorClass) {
	if d.opts.Metrics == nil {
		return
	}
	d.opts.Metrics.IncRetry(operation, string(class))
}

// olderActiveJob reports whether other predates job and therefore wins the
// per-connector concurrency guard.
func olderActiveJob(other, job *domain.SyncJob) bool {
	if other == nil {
		return false
	}
	if other.CreatedAt.Before(job.CreatedAt) {
		return true
	}
	return other.CreatedAt.Equal(job.CreatedAt) && other.ID < job.ID
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
