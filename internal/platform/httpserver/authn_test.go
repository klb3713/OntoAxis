package httpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/httpserver"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

type authDemoModule struct{}

func (authDemoModule) Name() string                                 { return "auth-demo" }
func (authDemoModule) Dependencies() []string                       { return nil }
func (authDemoModule) Migrate(context.Context, *pgxpool.Pool) error { return nil }
func (authDemoModule) Ready(context.Context) error                  { return nil }
func (authDemoModule) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/auth-demo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestModuleRoutesRequireTrustedIdentity(t *testing.T) {
	auth := authn.NewEdgeAuthenticator(authn.Config{TrustGateway: true}, nil)
	srv := httpserver.New(httpserver.Options{
		ServiceName:  "test",
		Version:      "test",
		HTTPAddr:     "127.0.0.1:0",
		MetricsAddr:  "127.0.0.1:0",
		Modules:      []module.Module{authDemoModule{}},
		Authenticate: auth.Middleware,
	})
	router := srv.Router()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth-demo", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without identity: got %d want 403", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-demo", nil)
	req.Header.Set("x-ontoaxis-tenant-id", "11111111-1111-4111-8111-111111111111")
	req.Header.Set("x-ontoaxis-user-id", "22222222-2222-4222-8222-222222222222")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with trusted identity: got %d want 200: %s", rec.Code, rec.Body.String())
	}
}
