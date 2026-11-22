package authz

import (
	"errors"
	"testing"
)

func TestResourceStatePolicyMatrix(t *testing.T) {
	cases := []struct {
		name       string
		action     Action
		resource   ResourceFact
		wantDeny   bool
		wantReason Reason
	}{
		{
			name: "archived pipeline update", action: "pipeline:update",
			resource: ResourceFact{Type: "pipeline", ID: "p1", Status: "archived"},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
		{
			name: "published pipeline invoke", action: "pipeline:invoke",
			resource: ResourceFact{Type: "pipeline", ID: "p1", Status: "published"},
			wantDeny: false,
		},
		{
			name: "draft pipeline invoke", action: "pipeline:invoke",
			resource: ResourceFact{Type: "pipeline", ID: "p1", Status: "draft"},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
		{
			name: "paused connector sync", action: "connector:sync",
			resource: ResourceFact{Type: "connector", ID: "c1", Status: "paused"},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
		{
			name: "disabled action type run", action: "action_type:run",
			resource: ResourceFact{Type: "action_type", ID: "a1", Status: "disabled"},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
		{
			name: "succeeded ingestion cancel", action: "ingestion:cancel",
			resource: ResourceFact{Type: "ingest_job", ID: "j1", Status: "succeeded",
				Ancestors: []Ancestor{{Type: "connector", ID: "c1"}}},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
		{
			name: "running action execution cancel", action: "action_execution:cancel",
			resource: ResourceFact{Type: "action_execution", ID: "e1", Status: "running",
				Ancestors: []Ancestor{{Type: "action_type", ID: "a1"}}},
			wantDeny: false,
		},
		{
			name: "failed action execution retry", action: "action_execution:retry",
			resource: ResourceFact{Type: "action_execution", ID: "e1", Status: "failed",
				Ancestors: []Ancestor{{Type: "action_type", ID: "a1"}}},
			wantDeny: false,
		},
		{
			name: "queued action execution retry", action: "action_execution:retry",
			resource: ResourceFact{Type: "action_execution", ID: "e1", Status: "queued",
				Ancestors: []Ancestor{{Type: "action_type", ID: "a1"}}},
			wantDeny: true, wantReason: ReasonResourceStateDenied,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tenantID, _ := testContext(t, "tenant-admin")
			tc.resource.TenantID = tenantID.String()
			a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
			decision, err := a.Authorize(ctx, Request{Action: tc.action, Resource: tc.resource})
			if tc.wantDeny {
				if err == nil || decision.Allow {
					t.Fatalf("expected deny, got decision=%+v err=%v", decision, err)
				}
				var authzErr *Error
				if !errors.As(err, &authzErr) || authzErr.Reason != tc.wantReason {
					t.Fatalf("deny reason = %v, want %s", err, tc.wantReason)
				}
				return
			}
			if err != nil || !decision.Allow {
				t.Fatalf("expected allow, got decision=%+v err=%v", decision, err)
			}
		})
	}
}
