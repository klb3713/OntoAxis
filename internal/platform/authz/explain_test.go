package authz

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExplainHandlerAdminAllowed(t *testing.T) {
	ctx, tenantID, userID := testContext(t, "users", "tenant-admin")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	body, _ := json.Marshal(ExplainRequest{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/authorization/explain", bytes.NewReader(body)).WithContext(ctx)
	_ = userID
	rec := httptest.NewRecorder()
	ExplainHandler(a).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response ExplainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Allowed || response.DecisionID == "" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if rec.Header().Get(DecisionHeader) != response.DecisionID {
		t.Fatalf("decision header %q != body %q", rec.Header().Get(DecisionHeader), response.DecisionID)
	}
}

func TestExplainHandlerRejectsNonAdmin(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "users", "viewer")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	body, _ := json.Marshal(ExplainRequest{
		Action:   "dataset:read",
		Resource: ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/authorization/explain", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	ExplainHandler(a).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
