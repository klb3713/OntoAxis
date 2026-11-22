package runtime

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
)

// fakeClock is a controllable clock shared by the dispatcher, reconciler and
// fake store so lease expiry can be tested without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeStore implements RuntimeStore in memory, including lease guards and
// tenant checks that mirror the Postgres repository.
type fakeStore struct {
	mu    sync.Mutex
	jobs  map[string]*domain.SyncJob
	runs  map[string]*domain.SyncRun
	clock *fakeClock
	// events records state-changing calls in order.
	events []string

	claimCalls  int
	renewCalls  int
	listCalls   int
	claimErr    error
	listErr     error
	renewErr    error
	renewOnCall int
	markRunErr  error
	failErr     error
	succeedErr  error
	statsErr    error
}

func newFakeStore(clock *fakeClock) *fakeStore {
	if clock == nil {
		clock = newFakeClock()
	}
	return &fakeStore{
		jobs:  make(map[string]*domain.SyncJob),
		runs:  make(map[string]*domain.SyncRun),
		clock: clock,
	}
}

func (s *fakeStore) nowLocked() time.Time {
	s.clock.mu.Lock()
	defer s.clock.mu.Unlock()
	return s.clock.now
}

func (s *fakeStore) record(event string) {
	s.events = append(s.events, event)
}

func (s *fakeStore) addJob(job *domain.SyncJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = cloneJob(job)
	_, exists := s.runs[job.ID]
	if !exists {
		s.runs[job.ID] = &domain.SyncRun{
			ID:        "run-" + job.ID,
			TenantID:  job.TenantID,
			JobID:     job.ID,
			Status:    job.Status,
			Stats:     map[string]any{},
			CreatedAt: job.CreatedAt,
			UpdatedAt: job.UpdatedAt,
		}
	}
}

func (s *fakeStore) setJobStatus(jobID string, status domain.JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[jobID]
	if job == nil {
		return
	}
	job.Status = status
	run := s.runs[jobID]
	if run != nil {
		run.Status = status
	}
}

func (s *fakeStore) getJob(jobID string) *domain.SyncJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneJob(s.jobs[jobID])
}

func (s *fakeStore) getRun(jobID string) *domain.SyncRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRun(s.runs[jobID])
}

func (s *fakeStore) eventsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func (s *fakeStore) countEvents(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, event := range s.events {
		if len(event) >= len(prefix) && event[:len(prefix)] == prefix {
			count++
		}
	}
	return count
}

func (s *fakeStore) ClaimQueued(ctx context.Context, leaseOwner string, leaseUntil time.Time) (*domain.SyncJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	now := s.nowLocked()
	var candidates []*domain.SyncJob
	for _, job := range s.jobs {
		if job.Status != domain.StatusQueued {
			continue
		}
		if job.LeaseUntil != nil && job.LeaseUntil.After(now) {
			continue
		}
		candidates = append(candidates, job)
	}
	if len(candidates) == 0 {
		return nil, domain.ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	job := candidates[0]
	job.LeaseOwner = leaseOwner
	lease := leaseUntil
	job.LeaseUntil = &lease
	s.record("claim:" + job.ID)
	return cloneJob(job), nil
}

func (s *fakeStore) RenewLease(ctx context.Context, jobID, leaseOwner string, leaseUntil time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renewCalls++
	if s.renewOnCall == s.renewCalls && s.renewErr != nil {
		// A lost lease means another dispatcher took ownership; simulate that
		// so the caller's deferred release cannot make the job claimable again.
		if errors.Is(s.renewErr, domain.ErrConflict) {
			if job := s.jobs[jobID]; job != nil {
				job.LeaseOwner = "other-owner"
				lease := leaseUntil
				job.LeaseUntil = &lease
			}
		}
		return s.renewErr
	}
	job := s.jobs[jobID]
	if job == nil {
		return domain.ErrNotFound
	}
	if job.Status != domain.StatusQueued || job.LeaseOwner != leaseOwner {
		return domain.ErrConflict
	}
	lease := leaseUntil
	job.LeaseUntil = &lease
	s.record("renew:" + jobID)
	return nil
}

func (s *fakeStore) ReleaseLease(ctx context.Context, jobID, leaseOwner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[jobID]
	if job == nil || job.LeaseOwner != leaseOwner {
		return nil
	}
	job.LeaseOwner = ""
	job.LeaseUntil = nil
	s.record("release:" + jobID)
	return nil
}

func (s *fakeStore) MarkRunningRuntime(ctx context.Context, job *domain.SyncJob, runtimeRef string, attempt int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.markRunErr != nil {
		// Simulate a concurrent cancel winning the race after the runtime was
		// submitted: the job leaves the queued state before the mark lands.
		if stored := s.jobs[job.ID]; stored != nil && stored.TenantID == job.TenantID {
			stored.Status = domain.StatusCancelled
		}
		return s.markRunErr
	}
	stored := s.jobs[job.ID]
	if stored == nil || stored.TenantID != job.TenantID {
		return domain.ErrNotFound
	}
	if stored.Status != domain.StatusQueued {
		return domain.ErrConflict
	}
	now := s.nowLocked()
	stored.Status = domain.StatusRunning
	stored.RuntimeRef = runtimeRef
	stored.Attempt = attempt
	stored.LeaseOwner = ""
	stored.LeaseUntil = nil
	if stored.StartedAt == nil {
		started := now
		stored.StartedAt = &started
	}
	stored.UpdatedAt = now
	run := s.runs[job.ID]
	if run != nil {
		run.Status = domain.StatusRunning
		run.UpdatedAt = now
		mergeStats(run.Stats, map[string]any{
			domain.StatsPhase:      domain.PhaseSnapshotting,
			domain.StatsRuntimeRef: runtimeRef,
			domain.StatsAttempt:    attempt,
		})
	}
	s.record("running:" + job.ID)
	return nil
}

func (s *fakeStore) RecordAttempt(ctx context.Context, tenantID, jobID string, attempt int, message string, stats map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.jobs[jobID]
	if stored == nil || stored.TenantID != tenantID {
		return domain.ErrNotFound
	}
	if stored.Status != domain.StatusQueued {
		return domain.ErrConflict
	}
	now := s.nowLocked()
	stored.Attempt = attempt
	stored.UpdatedAt = now
	run := s.runs[jobID]
	if run != nil {
		run.Status = domain.StatusQueued
		run.Error = message
		run.UpdatedAt = now
		mergeStats(run.Stats, stats)
	}
	s.record("attempt:" + jobID)
	return nil
}

func (s *fakeStore) MarkJobFailed(ctx context.Context, tenantID, jobID, message string, stats map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return s.failErr
	}
	stored := s.jobs[jobID]
	if stored == nil || stored.TenantID != tenantID {
		return domain.ErrNotFound
	}
	if stored.Status != domain.StatusQueued && stored.Status != domain.StatusRunning {
		return domain.ErrConflict
	}
	now := s.nowLocked()
	stored.Status = domain.StatusFailed
	stored.LeaseOwner = ""
	stored.LeaseUntil = nil
	stored.UpdatedAt = now
	if stored.FinishedAt == nil {
		finished := now
		stored.FinishedAt = &finished
	}
	run := s.runs[jobID]
	if run != nil {
		run.Status = domain.StatusFailed
		run.Error = message
		run.UpdatedAt = now
		if run.FinishedAt == nil {
			finished := now
			run.FinishedAt = &finished
		}
		mergeStats(run.Stats, stats)
	}
	s.record("failed:" + jobID)
	return nil
}

func (s *fakeStore) MarkJobSucceeded(ctx context.Context, tenantID, jobID string, stats map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.succeedErr != nil {
		return s.succeedErr
	}
	stored := s.jobs[jobID]
	if stored == nil || stored.TenantID != tenantID {
		return domain.ErrNotFound
	}
	if stored.Status != domain.StatusRunning {
		return domain.ErrConflict
	}
	now := s.nowLocked()
	stored.Status = domain.StatusSucceeded
	stored.UpdatedAt = now
	if stored.FinishedAt == nil {
		finished := now
		stored.FinishedAt = &finished
	}
	run := s.runs[jobID]
	if run != nil {
		run.Status = domain.StatusSucceeded
		run.UpdatedAt = now
		if run.FinishedAt == nil {
			finished := now
			run.FinishedAt = &finished
		}
		mergeStats(run.Stats, stats)
	}
	s.record("succeeded:" + jobID)
	return nil
}

func (s *fakeStore) ListRunning(ctx context.Context, limit int) ([]domain.SyncJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	var jobs []*domain.SyncJob
	for _, job := range s.jobs {
		if job.Status == domain.StatusRunning {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	result := make([]domain.SyncJob, 0, len(jobs))
	for _, job := range jobs {
		result = append(result, *cloneJob(job))
	}
	return result, nil
}

func (s *fakeStore) SetJobRuntimeStats(ctx context.Context, tenantID, jobID string, stats map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statsErr != nil {
		return s.statsErr
	}
	stored := s.jobs[jobID]
	if stored == nil || stored.TenantID != tenantID {
		return domain.ErrNotFound
	}
	run := s.runs[jobID]
	if run == nil {
		return domain.ErrNotFound
	}
	mergeStats(run.Stats, stats)
	s.record("stats:" + jobID)
	return nil
}

func (s *fakeStore) FindActiveJobByConnector(ctx context.Context, tenantID, connectorID, excludeJobID string) (*domain.SyncJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var candidates []*domain.SyncJob
	for _, job := range s.jobs {
		if job.TenantID != tenantID || job.ConnectorID != connectorID || job.ID == excludeJobID {
			continue
		}
		if job.Status == domain.StatusQueued || job.Status == domain.StatusRunning {
			candidates = append(candidates, job)
		}
	}
	if len(candidates) == 0 {
		return nil, domain.ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	return cloneJob(candidates[0]), nil
}

type submitCall struct {
	TenantID    string
	ConnectorID string
}

type stopCall struct {
	TenantID   string
	RuntimeRef string
}

type statusCall struct {
	TenantID   string
	RuntimeRef string
}

// fakeJobRuntime simulates the connector runtime with injectable hooks.
type fakeJobRuntime struct {
	mu         sync.Mutex
	submitFunc func(ctx context.Context, tenantID string, connector *connectordomain.Connector) (string, error)
	statusFunc func(ctx context.Context, tenantID, runtimeRef string) (RuntimeStatus, error)
	stopFunc   func(ctx context.Context, tenantID, runtimeRef string) error
	submits    []submitCall
	stops      []stopCall
	statuses   []statusCall
}

func newFakeJobRuntime() *fakeJobRuntime {
	return &fakeJobRuntime{}
}

func (f *fakeJobRuntime) Submit(ctx context.Context, tenantID string, connector *connectordomain.Connector) (string, error) {
	f.mu.Lock()
	f.submits = append(f.submits, submitCall{TenantID: tenantID, ConnectorID: connector.ID})
	hook := f.submitFunc
	f.mu.Unlock()
	if hook == nil {
		return "runtime-" + connector.ID, nil
	}
	return hook(ctx, tenantID, connector)
}

func (f *fakeJobRuntime) Status(ctx context.Context, tenantID, runtimeRef string) (RuntimeStatus, error) {
	f.mu.Lock()
	f.statuses = append(f.statuses, statusCall{TenantID: tenantID, RuntimeRef: runtimeRef})
	hook := f.statusFunc
	f.mu.Unlock()
	if hook == nil {
		return RuntimeStatus{Source: StateRunning, Sink: StateRunning, Ready: true}, nil
	}
	return hook(ctx, tenantID, runtimeRef)
}

func (f *fakeJobRuntime) Stop(ctx context.Context, tenantID, runtimeRef string) error {
	f.mu.Lock()
	f.stops = append(f.stops, stopCall{TenantID: tenantID, RuntimeRef: runtimeRef})
	hook := f.stopFunc
	f.mu.Unlock()
	if hook == nil {
		return nil
	}
	return hook(ctx, tenantID, runtimeRef)
}

func (f *fakeJobRuntime) submitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submits)
}

func (f *fakeJobRuntime) submitCalls() []submitCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]submitCall(nil), f.submits...)
}

func (f *fakeJobRuntime) stopCalls() []stopCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]stopCall(nil), f.stops...)
}

func (f *fakeJobRuntime) statusCalls() []statusCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]statusCall(nil), f.statuses...)
}

// fakeConnectorReader serves connectors keyed by tenant and connector id.
type fakeConnectorReader struct {
	mu         sync.Mutex
	connectors map[string]*connectordomain.Connector
	err        error
	calls      []string
}

func newFakeConnectorReader() *fakeConnectorReader {
	return &fakeConnectorReader{connectors: make(map[string]*connectordomain.Connector)}
}

func (r *fakeConnectorReader) add(connector *connectordomain.Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[connector.TenantID+"/"+connector.ID] = connector
}

func (r *fakeConnectorReader) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *fakeConnectorReader) GetConnector(ctx context.Context, tenantID, id string) (*connectordomain.Connector, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, tenantID+"/"+id)
	if r.err != nil {
		return nil, r.err
	}
	connector, ok := r.connectors[tenantID+"/"+id]
	if !ok {
		return nil, connectordomain.ErrNotFound
	}
	return connector, nil
}

type verifyResult struct {
	rows int64
	done bool
	err  error
}

// fakeVerifier returns scripted snapshot results; the last result repeats.
type fakeVerifier struct {
	mu      sync.Mutex
	results []verifyResult
	calls   int
}

func newFakeVerifier(results ...verifyResult) *fakeVerifier {
	return &fakeVerifier{results: results}
}

func (v *fakeVerifier) VerifySnapshot(ctx context.Context, job *domain.SyncJob, runtimeRef string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	index := v.calls
	v.calls++
	if len(v.results) == 0 {
		return 0, true, nil
	}
	if index >= len(v.results) {
		index = len(v.results) - 1
	}
	result := v.results[index]
	return result.rows, result.done, result.err
}

func (v *fakeVerifier) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

// fakeMetrics records hook invocations.
type fakeMetrics struct {
	mu          sync.Mutex
	submitCalls int
	retryCalls  int
	reconciles  int
}

func (m *fakeMetrics) ObserveSubmit(_ time.Duration, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submitCalls++
}

func (m *fakeMetrics) IncRetry(_, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryCalls++
}

func (m *fakeMetrics) ObserveReconcile(_ time.Duration, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reconciles++
}

func cloneJob(job *domain.SyncJob) *domain.SyncJob {
	if job == nil {
		return nil
	}
	clone := *job
	clone.Config = cloneMap(job.Config)
	if job.StartedAt != nil {
		started := *job.StartedAt
		clone.StartedAt = &started
	}
	if job.FinishedAt != nil {
		finished := *job.FinishedAt
		clone.FinishedAt = &finished
	}
	if job.LeaseUntil != nil {
		lease := *job.LeaseUntil
		clone.LeaseUntil = &lease
	}
	return &clone
}

func cloneRun(run *domain.SyncRun) *domain.SyncRun {
	if run == nil {
		return nil
	}
	clone := *run
	clone.Stats = cloneMap(run.Stats)
	started := run.StartedAt
	if started != nil {
		value := *started
		clone.StartedAt = &value
	}
	if run.FinishedAt != nil {
		finished := *run.FinishedAt
		clone.FinishedAt = &finished
	}
	return &clone
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func mergeStats(target, update map[string]any) {
	if target == nil {
		return
	}
	for key, value := range update {
		target[key] = value
	}
}

func newActiveConnector(tenantID, connectorID string) *connectordomain.Connector {
	return &connectordomain.Connector{
		ID:       connectorID,
		TenantID: tenantID,
		Name:     "connector-" + connectorID,
		Type:     "kafka",
		Config:   map[string]any{"dataset_id": "dataset-" + connectorID},
		Status:   connectordomain.StatusActive,
	}
}

func newQueuedJob(tenantID, jobID, connectorID string) *domain.SyncJob {
	createdAt := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	return &domain.SyncJob{
		ID:          jobID,
		TenantID:    tenantID,
		ConnectorID: connectorID,
		Mode:        "full",
		Status:      domain.StatusQueued,
		Config:      map[string]any{},
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
}

func newRunningJob(tenantID, jobID, connectorID, runtimeRef string) *domain.SyncJob {
	job := newQueuedJob(tenantID, jobID, connectorID)
	job.Status = domain.StatusRunning
	job.RuntimeRef = runtimeRef
	job.Attempt = 1
	started := job.CreatedAt.Add(time.Second)
	job.StartedAt = &started
	return job
}
