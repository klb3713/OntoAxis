package httpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/httpserver"
	"github.com/klb3713/ontoaxis/internal/platform/module"
)

type authTestModule struct{}

func (authTestModule) Name() string                                 { return "auth-test" }
func (authTestModule) Dependencies() []string                       { return nil }
func (authTestModule) Migrate(context.Context, *pgxpool.Pool) error { return nil }
func (authTestModule) Ready(context.Context) error                  { return nil }
func (authTestModule) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/tenancy/organizations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"items":[]}`)
	})
}

func newAuthTestServer(t *testing.T, auth *authn.EdgeAuthenticator) *httptest.Server {
	t.Helper()
	server := httpserver.New(httpserver.Options{
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modules:      []module.Module{authTestModule{}},
		Authenticate: auth.Middleware,
	})
	ts := httptest.NewServer(server.Router())
	t.Cleanup(ts.Close)
	return ts
}

func doGet(t *testing.T, url string, headers map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestDirectModeRejectsMissingTokenAndAcceptsValidToken(t *testing.T) {
	claims := authn.Claims{
		Issuer:   authn.DefaultIssuer,
		Audience: []string{authn.DefaultAudience},
		Subject:  "22222222-2222-4222-8222-222222222222",
		TenantID: "11111111-1111-4111-8111-111111111111",
		Email:    "admin@ontoaxis.local",
		Roles:    []string{"member"},
	}
	auth := authn.NewEdgeAuthenticator(authn.Config{}, &authn.FakeVerifier{Claims: claims})
	ts := newAuthTestServer(t, auth)
	url := ts.URL + "/api/v1/tenancy/organizations"

	if got := doGet(t, url, nil); got != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", got)
	}
	if got := doGet(t, url, map[string]string{"Authorization": "Bearer test-token"}); got != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", got)
	}
}

func TestGatewayModeAcceptsTrustedHeadersAndRejectsAuthorization(t *testing.T) {
	auth := authn.NewEdgeAuthenticator(authn.Config{TrustGateway: true}, nil)
	ts := newAuthTestServer(t, auth)
	url := ts.URL + "/api/v1/tenancy/organizations"

	trusted := map[string]string{
		"x-ontoaxis-tenant-id": "11111111-1111-4111-8111-111111111111",
		"x-ontoaxis-user-id":   "22222222-2222-4222-8222-222222222222",
		"x-ontoaxis-roles":     "member",
	}
	if got := doGet(t, url, trusted); got != http.StatusOK {
		t.Fatalf("trusted headers status = %d, want 200", got)
	}
	if got := doGet(t, url, map[string]string{"Authorization": "Bearer test-token"}); got != http.StatusForbidden {
		t.Fatalf("authorization in gateway mode status = %d, want 403", got)
	}
}

func TestDirectModeRejectsForgedGatewayHeaders(t *testing.T) {
	auth := authn.NewEdgeAuthenticator(authn.Config{}, &authn.FakeVerifier{})
	ts := newAuthTestServer(t, auth)
	url := ts.URL + "/api/v1/tenancy/organizations"

	if got := doGet(t, url, map[string]string{"x-ontoaxis-tenant-id": "11111111-1111-4111-8111-111111111111"}); got != http.StatusForbidden {
		t.Fatalf("forged header status = %d, want 403", got)
	}
}
