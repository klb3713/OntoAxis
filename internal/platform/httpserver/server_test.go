package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/module"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

type fakeModule struct{ ready error }

func (f fakeModule) Name() string                                 { return "demo" }
func (f fakeModule) Dependencies() []string                       { return nil }
func (f fakeModule) Migrate(context.Context, *pgxpool.Pool) error { return nil }
func (f fakeModule) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/demo", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}
func (f fakeModule) Ready(context.Context) error { return f.ready }

func TestHealthAnd404(t *testing.T) {
	s := New(Options{DB: fakeDB{}, Modules: []module.Module{fakeModule{}}})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["service"] != "ontoaxis-core" {
		t.Fatalf("unexpected body: %v", body)
	}
	req = httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("404 body = %s", rec.Body.String())
	}
}

func TestReadyFailure(t *testing.T) {
	s := New(Options{DB: fakeDB{err: context.DeadlineExceeded}, Modules: []module.Module{fakeModule{ready: context.DeadlineExceeded}}})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d", rec.Code)
	}
}

func TestModuleRouteRegistered(t *testing.T) {
	s := New(Options{Modules: []module.Module{fakeModule{}}})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/demo", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("module route status = %d", rec.Code)
	}
}
