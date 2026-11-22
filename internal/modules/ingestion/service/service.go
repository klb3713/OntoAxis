// Package service implements ingestion use cases: sync job creation, state
// transitions, run projections, cancellation, and watch streams.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

var (
	// ErrInvalidTenant reports a missing or malformed tenant identifier.
	ErrInvalidTenant = errors.New("invalid tenant id")
	// ErrInvalidInput reports malformed request input.
	ErrInvalidInput = errors.New("invalid ingestion input")
	// ErrUnavailable reports that a required cross-module dependency is absent.
	ErrUnavailable = errors.New("ingestion dependency unavailable")
)

// Store is the persistence port owned by the ingestion service. Every method
// must scope reads and writes to the supplied tenant.
type Store interface {
	CreateJob(ctx context.Context, job *domain.SyncJob, run *domain.SyncRun) error
	GetJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	ListJobs(ctx context.Context, tenantID, connectorID string, limit, offset int32) ([]domain.SyncJob, int64, error)
	GetLatestRun(ctx context.Context, tenantID, jobID string) (*domain.SyncRun, error)
	UpdateJobAndRun(ctx context.Context, job *domain.SyncJob, expectedStatus domain.JobStatus, run *domain.SyncRun) error
	ListRuns(ctx context.Context, tenantID, jobID string) ([]domain.SyncRun, error)
}

// ConnectorReader is the narrow cross-module port implemented by the connectors
// service. Its GetConnector signature intentionally matches connectors.Service.
type ConnectorReader interface {
	GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error)
}

// RuntimeStopper stops a connector runtime resource owned by a sync job. The
// connector runtime client implements it; cancellation requires a confirmed
// stop before a submitted job may be reported cancelled.
type RuntimeStopper interface {
	Stop(ctx context.Context, tenantID, runtimeRef string) error
}

// Option configures the ingestion service.
type Option func(*serviceImpl)

// WithClock overrides the clock used for timestamps and IDs' timestamps.
func WithClock(clock func() time.Time) Option {
	return func(s *serviceImpl) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// WithConnectorReader injects the connectors service dependency.
func WithConnectorReader(reader ConnectorReader) Option {
	return func(s *serviceImpl) { s.connectors = reader }
}

// WithRuntimeStopper injects the connector runtime stop dependency used to
// confirm cancellation of submitted sync jobs.
func WithRuntimeStopper(stopper RuntimeStopper) Option {
	return func(s *serviceImpl) { s.stopper = stopper }
}

// WithWatchInterval overrides the polling interval used by Watch.
func WithWatchInterval(interval time.Duration) Option {
	return func(s *serviceImpl) {
		if interval > 0 {
			s.watchInterval = interval
		}
	}
}

// Service is the public ingestion use case interface.
type Service interface {
	CreateJob(ctx context.Context, tenantID, connectorID string) (*domain.SyncJob, error)
	GetJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	ListJobs(ctx context.Context, tenantID, connectorID string, page, perPage int32) (*ListResult, error)
	CancelJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	MarkRunning(ctx context.Context, tenantID, id string) (*domain.SyncJob, error)
	MarkSucceeded(ctx context.Context, tenantID, id string, stats map[string]any) (*domain.SyncJob, error)
	MarkFailed(ctx context.Context, tenantID, id, message string, stats map[string]any) (*domain.SyncJob, error)
	Complete(ctx context.Context, tenantID, id string, stats map[string]any) (*domain.SyncJob, error)
	StartSync(ctx context.Context, tenantID, connectorID string) (string, error)
	ListRuns(ctx context.Context, tenantID, jobID string) ([]domain.SyncRun, error)
	Watch(ctx context.Context, tenantID, jobID string) (<-chan *domain.SyncJob, <-chan error, error)
}

// serviceImpl implements Service over a Store and the connectors read port.
type serviceImpl struct {
	store         Store
	connectors    ConnectorReader
	stopper       RuntimeStopper
	clock         func() time.Time
	watchInterval time.Duration
}

// NewService builds an ingestion service.
func NewService(store Store, opts ...Option) Service {
	s := &serviceImpl{store: store, clock: time.Now, watchInterval: 250 * time.Millisecond}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListResult is one page of sync jobs.
type ListResult struct {
	Jobs       []domain.SyncJob
	Page       int32
	PerPage    int32
	Total      int64
	TotalPages int32
}

// CreateJob creates a queued sync job and its initial run for an active connector.
func (s *serviceImpl) CreateJob(ctx context.Context, tenantID, connectorID string) (*domain.SyncJob, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if err := validateID(connectorID, "connector id"); err != nil {
		return nil, err
	}
	connector, err := s.connectors.GetConnector(ctx, tenantID, connectorID)
	if err != nil {
		if errors.Is(err, connectordomain.ErrNotFound) {
			return nil, fmt.Errorf("%w: connector %s", domain.ErrNotFound, connectorID)
		}
		return nil, err
	}
	if connector.Status != connectordomain.StatusActive {
		return nil, fmt.Errorf("%w: connector %s is %s", domain.ErrConflict, connectorID, connector.Status)
	}
	now := s.clock().UTC()
	jobID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate sync job id: %w", err)
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate sync run id: %w", err)
	}
	job := &domain.SyncJob{
		ID:          jobID.String(),
		TenantID:    tenantID,
		ConnectorID: connectorID,
		Mode:        "full",
		Status:      domain.StatusQueued,
		Config:      map[string]any{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	run := &domain.SyncRun{
		ID:        runID.String(),
		TenantID:  tenantID,
		JobID:     job.ID,
		Status:    domain.StatusQueued,
		Stats:     map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateJob(ctx, job, run); err != nil {
		return nil, err
	}
	return job, nil
}

// GetJob returns one job scoped to tenantID.
func (s *serviceImpl) GetJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if err := validateID(id, "job id"); err != nil {
		return nil, err
	}
	return s.store.GetJob(ctx, tenantID, id)
}

// ListJobs returns one page, optionally filtered by connector.
func (s *serviceImpl) ListJobs(ctx context.Context, tenantID, connectorID string, page, perPage int32) (*ListResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if connectorID != "" {
		if err := validateID(connectorID, "connector id"); err != nil {
			return nil, err
		}
	}
	page, perPage = normalizePage(page, perPage)
	items, total, err := s.store.ListJobs(ctx, tenantID, connectorID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, err
	}
	totalPages := int32(0)
	if total > 0 {
		totalPages = int32((total + int64(perPage) - 1) / int64(perPage))
	}
	return &ListResult{Jobs: items, Page: page, PerPage: perPage, Total: total, TotalPages: totalPages}, nil
}

// CancelJob moves a queued or running job to cancelled. When the job already
// holds a runtime reference the connector runtime must confirm the stop before
// the cancellation is written: an unconfirmed stop leaves the job running and
// reports ErrUnavailable, because a runtime that is still running must never be
// reported as cancelled.
func (s *serviceImpl) CancelJob(ctx context.Context, tenantID, id string) (*domain.SyncJob, error) {
	job, err := s.GetJob(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if job.Status.Terminal() {
		return nil, fmt.Errorf("%w: %w", domain.ErrConflict, domain.ValidateTransition(job.Status, domain.StatusCancelled))
	}
	stoppedRef, err := s.stopRuntimeForCancel(ctx, tenantID, job.RuntimeRef)
	if err != nil {
		return nil, err
	}
	run, err := s.store.GetLatestRun(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	latest, err := s.store.GetJob(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if latest.Status.Terminal() {
		return nil, fmt.Errorf("%w: sync job %s is already %s", domain.ErrConflict, id, latest.Status)
	}
	// A submit may have attached a runtime reference after the first read. Stop
	// it as well so cancellation never leaves a running resource behind.
	if latest.RuntimeRef != "" && latest.RuntimeRef != stoppedRef {
		if _, err := s.stopRuntimeForCancel(ctx, tenantID, latest.RuntimeRef); err != nil {
			return nil, err
		}
	}
	return s.applyTransition(ctx, latest, run, domain.StatusCancelled, "", nil)
}

// stopRuntimeForCancel confirms the stop of one runtime reference. An empty
// reference means the job was never submitted, so there is nothing to stop.
func (s *serviceImpl) stopRuntimeForCancel(ctx context.Context, tenantID, runtimeRef string) (string, error) {
	if runtimeRef == "" {
		return "", nil
	}
	if s.stopper == nil {
		return "", fmt.Errorf("%w: connector runtime stopper is not configured", ErrUnavailable)
	}
	if err := s.stopper.Stop(ctx, tenantID, runtimeRef); err != nil {
		return "", fmt.Errorf("%w: stop connector runtime %s: %w", ErrUnavailable, runtimeRef, err)
	}
	return runtimeRef, nil
}

// MarkRunning moves a queued job to running and projects the run start time.
func (s *serviceImpl) MarkRunning(ctx context.Context, tenantID, id string) (*domain.SyncJob, error) {
	return s.transition(ctx, tenantID, id, domain.StatusRunning, "", nil)
}

// MarkSucceeded finishes a running job successfully.
func (s *serviceImpl) MarkSucceeded(ctx context.Context, tenantID, id string, stats map[string]any) (*domain.SyncJob, error) {
	return s.transition(ctx, tenantID, id, domain.StatusSucceeded, "", stats)
}

// Complete finishes a running job successfully. It is an alias for
// MarkSucceeded kept for callers that use the shorter verb.
func (s *serviceImpl) Complete(ctx context.Context, tenantID, id string, stats map[string]any) (*domain.SyncJob, error) {
	return s.MarkSucceeded(ctx, tenantID, id, stats)
}

// MarkFailed finishes a running job with an error message.
func (s *serviceImpl) MarkFailed(ctx context.Context, tenantID, id, message string, stats map[string]any) (*domain.SyncJob, error) {
	return s.transition(ctx, tenantID, id, domain.StatusFailed, message, stats)
}

// StartSync creates a job and returns its identifier. The signature matches the
// connectors.SyncStarter port so the ingestion service can be injected there.
func (s *serviceImpl) StartSync(ctx context.Context, tenantID, connectorID string) (string, error) {
	job, err := s.CreateJob(ctx, tenantID, connectorID)
	if err != nil {
		return "", err
	}
	return job.ID, nil
}

// ListRuns returns the run projection history for a tenant-owned job.
func (s *serviceImpl) ListRuns(ctx context.Context, tenantID, jobID string) ([]domain.SyncRun, error) {
	if _, err := s.GetJob(ctx, tenantID, jobID); err != nil {
		return nil, err
	}
	return s.store.ListRuns(ctx, tenantID, jobID)
}

// Watch streams the job state. A terminal job is emitted immediately and the
// channels close; an active job emits status changes until terminal or ctx done.
func (s *serviceImpl) Watch(ctx context.Context, tenantID, jobID string) (<-chan *domain.SyncJob, <-chan error, error) {
	job, err := s.GetJob(ctx, tenantID, jobID)
	if err != nil {
		return nil, nil, err
	}
	updates := make(chan *domain.SyncJob, 1)
	errs := make(chan error, 1)
	updates <- job
	if job.Status.Terminal() {
		close(updates)
		close(errs)
		return updates, errs, nil
	}
	go func() {
		defer close(updates)
		defer close(errs)
		ticker := time.NewTicker(s.watchInterval)
		defer ticker.Stop()
		lastStatus := job.Status
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := s.store.GetJob(ctx, tenantID, jobID)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
				if current.Status == lastStatus {
					continue
				}
				lastStatus = current.Status
				select {
				case updates <- current:
				case <-ctx.Done():
					return
				}
				if current.Status.Terminal() {
					return
				}
			}
		}
	}()
	return updates, errs, nil
}

func (s *serviceImpl) transition(ctx context.Context, tenantID, id string, target domain.JobStatus, message string, stats map[string]any) (*domain.SyncJob, error) {
	job, err := s.GetJob(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateTransition(job.Status, target); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrConflict, err)
	}
	run, err := s.store.GetLatestRun(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return s.applyTransition(ctx, job, run, target, message, stats)
}

// applyTransition validates target against the job's loaded status, stamps the
// transition and persists the job with its run projection in one guarded write.
func (s *serviceImpl) applyTransition(ctx context.Context, job *domain.SyncJob, run *domain.SyncRun, target domain.JobStatus, message string, stats map[string]any) (*domain.SyncJob, error) {
	expected := job.Status
	if err := domain.ValidateTransition(expected, target); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrConflict, err)
	}
	now := s.clock().UTC()
	job.Status = target
	job.UpdatedAt = now
	if message != "" {
		job.Message = message
	}
	if target == domain.StatusRunning && job.StartedAt == nil {
		job.StartedAt = timePtr(now)
	}
	if target != domain.StatusQueued {
		// A lease only exists while a job is queued; leaving that state clears it.
		job.LeaseOwner = ""
		job.LeaseUntil = nil
	}
	if target.Terminal() {
		job.FinishedAt = timePtr(now)
	}
	run.Status = target
	run.UpdatedAt = now
	if target == domain.StatusRunning && run.StartedAt == nil {
		run.StartedAt = timePtr(now)
	}
	if target.Terminal() {
		run.FinishedAt = timePtr(now)
	}
	if message != "" {
		run.Error = message
	}
	if stats != nil {
		run.Stats = cloneMap(stats)
	}
	if err := s.store.UpdateJobAndRun(ctx, job, expected, run); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *serviceImpl) ready() error {
	if s == nil || s.store == nil || s.connectors == nil {
		return fmt.Errorf("%w: ingestion dependencies are not configured", ErrUnavailable)
	}
	return nil
}

func validateTenant(tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("%w: tenant id is empty", ErrInvalidTenant)
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTenant, err)
	}
	return nil
}

func validateID(id, field string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: invalid %s %q", ErrInvalidInput, field, id)
	}
	return nil
}

func normalizePage(page, perPage int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func timePtr(value time.Time) *time.Time { return &value }

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
