package tenancy

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/authz/authtest"
)

func newAuthorizedTestRouter(t *testing.T, svc Service, guard authz.DecisionGuard) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	newModuleWithService(nil, svc, WithGuard(guard)).RegisterRoutes(router)
	return router
}

func guardCalls(t *testing.T, guard *authtest.FakeGuard) ([]authz.Action, []authz.ResourceFact) {
	t.Helper()
	actions, facts := guard.Calls()
	if len(actions) != len(facts) {
		t.Fatalf("guard recorded %d actions and %d facts", len(actions), len(facts))
	}
	return actions, facts
}

func requireFact(t *testing.T, fact authz.ResourceFact, wantType, wantID, wantTenant string) {
	t.Helper()
	if fact.Type != wantType || fact.ID != wantID || fact.TenantID != wantTenant {
		t.Fatalf("resource fact = %#v, want type=%q id=%q tenant=%q", fact, wantType, wantID, wantTenant)
	}
}

func TestAPIAuthorizationSeamAllows(t *testing.T) {
	svc, _ := newTestService()
	guard := authtest.Guard()
	handler := newAuthorizedTestRouter(t, svc, guard)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", tenantA.String(), nil, map[string]any{
		"name": "Acme",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create organization status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(authz.DecisionHeader); got != "decision-test" {
		t.Fatalf("decision header = %q, want decision-test", got)
	}
	org := decodeResponse[Organization](t, rec)

	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/workspaces", tenantA.String(), nil, map[string]any{
		"organization_id": org.ID.String(),
		"name":            "Core",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d, body = %s", rec.Code, rec.Body.String())
	}
	workspace := decodeResponse[Workspace](t, rec)

	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/projects", tenantA.String(), nil, map[string]any{
		"organization_id": org.ID.String(),
		"workspace_id":    workspace.ID.String(),
		"name":            "Probe",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project status = %d, body = %s", rec.Code, rec.Body.String())
	}
	project := decodeResponse[Project](t, rec)

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/projects/"+project.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/workspaces", tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list workspaces status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/workspaces?organization_id="+org.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list workspaces status = %d, body = %s", rec.Code, rec.Body.String())
	}

	actions, facts := guardCalls(t, guard)
	want := []struct {
		action     authz.Action
		factType   string
		factID     string
		factTenant string
	}{
		{"tenancy:organization:create", "tenant", tenantA.String(), tenantA.String()},
		{"tenancy:workspace:create", "organization", org.ID.String(), tenantA.String()},
		{"tenancy:project:create", "workspace", workspace.ID.String(), tenantA.String()},
		{"tenancy:project:read", "project", project.ID.String(), tenantA.String()},
		{"tenancy:workspace:read", "tenant", tenantA.String(), tenantA.String()},
		{"tenancy:workspace:read", "organization", org.ID.String(), tenantA.String()},
	}
	if len(actions) != len(want) {
		t.Fatalf("guard calls = %v, want %d", actions, len(want))
	}
	for i := range want {
		if actions[i] != want[i].action {
			t.Fatalf("call %d action = %q, want %q", i, actions[i], want[i].action)
		}
		requireFact(t, facts[i], want[i].factType, want[i].factID, want[i].factTenant)
	}
}

func TestAPIAuthorizationDenyBlocksSideEffects(t *testing.T) {
	svc, _ := newTestService()
	guard := authtest.Deny(authz.ReasonRoleNotPermitted)
	handler := newAuthorizedTestRouter(t, svc, guard)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", tenantA.String(), nil, map[string]any{
		"name": "Acme",
	})
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	organizations, err := svc.ListOrganizations(context.Background(), tenantA, PageRequest{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if organizations.Total != 0 {
		t.Fatalf("denied create produced %d organizations", organizations.Total)
	}

	actions, facts := guardCalls(t, guard)
	if len(actions) != 1 || actions[0] != "tenancy:organization:create" {
		t.Fatalf("guard calls = %v, want one tenancy:organization:create", actions)
	}
	requireFact(t, facts[0], "tenant", tenantA.String(), tenantA.String())
}

func TestAPIAuthorizationSkipsGuardForMissingRows(t *testing.T) {
	svc, _ := newTestService()
	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	guard := authtest.Guard()
	handler := newAuthorizedTestRouter(t, svc, guard)

	// Cross-tenant read resolves no row, so it must 404 before authorization.
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantB.String(), nil, nil)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")

	// A membership row that does not exist in this tenant must also 404 first.
	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/memberships/"+uuid.NewString(), tenantA.String(), nil, nil)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")

	actions, _ := guardCalls(t, guard)
	if len(actions) != 0 {
		t.Fatalf("guard was called for missing rows: %v", actions)
	}
}

func TestAPIAuthorizationUsesParentScopeFacts(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	workspace := mustCreateWorkspace(t, svc, tenantA, org.ID, "Core")
	project, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
		OrganizationID: org.ID,
		WorkspaceID:    workspace.ID,
		Name:           "Probe",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	membership, err := svc.AddMembership(ctx, tenantA, AddMembershipInput{
		UserID:         "user-1",
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("AddMembership: %v", err)
	}
	otherOrg := mustCreateOrganization(t, svc, tenantB, "Beta")
	otherMembership, err := svc.AddMembership(ctx, tenantB, AddMembershipInput{
		UserID:         "user-2",
		OrganizationID: &otherOrg.ID,
	})
	if err != nil {
		t.Fatalf("AddMembership(other tenant): %v", err)
	}

	guard := authtest.Guard()
	handler := newAuthorizedTestRouter(t, svc, guard)

	// Membership removal authorizes against the membership's Organization.
	rec := doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/memberships/"+membership.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete membership status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// A membership owned by another tenant is invisible: 404 before authz.
	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/memberships/"+otherMembership.ID.String(), tenantA.String(), nil, nil)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")

	// Project-resource actions authorize against the loaded Project.
	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/projects/"+project.ID.String()+"/resources", tenantA.String(), nil, map[string]any{
		"resource_type": "dataset",
		"resource_id":   "ds-001",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add project resource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	actions, facts := guardCalls(t, guard)
	if len(actions) != 2 {
		t.Fatalf("guard calls = %v, want membership remove + project resource add", actions)
	}
	if actions[0] != "tenancy:membership:remove" {
		t.Fatalf("call 0 action = %q, want tenancy:membership:remove", actions[0])
	}
	requireFact(t, facts[0], "organization", org.ID.String(), tenantA.String())
	if actions[1] != "tenancy:project_resource:add" {
		t.Fatalf("call 1 action = %q, want tenancy:project_resource:add", actions[1])
	}
	requireFact(t, facts[1], "project", project.ID.String(), tenantA.String())
}
