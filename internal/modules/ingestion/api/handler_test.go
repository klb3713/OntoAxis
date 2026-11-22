package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/ingestion/api"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
)

const testTenant = "11111111-1111-7111-8111-111111111111"

type fakeService struct {
	job      *domain.SyncJob
	jobs     []domain.SyncJob
	runs     []domain.SyncRun
	err      error
	watchCh  chan *domain.SyncJob
	watchErr chan error
	lastConn string
}

func (f *fakeService) CreateJob(_ context.Context, _, connectorID string) (*domain.SyncJob, error) {
	f.lastConn = connectorID
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

func (f *fakeService) GetJob(_ context.Context, _, _ string) (*domain.SyncJob, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

func (f *fakeService) ListJobs(_ context.Context, _, connectorID string, _, _ int32) (*service.ListResult, error) {
	f.lastConn = connectorID
	if f.err != nil {
		return nil, f.err
	}
	return &service.ListResult{Jobs: f.jobs, Page: 1, PerPage: 20, Total: int64(len(f.jobs)), TotalPages: 1}, nil
}

func (f *fakeService) CancelJob(_ context.Context, _, _ string) (*domain.SyncJob, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.job, nil
}

func (f *fakeService) ListRuns(_ context.Context, _, _ string) ([]domain.SyncRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.runs, nil
}

func (f *fakeService) Watch(_ context.Context, _, _ string) (<-chan *domain.SyncJob, <-chan error, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.watchCh, f.watchErr, nil
}

func newRouter(svc api.Service) chi.Router {
	router := chi.NewRouter()
	api.NewHandler(svc).Register(router)
	return router
}

func sampleJob() *domain.SyncJob {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	return &domain.SyncJob{
		ID:          "22222222-2222-7222-8222-222222222222",
		TenantID:    testTenant,
		ConnectorID: "33333333-3333-7333-8333-333333333333",
		Mode:        "full",
		Status:      domain.StatusQueued,
		Config:      map[string]any{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestCreateJobRequiresTenant(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/jobs", strings.NewReader(`{"connector_id":"x"}`))
	rec := httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateJobRejectsExternalTenantHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/jobs", strings.NewReader(`{"connector_id":"x"}`))
	req.Header.Set("X-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// Presence of the external header is rejected even when its value is empty.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/jobs", strings.NewReader(`{"connector_id":"x"}`))
	req.Header.Set("X-Tenant-Id", "")
	rec = httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty external header status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateJobReturnsQueuedJob(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/jobs", strings.NewReader(`{"connector_id":"33333333-3333-7333-8333-333333333333"}`))
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{job: sampleJob()}).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "queued" {
		t.Fatalf("status = %q, want queued", body.Status)
	}
}

func TestListJobsRejectsPerPageAboveMaximum(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs?per_page=101", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListJobsPassesConnectorFilter(t *testing.T) {
	t.Parallel()
	svc := &fakeService{jobs: []domain.SyncJob{*sampleJob()}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs?connector_id=33333333-3333-7333-8333-333333333333&page=2&per_page=5", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if svc.lastConn != "33333333-3333-7333-8333-333333333333" {
		t.Fatalf("connector filter = %q", svc.lastConn)
	}
	var body struct {
		Jobs []map[string]any `json:"jobs"`
		Page map[string]any   `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Jobs) != 1 {
		t.Fatalf("jobs length = %d", len(body.Jobs))
	}
}

func TestGetJobNotFound(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs/"+uuid.NewString(), nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{err: domain.ErrNotFound}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCancelJobConflict(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingestion/jobs/"+uuid.NewString()+"/cancel", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{err: errors.Join(domain.ErrConflict, domain.ErrInvalidTransition)}).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWatchEmitsTerminalEvent(t *testing.T) {
	t.Parallel()
	job := sampleJob()
	job.Status = domain.StatusSucceeded
	updates := make(chan *domain.SyncJob, 1)
	errs := make(chan error)
	updates <- job
	close(updates)
	close(errs)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs/"+job.ID+"/watch", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{watchCh: updates, watchErr: errs}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "event: status") || !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("watch body = %q", rec.Body.String())
	}
}

func TestListRuns(t *testing.T) {
	t.Parallel()
	run := domain.SyncRun{
		ID:        uuid.NewString(),
		TenantID:  testTenant,
		JobID:     uuid.NewString(),
		Status:    domain.StatusQueued,
		Stats:     map[string]any{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs/"+uuid.NewString()+"/runs", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{runs: []domain.SyncRun{run}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"runs"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestInvalidPageRejected(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs?page=0", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", testTenant)
	rec := httptest.NewRecorder()
	newRouter(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
