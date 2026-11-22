package authn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func newTestEdgeAuthenticator(trustGateway bool, verifier Verifier) *EdgeAuthenticator {
	return NewEdgeAuthenticator(Config{TrustGateway: trustGateway}, verifier)
}

type capturedContext struct {
	tc     TenantContext
	ok     bool
	called bool
}

func serveMiddleware(auth *EdgeAuthenticator, req *http.Request) (*httptest.ResponseRecorder, *capturedContext) {
	rec := httptest.NewRecorder()
	capture := &capturedContext{}
	auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.called = true
		capture.tc, capture.ok = FromContext(r.Context())
	})).ServeHTTP(rec, req)
	return rec, capture
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorDetail {
	t.Helper()
	var detail ErrorDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode error response: %v (body=%s)", err, rec.Body.String())
	}
	return detail
}

func TestAuthenticateValidToken(t *testing.T) {
	fake := &FakeVerifier{Claims: validClaims()}
	auth := newTestEdgeAuthenticator(false, fake)

	tc, err := auth.Authenticate(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("Authenticate error: %v", err)
	}
	if tc.TenantID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("TenantID = %s", tc.TenantID)
	}
	if fake.Calls() != 1 || fake.LastToken() != "token-1" {
		t.Fatalf("verifier calls=%d token=%q", fake.Calls(), fake.LastToken())
	}
}

func TestAuthenticateEmptyToken(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	_, err := auth.Authenticate(context.Background(), " ")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateVerifierError(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Err: errors.New("bad signature")})
	_, err := auth.Authenticate(context.Background(), "token")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateMissingTenant(t *testing.T) {
	claims := validClaims()
	claims.TenantID = ""
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: claims})
	_, err := auth.Authenticate(context.Background(), "token")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestAuthenticateInvalidTenant(t *testing.T) {
	claims := validClaims()
	claims.TenantID = "invalid"
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: claims})
	_, err := auth.Authenticate(context.Background(), "token")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want ErrPermissionDenied", err)
	}
}

func TestMiddlewareDirectValidToken(t *testing.T) {
	fake := &FakeVerifier{Claims: validClaims()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token-1")
	req.Header.Set("X-Request-Id", "req-1")

	rec, capture := serveMiddleware(newTestEdgeAuthenticator(false, fake), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !capture.called || !capture.ok {
		t.Fatalf("handler called=%v ok=%v", capture.called, capture.ok)
	}
	if capture.tc.TenantID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("TenantID = %s", capture.tc.TenantID)
	}
	if capture.tc.RequestID != "req-1" {
		t.Fatalf("RequestID = %q, want req-1", capture.tc.RequestID)
	}
}

func TestMiddlewareDirectMissingToken(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if capture.called {
		t.Fatalf("handler must not run without a token")
	}
	if detail := decodeError(t, rec); detail.Error.Code != CodeUnauthenticated {
		t.Fatalf("code = %q", detail.Error.Code)
	}
}

func TestMiddlewareDirectInvalidToken(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Err: errors.New("invalid")})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer broken")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if capture.called {
		t.Fatalf("handler must not run with an invalid token")
	}
}

func TestMiddlewareDirectRejectsInternalHeaders(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if capture.called {
		t.Fatalf("handler must not run with forged internal headers")
	}
	if detail := decodeError(t, rec); detail.Error.Code != CodePermissionDenied {
		t.Fatalf("code = %q", detail.Error.Code)
	}
}

func TestMiddlewareDirectRejectsClientIdentityHeaders(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-User-Id", "22222222-2222-2222-2222-222222222222")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403 and handler not called", rec.Code, capture.called)
	}
}

func TestMiddlewareTenantHeaderMatchesJWT(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", "11111111-1111-1111-1111-111111111111")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusOK || !capture.ok {
		t.Fatalf("status=%d ok=%v, want 200", rec.Code, capture.ok)
	}
}

func TestMiddlewareTenantHeaderMismatchNonAdmin(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", "33333333-3333-3333-3333-333333333333")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403 and handler not called", rec.Code, capture.called)
	}
}

func TestMiddlewareTenantHeaderPlatformAdminInOrg(t *testing.T) {
	claims := validClaims()
	claims.Roles = []string{"platform-admin"}
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: claims})
	target := "33333333-3333-3333-3333-333333333333"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", target)
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusOK || !capture.ok {
		t.Fatalf("status=%d ok=%v, want 200", rec.Code, capture.ok)
	}
	if capture.tc.TenantID.String() != target {
		t.Fatalf("TenantID = %s, want %s", capture.tc.TenantID, target)
	}
}

func TestMiddlewareTenantHeaderPlatformAdminOutsideOrg(t *testing.T) {
	claims := validClaims()
	claims.Roles = []string{"platform-admin"}
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: claims})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", "99999999-9999-9999-9999-999999999999")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareTenantHeaderInvalidUUID(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", "not-a-uuid")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareGatewayValidHeaders(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datasets", nil)
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	req.Header.Set(internalOrgIDsHeader, "33333333-3333-3333-3333-333333333333, 55555555-5555-5555-5555-555555555555")
	req.Header.Set(internalWorkspaceIDsHeader, "44444444-4444-4444-4444-444444444444")
	req.Header.Set(internalRolesHeader, "viewer, platform-admin, viewer, bogus")
	req.Header.Set(internalRequestIDHeader, "gw-req-1")

	rec, capture := serveMiddleware(auth, req)
	if rec.Code != http.StatusOK || !capture.ok {
		t.Fatalf("status=%d ok=%v, want 200", rec.Code, capture.ok)
	}
	if len(capture.tc.OrgIDs) != 2 || len(capture.tc.WorkspaceIDs) != 1 {
		t.Fatalf("org/workspace ids = %+v", capture.tc)
	}
	if want := []string{"platform-admin", "viewer"}; !reflect.DeepEqual(capture.tc.Roles, want) {
		t.Fatalf("Roles = %v, want %v", capture.tc.Roles, want)
	}
	if capture.tc.RequestID != "gw-req-1" {
		t.Fatalf("RequestID = %q", capture.tc.RequestID)
	}
}

func TestMiddlewareGatewayRejectsAuthorizationMix(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
	if detail := decodeError(t, rec); detail.Error.Code != CodePermissionDenied {
		t.Fatalf("code = %q", detail.Error.Code)
	}
}

func TestMiddlewareGatewayMissingTenant(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareGatewayInvalidTenantUUID(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	req.Header.Set(internalTenantHeader, "bad-tenant")
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareGatewayRejectsClientTenantHeader(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	req.Header.Set("X-Tenant-Id", "99999999-9999-9999-9999-999999999999")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareRequestIDFromTraceparent(t *testing.T) {
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: validClaims()})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/actions", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusOK || !capture.ok {
		t.Fatalf("status=%d ok=%v, want 200", rec.Code, capture.ok)
	}
	if want := "4bf92f3577b34da6a3ce929d0e0e4736"; capture.tc.RequestID != want {
		t.Fatalf("RequestID = %q, want %q", capture.tc.RequestID, want)
	}
}

func TestMiddlewareGatewayRejectsInvalidOrgUUID(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ontology", nil)
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	req.Header.Set(internalUserHeader, "22222222-2222-2222-2222-222222222222")
	req.Header.Set(internalOrgIDsHeader, "not-a-uuid")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestMiddlewareGatewayMissingUser(t *testing.T) {
	auth := newTestEdgeAuthenticator(true, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ontology", nil)
	req.Header.Set(internalTenantHeader, "11111111-1111-1111-1111-111111111111")
	rec, capture := serveMiddleware(auth, req)

	if rec.Code != http.StatusForbidden || capture.called {
		t.Fatalf("status=%d called=%v, want 403", rec.Code, capture.called)
	}
}

func TestTenantHeaderSelectionWithNilOrgIDs(t *testing.T) {
	claims := validClaims()
	claims.Roles = []string{"platform-admin"}
	claims.OrgIDs = nil
	auth := newTestEdgeAuthenticator(false, &FakeVerifier{Claims: claims})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Tenant-Id", "11111111-1111-1111-1111-111111111111")
	rec, capture := serveMiddleware(auth, req)

	// Selecting the token's own tenant is always allowed even with no orgs.
	if rec.Code != http.StatusOK || !capture.ok {
		t.Fatalf("status=%d ok=%v, want 200", rec.Code, capture.ok)
	}
}
