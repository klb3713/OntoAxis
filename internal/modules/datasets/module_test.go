package datasets_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/datasets"
	"github.com/klb3713/ontoaxis/internal/modules/datasets/repo"
	"github.com/klb3713/ontoaxis/internal/platform/httpserver"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

func TestModuleMetadata(t *testing.T) {
	module := datasets.New()
	if module.Name() != "datasets" {
		t.Fatalf("Name() = %q, want datasets", module.Name())
	}
	dependencies := module.Dependencies()
	if len(dependencies) != 1 || dependencies[0] != "tenancy" {
		t.Fatalf("Dependencies() = %v, want [tenancy]", dependencies)
	}
}

func TestModuleReadyAndRoutes(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7()).String()
	module := datasets.New(datasets.WithRepository(repo.NewMemoryStore()))
	if err := module.Ready(t.Context()); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant status = %d, want 401", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	request.Header.Set("X-Ontoaxis-Tenant-Id", tenantID)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("with tenant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestModuleReadyWithoutRepository(t *testing.T) {
	module := datasets.New()
	if err := module.Ready(t.Context()); err == nil {
		t.Fatal("Ready() error = nil, want configuration error")
	}
}

func TestModulePlatformServerReturnsJSONErrors(t *testing.T) {
	catalog := datasets.New(datasets.WithRepository(repo.NewMemoryStore()))
	server := httpserver.New(httpserver.Options{Modules: []module.Module{catalog}})

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/not-a-route", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", recorder.Code)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", recorder.Body.String(), err)
	}
	if payload.Error.Code != "not_found" {
		t.Fatalf("unknown route error code = %q, want not_found", payload.Error.Code)
	}

	recorder = httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing tenant status = %d, want 401", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", recorder.Body.String(), err)
	}
	if payload.Error.Code != "unauthorized" {
		t.Fatalf("missing tenant error code = %q, want unauthorized", payload.Error.Code)
	}
}

func TestModuleMigrateRejectsNilPool(t *testing.T) {
	catalog := datasets.New()
	if err := catalog.Migrate(t.Context(), nil); err == nil {
		t.Fatal("Migrate(nil) error = nil, want configuration error")
	}
}
