package authn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAuthErrorUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenancy/orgs", nil)
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()

	writeAuthError(rec, req, http.StatusUnauthorized, CodeUnauthenticated, "authentication failed")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "req-123" {
		t.Fatalf("X-Request-Id = %q, want req-123", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var detail ErrorDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Error.Code != CodeUnauthenticated {
		t.Fatalf("code = %q, want %q", detail.Error.Code, CodeUnauthenticated)
	}
	if detail.Error.Retryable {
		t.Fatalf("retryable = true, want false")
	}
	if detail.Error.Metadata == nil {
		t.Fatalf("metadata = nil, want empty object")
	}
	if detail.Error.RequestID != "req-123" {
		t.Fatalf("request_id = %q, want req-123", detail.Error.RequestID)
	}
}

func TestWriteAuthErrorTraceparent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ontology", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	writeAuthError(rec, req, http.StatusForbidden, CodePermissionDenied, "permission denied")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	want := "4bf92f3577b34da6a3ce929d0e0e4736"
	if got := rec.Header().Get("X-Request-Id"); got != want {
		t.Fatalf("X-Request-Id = %q, want %q", got, want)
	}
	var detail ErrorDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if detail.Error.Code != CodePermissionDenied || detail.Error.RequestID != want {
		t.Fatalf("detail = %+v", detail.Error)
	}
}
