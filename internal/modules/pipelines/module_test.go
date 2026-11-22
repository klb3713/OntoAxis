package pipelines_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/api"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/kestra"
	"github.com/klb3713/ontoaxis/internal/modules/pipelines/repo"
)

const tenantID = "11111111-1111-4111-8111-111111111111"

func TestModuleMetadataAndServices(t *testing.T) {
	store := repo.NewMemoryStore()
	adapter := kestra.NewMemoryAdapter()
	module := pipelines.New(nil,
		pipelines.WithStore(store),
		pipelines.WithKestraAdapter(adapter),
		pipelines.WithTenantResolver(api.HeaderTenantResolver{}),
	)
	if module.Name() != "pipelines" {
		t.Fatalf("Name = %q", module.Name())
	}
	dependencies := module.Dependencies()
	if len(dependencies) != 1 || dependencies[0] != "datasets" {
		t.Fatalf("Dependencies = %v", dependencies)
	}
	pipelineService, scheduleService := module.Services()
	if pipelineService == nil || scheduleService == nil {
		t.Fatalf("Services returned nil")
	}
	if err := module.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := module.Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate with nil pool should fail")
	}
}

func TestModuleRouteMountsCollectionPath(t *testing.T) {
	module := pipelines.New(nil,
		pipelines.WithStore(repo.NewMemoryStore()),
		pipelines.WithKestraAdapter(kestra.NewMemoryAdapter()),
		pipelines.WithTenantResolver(api.HeaderTenantResolver{}),
	)
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", strings.NewReader(`{
		"name":"module_route","type":"batch","definition":{}
	}`))
	request.Header.Set("X-Ontoaxis-Tenant-Id", tenantID)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/pipelines = %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", nil)
	request.Header.Set("X-Ontoaxis-Tenant-Id", tenantID)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/pipelines = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestModuleWithoutStoreFailsClosed(t *testing.T) {
	module := pipelines.New(nil)
	if err := module.Ready(context.Background()); err == nil {
		t.Fatal("Ready without a store should fail")
	}
}
