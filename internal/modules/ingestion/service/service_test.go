package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
)

const tenantID = "11111111-1111-7111-8111-111111111111"

type fakeStore struct {
	mu   sync.Mutex
	jobs map[string]*domain.SyncJob
	runs map[string]*domain.SyncRun
}

func newFakeStore() *fakeStore {
	return &fakeStore{jobs: map[string]*domain.SyncJob{}, runs: map[string]*domain.SyncRun{}}
}

func (s *fakeStore) CreateJob(_ context.Context, job *domain.SyncJob, run *domain.SyncRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = cloneJob(job)
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *fakeStore) GetJob(_ context.Context, tenantID, id string) (*domain.SyncJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	return cloneJob(job), nil
}

func (s *fakeStore) ListJobs(_ context.Context, tenantID, connectorID string, limit, offset int32) ([]domain.SyncJob, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]domain.SyncJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.TenantID != tenantID {
			continue
		}
		if connectorID != "" && job.ConnectorID != connectorID {
			continue
		}
		all = append(all, *cloneJob(job))
	}
	total := int64(len(all))
	start := int(offset)
	if start > len(all) {
		start = len(all)
	}
	end := start + int(limit)
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

func (s *fakeStore) GetLatestRun(_ context.Context, tenantID, jobID string) (*domain.SyncRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *domain.SyncRun
	for _, run := range s.runs {
		if run.TenantID != tenantID || run.JobID != jobID {
			continue
		}
		if latest == nil || run.CreatedAt.After(latest.CreatedAt) {
			latest = run
		}
	}
	if latest == nil {
		return nil, domain.ErrNotFound
	}
	return cloneRun(latest), nil
}

func (s *fakeStore) UpdateJobAndRun(_ context.Context, job *domain.SyncJob, expectedStatus domain.JobStatus, run *domain.SyncRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.jobs[job.ID]
	if !ok || current.TenantID != job.TenantID {
		return domain.ErrNotFound
	}
	if current.Status != expectedStatus {
		return domain.ErrConflict
	}
	s.jobs[job.ID] = cloneJob(job)
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *fakeStore) ListRuns(_ context.Context, tenantID, jobID string) ([]domain.SyncRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.SyncRun, 0)
	for _, run := range s.runs {
		if run.TenantID == tenantID && run.JobID == jobID {
			out = append(out, *cloneRun(run))
		}
	}
	return out, nil
}

func (s *fakeStore) setStatus(jobID string, status domain.JobStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		job.Status = status
	}
}

func cloneJob(job *domain.SyncJob) *domain.SyncJob {
	copied := *job
	if job.Config != nil {
		copied.Config = make(map[string]any, len(job.Config))
		for key, value := range job.Config {
			copied.Config[key] = value
		}
	}
	return &copied
}

func cloneRun(run *domain.SyncRun) *domain.SyncRun {
	copied := *run
	if run.Stats != nil {
		copied.Stats = make(map[string]any, len(run.Stats))
		for key, value := range run.Stats {
			copied.Stats[key] = value
		}
	}
	return &copied
}

type fakeReader struct {
	connectors map[string]*connectordomain.Connector
	err        error
}

func (r *fakeReader) GetConnector(_ context.Context, tenantID, id string) (*connectordomain.Connector, error) {
	if r.err != nil {
		return nil, r.err
	}
	connector, ok := r.connectors[id]
	if !ok || connector.TenantID != tenantID {
		return nil, connectordomain.ErrNotFound
	}
	return connector, nil
}

func activeConnector(connectorID string) *connectordomain.Connector {
	return &connectordomain.Connector{ID: connectorID, TenantID: tenantID, Status: connectordomain.StatusActive}
}

func newService(t *testing.T, store service.Store, reader service.ConnectorReader, clock func() time.Time) service.Service {
	t.Helper()
	opts := []service.Option{service.WithConnectorReader(reader)}
	if clock != nil {
		opts = append(opts, service.WithClock(clock))
	}
	return service.NewService(store, opts...)
}

func TestCreateJobSuccess(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	svc := newService(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.Status != domain.StatusQueued {
		t.Fatalf("status = %s", job.Status)
	}
	if job.ConnectorID != connectorID || job.TenantID != tenantID {
		t.Fatalf("job = %+v", job)
	}
	if version := uuid.MustParse(job.ID).Version(); version != 7 {
		t.Fatalf("job id version = %d, want 7", version)
	}
	run, err := store.GetLatestRun(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if run.Status != domain.StatusQueued {
		t.Fatalf("initial run status = %s", run.Status)
	}
}

func TestCreateJobRejectsMissingTenant(t *testing.T) {
	t.Parallel()
	svc := newService(t, newFakeStore(), &fakeReader{}, nil)
	if _, err := svc.CreateJob(context.Background(), "", uuid.NewString()); !errors.Is(err, service.ErrInvalidTenant) {
		t.Fatalf("error = %v, want ErrInvalidTenant", err)
	}
}

func TestCreateJobRejectsPausedConnector(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	connector := activeConnector(connectorID)
	connector.Status = connectordomain.StatusPaused
	svc := newService(t, newFakeStore(), &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: connector}}, nil)
	if _, err := svc.CreateJob(context.Background(), tenantID, connectorID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}

func TestCreateJobUnknownConnector(t *testing.T) {
	t.Parallel()
	svc := newService(t, newFakeStore(), &fakeReader{connectors: map[string]*connectordomain.Connector{}}, nil)
	if _, err := svc.CreateJob(context.Background(), tenantID, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCancelTerminalJobConflicts(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	svc := newService(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if _, err := svc.MarkRunning(context.Background(), tenantID, job.ID); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if _, err := svc.MarkSucceeded(context.Background(), tenantID, job.ID, map[string]any{"rows": 3}); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if _, err := svc.CancelJob(context.Background(), tenantID, job.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("CancelJob error = %v, want ErrConflict", err)
	}
}

func TestStateTransitionsProjectRun(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	store := newFakeStore()
	svc := newService(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, func() time.Time { return now })
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if _, err := svc.MarkRunning(context.Background(), tenantID, job.ID); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	now = now.Add(time.Minute)
	finished, err := svc.MarkSucceeded(context.Background(), tenantID, job.ID, map[string]any{"rows": 10})
	if err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	if finished.Status != domain.StatusSucceeded || finished.FinishedAt == nil {
		t.Fatalf("finished job = %+v", finished)
	}
	runs, err := svc.ListRuns(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != domain.StatusSucceeded || runs[0].Stats["rows"] != 10 {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestWatchTerminalJobClosesImmediately(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	svc := newService(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if _, err := svc.MarkRunning(context.Background(), tenantID, job.ID); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if _, err := svc.MarkFailed(context.Background(), tenantID, job.ID, "boom", nil); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, errs, err := svc.Watch(ctx, tenantID, job.ID)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	first := <-updates
	if first.Status != domain.StatusFailed {
		t.Fatalf("first status = %s", first.Status)
	}
	if _, open := <-updates; open {
		t.Fatal("watch channel should be closed for terminal job")
	}
	if err := <-errs; err != nil {
		t.Fatalf("watch error = %v", err)
	}
}

func TestWatchSeesStatusChange(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	svc := service.NewService(store,
		service.WithConnectorReader(&fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}),
		service.WithWatchInterval(10*time.Millisecond))
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	updates, _, err := svc.Watch(ctx, tenantID, job.ID)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if first := <-updates; first.Status != domain.StatusQueued {
		t.Fatalf("first status = %s", first.Status)
	}
	store.setStatus(job.ID, domain.StatusRunning)
	select {
	case update := <-updates:
		if update.Status != domain.StatusRunning {
			t.Fatalf("update status = %s", update.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for status update")
	}
}
