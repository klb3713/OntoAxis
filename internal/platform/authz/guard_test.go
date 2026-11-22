package authz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

func TestGuardAllowSetsDecisionHeader(t *testing.T) {
	ctx, tenantID, _ := testContext(t, "viewer")
	a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
	guard := NewGuard(a)
	decision, err := guard.Require(ctx, "dataset:read", ResourceFact{Type: "dataset", ID: "ds-1", TenantID: tenantID.String()})
	if err != nil {
		t.Fatalf("require: %v", err)
	}
	rec := httptest.NewRecorder()
	guard.SetDecisionHeader(rec, decision)
	if got := rec.Header().Get(DecisionHeader); got != decision.DecisionID {
		t.Fatalf("header %q != decision id %q", got, decision.DecisionID)
	}
}

func TestGuardWriteHTTPMappings(t *testing.T) {
	guard := NewGuard(nil)
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "unauthenticated", err: authn.ErrUnauthenticated, status: 401, code: httperr.CodeUnauthorized},
		{name: "denied", err: &Error{Reason: ReasonRoleNotPermitted, Engine: "cedar"}, status: 403, code: httperr.CodeForbidden},
		{name: "unavailable", err: &Error{Reason: ReasonEngineTimeout, Engine: "openfga"}, status: 503, code: httperr.CodeUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/datasets/ds-1", nil)
			rec := httptest.NewRecorder()
			guard.WriteHTTP(rec, req, tt.err)
			if rec.Code != tt.status {
				t.Fatalf("status %d != %d", rec.Code, tt.status)
			}
			var body map[string]httperr.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			got := body["error"]
			if got.Code != tt.code {
				t.Fatalf("code %q != %q", got.Code, tt.code)
			}
			if tt.status >= 500 && len(got.Message) > 128 {
				t.Fatalf("5xx leaked long message: %q", got.Message)
			}
		})
	}
}

func TestGuardRequireWithoutContext(t *testing.T) {
	guard := NewGuard(nil)
	_, err := guard.Require(context.Background(), "dataset:read", ResourceFact{})
	if err == nil {
		t.Fatal("expected error without authorizer")
	}
}
