package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

const (
	// DefaultReconcileInterval is how often running jobs are reconciled.
	DefaultReconcileInterval = 5 * time.Second
	// DefaultReconcileBatch caps how many running jobs one cycle inspects.
	DefaultReconcileBatch = 50
	// DefaultStableInterval is the minimum time between the two snapshot row
	// count samples that confirm a completed sync.
	DefaultStableInterval = 5 * time.Second
	// DefaultStabilityTTL drops in-memory stability samples of jobs that have
	// not been observed for a while (for example because they were cancelled).
	DefaultStabilityTTL = 2 * time.Minute
)

// ReconcilerOptions configures the reconciler.
type ReconcilerOptions struct {
	Interval       time.Duration
	BatchSize      int
	StableInterval time.Duration
	StabilityTTL   time.Duration
	Clock          func() time.Time
	Ticker         TickerFactory
	Logger         *slog.Logger
	Metrics        Metrics
}

type stabilitySample struct {
	rows int64
	at   time.Time
}

// Reconciler tracks running sync jobs against the connector runtime and drives
// them to a terminal state. It is safe to run in several instances: terminal
// writes are guarded by the stored job status, so a stale reconciler cannot
// overwrite a finished job.
type Reconciler struct {
	store      RuntimeStore
	connectors ConnectorReader
	jobRuntime JobRuntime
	verifier   SnapshotVerifier
	opts       ReconcilerOptions
	logger     *slog.Logger

	mu   sync.Mutex
	seen map[string]stabilitySample
}

// NewReconciler builds a reconciler. A nil verifier is allowed: the runtime
// then never reports a completed sync, so jobs stay running until the runtime
// itself fails or the job is cancelled.
func NewReconciler(store RuntimeStore, connectors ConnectorReader, jobRuntime JobRuntime, verifier SnapshotVerifier, opts ReconcilerOptions) (*Reconciler, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: reconciler store is nil", ErrInvalidConfig)
	}
	if connectors == nil {
		return nil, fmt.Errorf("%w: reconciler connector reader is nil", ErrInvalidConfig)
	}
	if jobRuntime == nil {
		return nil, fmt.Errorf("%w: reconciler job runtime is nil", ErrInvalidConfig)
	}
	opts = normalizeReconcilerOptions(opts)
	logger := opts.Logger
	if logger == nil {
		logger = discardLogger()
	}
	return &Reconciler{
		store:      store,
		connectors: connectors,
		jobRuntime: jobRuntime,
		verifier:   verifier,
		opts:       opts,
		logger:     logger,
		seen:       make(map[string]stabilitySample),
	}, nil
}

func normalizeReconcilerOptions(opts ReconcilerOptions) ReconcilerOptions {
	if opts.Interval <= 0 {
		opts.Interval = DefaultReconcileInterval
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultReconcileBatch
	}
	if opts.StableInterval <= 0 {
		opts.StableInterval = DefaultStableInterval
	}
	if opts.StabilityTTL <= 0 {
		opts.StabilityTTL = DefaultStabilityTTL
	}
	opts.Clock = clockOrNow(opts.Clock)
	if opts.Ticker == nil {
		opts.Ticker = DefaultTickerFactory
	}
	return opts
}

// Run reconciles running jobs until ctx is done. The first cycle runs
// immediately, later cycles follow the configured ticker.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := r.opts.Ticker(r.opts.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := r.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			r.logger.Warn("ingestion reconcile cycle failed", "error", Redact(err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
		}
	}
}

// ReconcileOnce reconciles up to BatchSize running jobs.
func (r *Reconciler) ReconcileOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := r.opts.Clock()
	jobs, err := r.store.ListRunning(ctx, r.opts.BatchSize)
	if err != nil {
		return fmt.Errorf("list running sync jobs: %w", err)
	}
	r.pruneStability(now)
	var errs []error
	for i := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := r.reconcileJob(ctx, now, &jobs[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// reconcileJob maps one runtime status to the persisted job state.
func (r *Reconciler) reconcileJob(ctx context.Context, now time.Time, job *domain.SyncJob) error {
	logger := r.logger.With("job_id", job.ID, "tenant_id", job.TenantID, "runtime_ref", job.RuntimeRef)
	if job.RuntimeRef == "" {
		// Jobs dispatched by an older version may lack a runtime reference;
		// without it the runtime cannot be inspected, so they are left alone.
		logger.Warn("running sync job has no runtime reference; leaving it unreconciled")
		return nil
	}
	status, err := r.jobRuntime.Status(ctx, job.TenantID, job.RuntimeRef)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A status read failure is transient: keep the job running and restart
		// its stability window because the two confirmation samples must be
		// consecutive successful checks.
		r.forgetStability(job.ID)
		logger.Warn("read connector runtime status failed", "error", Redact(err.Error()))
		return nil
	}

	switch status.Overall() {
	case StateFailed:
		return r.fail(ctx, job, failedMessage(status.FailedMessage), logger)
	case StateMissing, StateStopped, StatePaused:
		return r.fail(ctx, job, "runtime_not_running: runtime_"+string(status.Overall()), logger)
	case StateRunning:
		return r.handleRunning(ctx, job, logger)
	default:
		// Unknown runtime states are inconclusive: keep running and forget the
		// stability window so a later sample pair starts fresh.
		r.forgetStability(job.ID)
		logger.Debug("connector runtime reported an inconclusive state")
		return nil
	}
}

// handleRunning confirms completion through the snapshot verifier and only
// marks the job succeeded once the row count stayed stable across two
// consecutive checks at least StableInterval apart.
func (r *Reconciler) handleRunning(ctx context.Context, job *domain.SyncJob, logger *slog.Logger) error {
	if r.verifier == nil {
		r.forgetStability(job.ID)
		logger.Debug("no snapshot verifier configured; sync job stays running")
		return nil
	}
	rows, done, err := r.verifier.VerifySnapshot(ctx, job, job.RuntimeRef)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.forgetStability(job.ID)
		logger.Warn("verify connector runtime snapshot failed", "error", Redact(err.Error()))
		return nil
	}
	if !done {
		r.forgetStability(job.ID)
		logger.Debug("connector runtime snapshot is not complete yet")
		return nil
	}

	now := r.opts.Clock()
	prev, ok := r.stabilitySample(job.ID)
	r.rememberStability(job.ID, rows, now)
	if !ok || prev.rows != rows {
		logger.Debug("snapshot row count is not stable yet", "verified_rows", rows)
		return nil
	}
	if now.Sub(prev.at) < r.opts.StableInterval {
		// The second confirmation came too early; the next check will confirm.
		return nil
	}
	return r.succeed(ctx, job, rows, logger)
}

// succeed marks the job succeeded (terminal, immutable) and then performs the
// best-effort runtime cleanup. A cleanup failure is recorded and never revokes
// the success.
func (r *Reconciler) succeed(ctx context.Context, job *domain.SyncJob, verifiedRows int64, logger *slog.Logger) error {
	stats := map[string]any{
		domain.StatsPhase:        domain.PhaseDone,
		domain.StatsRuntimeRef:   job.RuntimeRef,
		domain.StatsAttempt:      job.Attempt,
		domain.StatsVerifiedRows: verifiedRows,
	}
	if datasetID := r.datasetID(ctx, job, logger); datasetID != "" {
		stats[domain.StatsDatasetID] = datasetID
	}
	if err := r.store.MarkJobSucceeded(ctx, job.TenantID, job.ID, stats); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			r.forgetStability(job.ID)
			logger.Info("sync job is no longer running; success not recorded")
			return nil
		}
		return fmt.Errorf("mark sync job succeeded: %w", err)
	}
	r.forgetStability(job.ID)
	logger.Info("sync job completed", "verified_rows", verifiedRows, "runtime_ref", job.RuntimeRef)
	r.cleanup(ctx, job, logger)
	return nil
}

// fail records a runtime failure. The guarded store write keeps terminal jobs
// immutable.
func (r *Reconciler) fail(ctx context.Context, job *domain.SyncJob, message string, logger *slog.Logger) error {
	message = Redact(message)
	stats := map[string]any{
		domain.StatsPhase:      domain.PhaseVerifying,
		domain.StatsRuntimeRef: job.RuntimeRef,
		domain.StatsAttempt:    job.Attempt,
		domain.StatsLastError:  message,
	}
	if err := r.store.MarkJobFailed(ctx, job.TenantID, job.ID, message, stats); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			r.forgetStability(job.ID)
			logger.Info("sync job is no longer running; failure not recorded")
			return nil
		}
		return fmt.Errorf("mark sync job failed: %w", err)
	}
	r.forgetStability(job.ID)
	logger.Warn("sync job runtime stopped running", "reason", message)
	return nil
}

// cleanup stops the submitted runtime resources best-effort. Failures are
// recorded as cleanup_pending on the succeeded run and never change the job
// status.
func (r *Reconciler) cleanup(ctx context.Context, job *domain.SyncJob, logger *slog.Logger) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeCleanupTimeout)
	defer cancel()
	if err := r.jobRuntime.Stop(cleanupCtx, job.TenantID, job.RuntimeRef); err != nil {
		message := Redact(err.Error())
		logger.Warn("stop completed connector runtime failed", "error", message)
		stats := map[string]any{
			domain.StatsCleanupPending: true,
			domain.StatsLastError:      message,
		}
		if statsErr := r.store.SetJobRuntimeStats(cleanupCtx, job.TenantID, job.ID, stats); statsErr != nil {
			logger.Warn("record pending runtime cleanup failed", "error", Redact(statsErr.Error()))
		}
	}
}

// datasetID reads the dataset the connector writes into. It is optional: a
// missing connector or dataset id only means the statistic is not recorded.
func (r *Reconciler) datasetID(ctx context.Context, job *domain.SyncJob, logger *slog.Logger) string {
	connector, err := r.connectors.GetConnector(ctx, job.TenantID, job.ConnectorID)
	if err != nil {
		logger.Warn("load connector for completed sync job failed", "error", Redact(err.Error()))
		return ""
	}
	if connector == nil || connector.Config == nil {
		return ""
	}
	value, ok := connector.Config["dataset_id"]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func failedMessage(message string) string {
	if message == "" {
		return "runtime_failed: the connector runtime reported a failure"
	}
	return "runtime_failed: " + message
}

func (r *Reconciler) stabilitySample(jobID string) (stabilitySample, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sample, ok := r.seen[jobID]
	return sample, ok
}

func (r *Reconciler) rememberStability(jobID string, rows int64, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[jobID] = stabilitySample{rows: rows, at: at}
}

func (r *Reconciler) forgetStability(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.seen, jobID)
}

func (r *Reconciler) pruneStability(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for jobID, sample := range r.seen {
		if now.Sub(sample.at) > r.opts.StabilityTTL {
			delete(r.seen, jobID)
		}
	}
}
