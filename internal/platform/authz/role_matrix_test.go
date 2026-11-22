package authz

import (
	"testing"
)

// TestRoleGroupMatrix covers the boundary §5.3 role×action-group matrix.
func TestRoleGroupMatrix(t *testing.T) {
	cases := []struct {
		name    string
		roles   []string
		action  Action
		typ     string
		status  string
		allowed bool
	}{
		{"viewer/read", []string{"viewer"}, "dataset:read", "dataset", "", true},
		{"viewer/write", []string{"viewer"}, "dataset:update", "dataset", "", false},
		{"viewer/delete", []string{"viewer"}, "dataset:delete", "dataset", "", false},
		{"viewer/invoke", []string{"viewer"}, "pipeline:invoke", "pipeline", "published", false},
		{"viewer/admin", []string{"viewer"}, "tenancy:workspace:create", "organization", "", false},
		{"viewer/audit", []string{"viewer"}, "audit:read", "tenant", "", false},
		{"member/read", []string{"member"}, "dataset:read", "dataset", "", true},
		{"member/write", []string{"member"}, "dataset:update", "dataset", "", true},
		{"member/delete", []string{"member"}, "dataset:delete", "dataset", "", true},
		{"member/invoke", []string{"member"}, "pipeline:invoke", "pipeline", "published", true},
		{"member/admin", []string{"member"}, "tenancy:workspace:create", "organization", "", false},
		{"member/audit", []string{"member"}, "audit:read", "tenant", "", false},
		{"workspace-admin/read", []string{"workspace-admin"}, "dataset:read", "dataset", "", true},
		{"workspace-admin/write", []string{"workspace-admin"}, "dataset:update", "dataset", "", true},
		{"workspace-admin/delete", []string{"workspace-admin"}, "dataset:delete", "dataset", "", true},
		{"workspace-admin/invoke", []string{"workspace-admin"}, "pipeline:invoke", "pipeline", "published", true},
		{"workspace-admin/admin", []string{"workspace-admin"}, "tenancy:workspace:create", "organization", "", true},
		{"workspace-admin/audit", []string{"workspace-admin"}, "audit:read", "tenant", "", false},
		{"tenant-admin/read", []string{"tenant-admin"}, "dataset:read", "dataset", "", true},
		{"tenant-admin/write", []string{"tenant-admin"}, "dataset:update", "dataset", "", true},
		{"tenant-admin/delete", []string{"tenant-admin"}, "dataset:delete", "dataset", "", true},
		{"tenant-admin/invoke", []string{"tenant-admin"}, "pipeline:invoke", "pipeline", "published", true},
		{"tenant-admin/admin", []string{"tenant-admin"}, "tenancy:workspace:create", "organization", "", true},
		{"tenant-admin/audit", []string{"tenant-admin"}, "audit:read", "tenant", "", true},
		{"platform-admin/read", []string{"platform-admin"}, "dataset:read", "dataset", "", true},
		{"platform-admin/write", []string{"platform-admin"}, "dataset:update", "dataset", "", true},
		{"platform-admin/delete", []string{"platform-admin"}, "dataset:delete", "dataset", "", true},
		{"platform-admin/invoke", []string{"platform-admin"}, "pipeline:invoke", "pipeline", "published", true},
		{"platform-admin/admin", []string{"platform-admin"}, "tenancy:workspace:create", "organization", "", true},
		{"platform-admin/audit", []string{"platform-admin"}, "audit:read", "tenant", "", true},
		{"no-role/read", nil, "dataset:read", "dataset", "", false},
		{"no-role/admin", nil, "tenancy:workspace:create", "organization", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, tenantID, _ := testContext(t, tc.roles...)
			a := newTestAuthorizer(t, &fakeFGA{allowed: true}, nil)
			decision, err := a.Authorize(ctx, Request{
				Action: tc.action,
				Resource: ResourceFact{
					Type: tc.typ, ID: "res-1", TenantID: tenantID.String(), Status: tc.status,
				},
			})
			if tc.allowed {
				if err != nil || !decision.Allow {
					t.Fatalf("expected allow, got decision=%+v err=%v", decision, err)
				}
				return
			}
			if err == nil || decision.Allow {
				t.Fatalf("expected deny, got decision=%+v err=%v", decision, err)
			}
		})
	}
}
