package ontology

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	platformmodule "github.com/klb3713/ontoaxis/internal/platform/module"
)

func TestModuleIdentityAndDependencies(t *testing.T) {
	module := New()
	if module.Name() != "ontology" {
		t.Fatalf("Name() = %q, want ontology", module.Name())
	}
	dependencies := module.Dependencies()
	if len(dependencies) != 1 || dependencies[0] != "tenancy" {
		t.Fatalf("Dependencies() = %v, want [tenancy]", dependencies)
	}
}

func TestModuleMigrateRejectsNilPool(t *testing.T) {
	if err := New().Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate(nil) error = nil, want error")
	}
}

func TestModuleNotReadyWithoutPool(t *testing.T) {
	if err := New().Ready(context.Background()); err == nil {
		t.Fatal("Ready() error = nil, want error")
	}
}

func TestModuleRegistersFailClosedRoutes(t *testing.T) {
	router := chi.NewRouter()
	New().RegisterRoutes(router)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ontology/object-types", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] == nil {
		t.Fatalf("error envelope missing: %s", recorder.Body.String())
	}
}

func TestModuleDoesNotExposeProjectRoutes(t *testing.T) {
	router := chi.NewRouter()
	New().RegisterRoutes(router)

	for _, path := range []string{"/api/v1/ontology/projects", "/api/v1/ontology/project-resources"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, recorder.Code)
		}
	}
}

func TestModuleImplementsModuleContract(t *testing.T) {
	var _ platformmodule.Module = New()
}
