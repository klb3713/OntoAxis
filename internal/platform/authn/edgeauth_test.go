package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEdgeAuthHandlerValidToken(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/internal/edge-auth", nil)
	req.Header.Set("Authorization", "Bearer token-1")
	req.Header.Set("X-Request-Id", "req-edge-1")
	rec := httptest.NewRecorder()

	auth.EdgeAuthHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(internalTenantHeader); got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("tenant header = %q", got)
	}
	if got := rec.Header().Get(internalUserHeader); got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("user header = %q", got)
	}
	if got := rec.Header().Get(internalOrgIDsHeader); got != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("org header = %q", got)
	}
	if got := rec.Header().Get(internalWorkspaceIDsHeader); got != "44444444-4444-4444-4444-444444444444" {
		t.Fatalf("workspace header = %q", got)
	}
	if got := rec.Header().Get(internalRolesHeader); got != "member,viewer" {
		t.Fatalf("roles header = %q", got)
	}
	if got := rec.Header().Get(internalRequestIDHeader); got != "req-edge-1" {
		t.Fatalf("request id header = %q", got)
	}
}

func TestEdgeAuthHandlerMissingToken(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/internal/edge-auth", nil)
	rec := httptest.NewRecorder()

	auth.EdgeAuthHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if detail := decodeError(t, rec); detail.Error.Code != CodeUnauthenticated {
		t.Fatalf("code = %q", detail.Error.Code)
	}
}

func TestEdgeAuthHandlerRejectsInternalHeaders(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/internal/edge-auth", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()

	auth.EdgeAuthHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
