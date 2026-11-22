package connectors_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/connectors"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

func TestModuleContract(t *testing.T) {
	t.Parallel()
	var _ module.Module = connectors.New()
	m := connectors.New()
	if m.Name() != "connectors" {
		t.Fatalf("Name() = %q", m.Name())
	}
	if !reflect.DeepEqual(m.Dependencies(), []string{"tenancy"}) {
		t.Fatalf("Dependencies() = %v", m.Dependencies())
	}
	if err := m.Ready(t.Context()); err == nil {
		t.Fatal("Ready() before store initialization must return an error")
	}
}

func TestModuleRegistersRoutes(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	connectors.New().RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", "11111111-1111-7111-8111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("route not registered: status = %d", rec.Code)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 before initialization", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/connectors/019c0000-0000-7000-8000-000000000001/sync", nil)
	req.Header.Set("X-Ontoaxis-Tenant-Id", "11111111-1111-7111-8111-111111111111")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("sync route not registered: status = %d", rec.Code)
	}
}
