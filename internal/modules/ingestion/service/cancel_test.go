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

type stopCall struct {
	TenantID   string
	RuntimeRef string
}

type fakeStopper struct {
	mu    sync.Mutex
	calls []stopCall
	err   error
}

func (s *fakeStopper) Stop(_ context.Context, tenantID, runtimeRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stopCall{TenantID: tenantID, RuntimeRef: runtimeRef})
	return s.err
}

func (s *fakeStopper) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *fakeStopper) callsSnapshot() []stopCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stopCall(nil), s.calls...)
}

// setRuntimeState rewrites the stored job's lifecycle fields, simulating the
// dispatcher or reconciler having changed them.
func (s *fakeStore) setRuntimeState(jobID string, status domain.JobStatus, runtimeRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.Status = status
	job.RuntimeRef = runtimeRef
}

func newServiceWithStopper(t *testing.T, store service.Store, reader service.ConnectorReader, clock func() time.Time, stopper service.RuntimeStopper) service.Service {
	t.Helper()
	opts := []service.Option{service.WithConnectorReader(reader), service.WithRuntimeStopper(stopper)}
	if clock != nil {
		opts = append(opts, service.WithClock(clock))
	}
	return service.NewService(store, opts...)
}

func TestCancelRunningJobStopsRuntimeAndCancels(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "runtime-1")

	cancelled, err := svc.CancelJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if stopper.callCount() != 1 {
		t.Fatalf("stop calls = %d, want 1", stopper.callCount())
	}
	call := stopper.callsSnapshot()[0]
	if call.TenantID != tenantID || call.RuntimeRef != "runtime-1" {
		t.Fatalf("stop call = %+v, want tenant/runtime-1", call)
	}
	stored, err := store.GetJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if stored.Status != domain.StatusCancelled {
		t.Fatalf("stored status = %q, want cancelled", stored.Status)
	}
}

func TestCancelRunningJobKeepsRunningWhenStopFails(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{err: errors.New("runtime API unavailable")}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "runtime-1")

	if _, err := svc.CancelJob(context.Background(), tenantID, job.ID); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("CancelJob error = %v, want ErrUnavailable", err)
	}
	stored, err := store.GetJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if stored.Status != domain.StatusRunning {
		t.Fatalf("stored status = %q, want running (cancel must not be reported)", stored.Status)
	}
	if stopper.callCount() != 1 {
		t.Fatalf("stop calls = %d, want 1", stopper.callCount())
	}
}

func TestCancelQueuedJobWithoutRuntimeDoesNotStop(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	cancelled, err := svc.CancelJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if stopper.callCount() != 0 {
		t.Fatalf("stop calls = %d, want 0 (nothing submitted yet)", stopper.callCount())
	}
}

func TestCancelQueuedJobWithRuntimeStopsRuntime(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusQueued, "runtime-submitting")

	cancelled, err := svc.CancelJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if stopper.callCount() != 1 {
		t.Fatalf("stop calls = %d, want 1", stopper.callCount())
	}
	if ref := stopper.callsSnapshot()[0].RuntimeRef; ref != "runtime-submitting" {
		t.Fatalf("stopped runtime = %q, want runtime-submitting", ref)
	}
}

func TestCancelWithRuntimeRequiresStopper(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	svc := newService(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "runtime-1")

	if _, err := svc.CancelJob(context.Background(), tenantID, job.ID); !errors.Is(err, service.ErrUnavailable) {
		t.Fatalf("CancelJob error = %v, want ErrUnavailable without a stopper", err)
	}
	stored, err := store.GetJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if stored.Status != domain.StatusRunning {
		t.Fatalf("stored status = %q, want running", stored.Status)
	}
}

func TestCancelRunningJobWithoutRuntimeCancelsDirectly(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "")

	cancelled, err := svc.CancelJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if stopper.callCount() != 0 {
		t.Fatalf("stop calls = %d, want 0", stopper.callCount())
	}
}

func TestCancelTerminalJobDoesNotStopRuntime(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "runtime-1")
	if _, err := svc.CancelJob(context.Background(), tenantID, job.ID); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}

	if _, err := svc.CancelJob(context.Background(), tenantID, job.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second CancelJob error = %v, want ErrConflict", err)
	}
	if stopper.callCount() != 1 {
		t.Fatalf("stop calls = %d, want 1 (terminal cancel must not stop)", stopper.callCount())
	}
}

func TestCancelJobIsTenantScoped(t *testing.T) {
	t.Parallel()
	connectorID := uuid.NewString()
	store := newFakeStore()
	stopper := &fakeStopper{}
	svc := newServiceWithStopper(t, store, &fakeReader{connectors: map[string]*connectordomain.Connector{connectorID: activeConnector(connectorID)}}, nil, stopper)
	job, err := svc.CreateJob(context.Background(), tenantID, connectorID)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	store.setRuntimeState(job.ID, domain.StatusRunning, "runtime-1")

	otherTenant := uuid.NewString()
	if _, err := svc.CancelJob(context.Background(), otherTenant, job.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("CancelJob(other tenant) error = %v, want ErrNotFound", err)
	}
	if stopper.callCount() != 0 {
		t.Fatalf("stop calls = %d, want 0", stopper.callCount())
	}
	stored, err := store.GetJob(context.Background(), tenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if stored.Status != domain.StatusRunning {
		t.Fatalf("stored status = %q, want running", stored.Status)
	}
}
