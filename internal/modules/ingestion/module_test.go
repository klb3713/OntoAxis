package ingestion_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	connectordomain "github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/domain"
	ingestionruntime "github.com/klb3713/ontoaxis/internal/modules/ingestion/runtime"
	"github.com/klb3713/ontoaxis/internal/modules/ingestion/service"
)

type moduleStore struct {
	jobs []domain.SyncJob
	runs []domain.SyncRun
}

func (s *moduleStore) CreateJob(_ context.Context, job *domain.SyncJob, run *domain.SyncRun) error {
	s.jobs = append(s.jobs, *job)
	s.runs = append(s.runs, *run)
	return nil
}

func (s *moduleStore) GetJob(_ context.Context, _, id string) (*domain.SyncJob, error) {
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			job := s.jobs[i]
			return &job, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *moduleStore) ListJobs(_ context.Context, _, _ string, _, _ int32) ([]domain.SyncJob, int64, error) {
	return s.jobs, int64(len(s.jobs)), nil
}

func (s *moduleStore) GetLatestRun(_ context.Context, _, _ string) (*domain.SyncRun, error) {
	if len(s.runs) == 0 {
		return nil, domain.ErrNotFound
	}
	run := s.runs[len(s.runs)-1]
	return &run, nil
}

func (s *moduleStore) UpdateJobAndRun(_ context.Context, _ *domain.SyncJob, _ domain.JobStatus, _ *domain.SyncRun) error {
	return nil
}

func (s *moduleStore) ListRuns(_ context.Context, _, _ string) ([]domain.SyncRun, error) {
	return s.runs, nil
}

type moduleReader struct{}

func (moduleReader) GetConnector(_ context.Context, tenantID, id string) (*connectordomain.Connector, error) {
	return &connectordomain.Connector{ID: id, TenantID: tenantID, Status: connectordomain.StatusActive}, nil
}

func TestModuleContract(t *testing.T) {
	t.Parallel()
	mod := ingestion.New()
	if mod.Name() != "ingestion" {
		t.Fatalf("Name = %q", mod.Name())
	}
	deps := mod.Dependencies()
	if len(deps) != 1 || deps[0] != "connectors" {
		t.Fatalf("Dependencies = %v", deps)
	}
	if err := mod.Ready(context.Background()); err == nil {
		t.Fatal("Ready before initialization should fail")
	}
	if err := mod.Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate with nil pool should fail")
	}
}

func TestModuleRoutesWithStore(t *testing.T) {
	t.Parallel()
	mod := ingestion.NewWithStore(&moduleStore{}, service.WithConnectorReader(moduleReader{}))
	if err := mod.Ready(context.Background()); err != nil {
		t.Fatalf("Ready = %v", err)
	}
	router := chi.NewRouter()
	mod.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion/jobs", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", "11111111-1111-7111-8111-111111111111")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if mod.Service() == nil {
		t.Fatal("Service should be initialized")
	}
}

func TestModuleDoesNotInitializeWithoutStore(t *testing.T) {
	t.Parallel()
	mod := ingestion.New(service.WithClock(func() time.Time { return time.Unix(0, 0).UTC() }))
	if mod.Service() != nil {
		t.Fatal("Service should be nil without store")
	}
}

func TestModuleNewRuntimeFailsClosedWithoutRuntimeStore(t *testing.T) {
	m := ingestion.NewWithStore(&moduleStore{}, service.WithConnectorReader(moduleReader{}))
	if _, err := m.NewRuntime(nil, nil, ingestionruntime.Options{}); err == nil {
		t.Fatal("NewRuntime with a non-runtime store must fail")
	}
}
