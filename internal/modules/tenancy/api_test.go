package tenancy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/observability"
)

type apiErrorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), id)))
	})
}

func newTestRouter(svc Service) http.Handler {
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	mod := newModuleWithService(nil, svc)
	mod.RegisterRoutes(router)
	return router
}

func doJSON(t *testing.T, handler http.Handler, method, path, tenantHeader string, ctxTenant *uuid.UUID, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenantHeader != "" {
		req.Header.Set("X-Tenant-Id", tenantHeader)
	}
	if ctxTenant != nil {
		req = req.WithContext(WithTenantID(req.Context(), *ctxTenant))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return value
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) apiErrorEnvelope {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	envelope := decodeResponse[apiErrorEnvelope](t, rec)
	if envelope.Error.Code != code {
		t.Fatalf("error code = %q, want %q; body=%s", envelope.Error.Code, code, rec.Body.String())
	}
	if envelope.Error.RequestID == "" {
		t.Fatalf("error response is missing request_id: %s", rec.Body.String())
	}
	return envelope
}

func TestAPIOrganizationCRUDAndTenantErrors(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", tenantA.String(), nil, map[string]any{
		"name":        "Acme",
		"description": "primary",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create organization status = %d, body = %s", rec.Code, rec.Body.String())
	}
	org := decodeResponse[Organization](t, rec)
	if org.Name != "Acme" || org.TenantID != tenantA {
		t.Fatalf("organization = %+v, want tenant A Acme", org)
	}

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations", tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list organizations status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeResponse[listPage[Organization]](t, rec)
	if list.Page.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("organization list total/len = %d/%d, want 1/1", list.Page.Total, len(list.Items))
	}

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantB.String(), nil, nil)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations", "", nil, nil)
	assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations", "not-a-uuid", nil, nil)
	assertErrorCode(t, rec, http.StatusBadRequest, "bad_request")

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations", tenantB.String(), &tenantA, nil)
	assertErrorCode(t, rec, http.StatusForbidden, "forbidden")

	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", tenantA.String(), nil, map[string]any{"name": " "})
	assertErrorCode(t, rec, http.StatusBadRequest, "bad_request")

	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete organization status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantA.String(), nil, nil)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

func TestAPIWorkspaceProjectAndResourceFlow(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)
	ctx := context.Background()

	org := mustCreateOrganization(t, svc, tenantA, "Acme")

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/workspaces", tenantA.String(), nil, map[string]any{
		"organization_id": org.ID.String(),
		"name":            "Analytics",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d, body = %s", rec.Code, rec.Body.String())
	}
	workspace := decodeResponse[Workspace](t, rec)

	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/projects", tenantA.String(), nil, map[string]any{
		"organization_id": org.ID.String(),
		"workspace_id":    workspace.ID.String(),
		"name":            "Risk",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project status = %d, body = %s", rec.Code, rec.Body.String())
	}
	project := decodeResponse[Project](t, rec)

	resourceBody := map[string]any{"resource_type": "dataset", "resource_id": "ds-001"}
	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/projects/"+project.ID.String()+"/resources", tenantA.String(), nil, resourceBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add resource status = %d, body = %s", rec.Code, rec.Body.String())
	}
	resource := decodeResponse[ProjectResource](t, rec)

	rec = doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/projects/"+project.ID.String()+"/resources", tenantA.String(), nil, resourceBody)
	assertErrorCode(t, rec, http.StatusConflict, "conflict")

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/projects/"+project.ID.String()+"/resources", tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list resources status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeResponse[listPage[ProjectResource]](t, rec)
	if list.Page.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("resource list total/len = %d/%d, want 1/1", list.Page.Total, len(list.Items))
	}

	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/projects/"+project.ID.String()+"/resources/"+resource.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete resource status = %d, body = %s", rec.Code, rec.Body.String())
	}

	projects, err := svc.ListProjects(ctx, tenantA, nil, nil, PageRequest{})
	if err != nil || projects.Total != 1 {
		t.Fatalf("service ListProjects = %+v, %v", projects, err)
	}
}

func TestAPIMembershipFlow(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)
	org := mustCreateOrganization(t, svc, tenantA, "Acme")

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/memberships", tenantA.String(), nil, map[string]any{
		"user_id":         "user-1",
		"organization_id": org.ID.String(),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add membership status = %d, body = %s", rec.Code, rec.Body.String())
	}
	membership := decodeResponse[Membership](t, rec)
	if membership.PrincipalType != "user" || membership.UserID != "user-1" {
		t.Fatalf("membership = %+v, want user principal", membership)
	}

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/memberships?organization_id="+org.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list memberships status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeResponse[listPage[Membership]](t, rec)
	if list.Page.Total != 1 {
		t.Fatalf("membership total = %d, want 1", list.Page.Total)
	}

	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/memberships/"+membership.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete membership status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIPaginationValidation(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)

	for _, path := range []string{
		"/api/v1/tenancy/organizations?page=0",
		"/api/v1/tenancy/organizations?per_page=0",
		"/api/v1/tenancy/organizations?per_page=101",
		"/api/v1/tenancy/organizations?page=abc",
	} {
		rec := doJSON(t, handler, http.MethodGet, path, tenantA.String(), nil, nil)
		assertErrorCode(t, rec, http.StatusBadRequest, "bad_request")
	}
}

type failingOrganizationStore struct {
	*fakeStore
}

func (failingOrganizationStore) CreateOrganization(context.Context, *Organization) error {
	return errors.New("internal SQL detail: password=secret")
}

func TestAPIInternalErrorDoesNotLeakDetails(t *testing.T) {
	store := failingOrganizationStore{fakeStore: newFakeStore()}
	handler := newTestRouter(NewService(store))

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", tenantA.String(), nil, map[string]any{"name": "Acme"})
	envelope := assertErrorCode(t, rec, http.StatusInternalServerError, "internal")
	if strings.Contains(envelope.Error.Message, "SQL") || strings.Contains(envelope.Error.Message, "secret") {
		t.Fatalf("internal error leaked detail: %q", envelope.Error.Message)
	}
}

func TestAPIContextTenantIsUsed(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/tenancy/organizations", "", &tenantB, map[string]any{"name": "Context Org"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with context tenant status = %d, body = %s", rec.Code, rec.Body.String())
	}
	org := decodeResponse[Organization](t, rec)
	if org.TenantID != tenantB {
		t.Fatalf("tenant_id = %s, want context tenant %s", org.TenantID, tenantB)
	}
}

func TestAPIUpdateAndDeleteRoutes(t *testing.T) {
	svc, _ := newTestService()
	handler := newTestRouter(svc)

	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	workspace := mustCreateWorkspace(t, svc, tenantA, org.ID, "Analytics")
	project, err := svc.CreateProject(context.Background(), tenantA, CreateProjectInput{
		OrganizationID: org.ID,
		WorkspaceID:    workspace.ID,
		Name:           "Risk",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	rec := doJSON(t, handler, http.MethodPatch, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantA.String(), nil, map[string]any{"display_name": "Acme Updated"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch organization status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeResponse[Organization](t, rec); got.DisplayName != "Acme Updated" {
		t.Fatalf("patched organization display_name = %q", got.DisplayName)
	}

	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/workspaces?organization_id="+org.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list workspaces status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeResponse[listPage[Workspace]](t, rec); got.Page.Total != 1 {
		t.Fatalf("workspace list total = %d, want 1", got.Page.Total)
	}

	rec = doJSON(t, handler, http.MethodPatch, "/api/v1/tenancy/workspaces/"+workspace.ID.String(), tenantA.String(), nil, map[string]any{"name": "Analytics 2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch workspace status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/workspaces/"+workspace.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get workspace status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodPatch, "/api/v1/tenancy/projects/"+project.ID.String(), tenantA.String(), nil, map[string]any{"display_name": "Risk 2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch project status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodGet, "/api/v1/tenancy/projects/"+project.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/projects/"+project.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete project status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/workspaces/"+workspace.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete workspace status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, handler, http.MethodDelete, "/api/v1/tenancy/organizations/"+org.ID.String(), tenantA.String(), nil, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete organization status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPIAuthnTenantContextIsUsed(t *testing.T) {
	svc, _ := newTestService()
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	auth := authn.NewEdgeAuthenticator(authn.Config{TrustGateway: true}, nil)
	router.Use(auth.Middleware)
	newModuleWithService(nil, svc).RegisterRoutes(router)

	payload := bytes.NewBufferString(`{"name":"Gateway Org"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenancy/organizations", payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ontoaxis-tenant-id", tenantA.String())
	req.Header.Set("x-ontoaxis-user-id", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("gateway-context create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	org := decodeResponse[Organization](t, rec)
	if org.TenantID != tenantA {
		t.Fatalf("tenant_id = %s, want gateway tenant %s", org.TenantID, tenantA)
	}
}
