package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/authn"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

const maxRequestBody = 1 << 20

type tenantContextKey struct{}

// WithTenantID stores a tenant identifier for tests and pre-authn wiring. The
// production request path prefers the platform authn TenantContext.
func WithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantIDFromContext returns the tenant identifier stored by WithTenantID.
func TenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(tenantContextKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

type api struct {
	service Service
	guard   authz.DecisionGuard
}

// Option configures optional api dependencies. Options are additive so existing
// construction sites keep compiling unchanged.
type Option func(*api)

// WithDecisionGuard injects the authorization decision guard used at every
// tenancy seam. A nil guard preserves the legacy allow-all behavior from before
// the authorization boundary was wired.
func WithDecisionGuard(guard authz.DecisionGuard) Option {
	return func(a *api) { a.guard = guard }
}

func newAPI(svc Service, opts ...Option) *api {
	a := &api{service: svc}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// authorize runs the single authorization decision point for a route. A
// configured guard fails closed; a nil guard keeps legacy behavior.
func (a *api) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, a.guard, action, resource)
}

func tenantFact(tenantID uuid.UUID) authz.ResourceFact {
	return authz.ResourceFact{Type: "tenant", ID: tenantID.String(), TenantID: tenantID.String()}
}

func organizationFact(org *Organization) authz.ResourceFact {
	return authz.ResourceFact{Type: "organization", ID: org.ID.String(), TenantID: org.TenantID.String()}
}

func workspaceFact(workspace *Workspace) authz.ResourceFact {
	return authz.ResourceFact{Type: "workspace", ID: workspace.ID.String(), TenantID: workspace.TenantID.String()}
}

func projectFact(project *Project) authz.ResourceFact {
	return authz.ResourceFact{Type: "project", ID: project.ID.String(), TenantID: project.TenantID.String()}
}

// organizationIDForMembership resolves the organization that owns a membership
// row. Membership rows bind an organization and/or a workspace; the workspace
// case still authorizes against the owning organization because membership
// actions are organization-scoped in the platform catalog.
func (a *api) organizationIDForMembership(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID) (*uuid.UUID, error) {
	if organizationID != nil {
		return organizationID, nil
	}
	if workspaceID == nil {
		return nil, nil
	}
	workspace, err := a.service.GetWorkspace(ctx, tenantID, *workspaceID)
	if err != nil {
		return nil, err
	}
	orgID := workspace.OrganizationID
	return &orgID, nil
}

func (a *api) register(r chi.Router) {
	r.Post("/organizations", a.withTenant(a.createOrganization))
	r.Get("/organizations", a.withTenant(a.listOrganizations))
	r.Get("/organizations/{id}", a.withTenant(a.getOrganization))
	r.Patch("/organizations/{id}", a.withTenant(a.updateOrganization))
	r.Delete("/organizations/{id}", a.withTenant(a.deleteOrganization))

	r.Post("/workspaces", a.withTenant(a.createWorkspace))
	r.Get("/workspaces", a.withTenant(a.listWorkspaces))
	r.Get("/workspaces/{id}", a.withTenant(a.getWorkspace))
	r.Patch("/workspaces/{id}", a.withTenant(a.updateWorkspace))
	r.Delete("/workspaces/{id}", a.withTenant(a.deleteWorkspace))

	r.Post("/memberships", a.withTenant(a.createMembership))
	r.Get("/memberships", a.withTenant(a.listMemberships))
	r.Delete("/memberships/{id}", a.withTenant(a.deleteMembership))

	r.Post("/projects", a.withTenant(a.createProject))
	r.Get("/projects", a.withTenant(a.listProjects))
	r.Get("/projects/{id}", a.withTenant(a.getProject))
	r.Patch("/projects/{id}", a.withTenant(a.updateProject))
	r.Delete("/projects/{id}", a.withTenant(a.deleteProject))

	r.Get("/projects/{projectID}/resources", a.withTenant(a.listProjectResources))
	r.Post("/projects/{projectID}/resources", a.withTenant(a.addProjectResource))
	r.Delete("/projects/{projectID}/resources/{resourceID}", a.withTenant(a.deleteProjectResource))
}

type pageMeta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type listPage[T any] struct {
	Items []T      `json:"items"`
	Page  pageMeta `json:"page"`
}

func newListPage[T any](p Page[T]) listPage[T] {
	items := p.Items
	if items == nil {
		items = []T{}
	}
	return listPage[T]{
		Items: items,
		Page: pageMeta{
			Page:       p.Page,
			PerPage:    p.PerPage,
			Total:      p.Total,
			TotalPages: p.TotalPages,
		},
	}
}

func (a *api) withTenant(fn func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, apiErr := resolveTenantID(r)
		if apiErr != nil {
			httperr.Write(w, r, *apiErr)
			return
		}
		fn(w, r, tenantID)
	}
}

func tenantFromAuthnContext(ctx context.Context) (uuid.UUID, bool) {
	tc, ok := authn.FromContext(ctx)
	if !ok || tc.TenantID == uuid.Nil {
		return uuid.Nil, false
	}
	return tc.TenantID, true
}

func resolveTenantID(r *http.Request) (uuid.UUID, *httperr.Error) {
	ctxTenant, hasContext := tenantFromAuthnContext(r.Context())
	if !hasContext {
		ctxTenant, hasContext = TenantIDFromContext(r.Context())
	}
	headerValue := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	var headerTenant uuid.UUID
	hasHeader := headerValue != ""
	if hasHeader {
		id, err := uuid.Parse(headerValue)
		if err != nil || id == uuid.Nil {
			apiErr := httperr.BadRequest("X-Tenant-Id must be a valid UUID")
			return uuid.Nil, &apiErr
		}
		headerTenant = id
	}
	if hasContext {
		if hasHeader && headerTenant != ctxTenant {
			apiErr := httperr.Forbidden("X-Tenant-Id does not match the authenticated tenant")
			return uuid.Nil, &apiErr
		}
		return ctxTenant, nil
	}
	if hasHeader {
		return headerTenant, nil
	}
	apiErr := httperr.Unauthorized("tenant context is required")
	return uuid.Nil, &apiErr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return &ValidationError{Message: "request body is required"}
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return &ValidationError{Message: "invalid or oversized JSON request body"}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &ValidationError{Message: "request body must contain a single JSON object"}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httperr.Write(w, r, httperr.BadRequest(validation.Message))
	case errors.Is(err, ErrInvalidArgument):
		httperr.Write(w, r, httperr.BadRequest("invalid request"))
	case errors.Is(err, ErrNotFound):
		httperr.Write(w, r, httperr.NotFound("resource not found"))
	case errors.Is(err, ErrConflict):
		httperr.Write(w, r, httperr.Conflict("resource already exists or conflicts with existing data"))
	default:
		httperr.Write(w, r, httperr.Internal())
	}
}

func parsePage(r *http.Request) (PageRequest, error) {
	var req PageRequest
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || page < 1 {
			return PageRequest{}, invalidf("page must be a positive integer")
		}
		req.Page = page
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("per_page")); raw != "" {
		perPage, err := strconv.Atoi(raw)
		if err != nil || perPage < 1 {
			return PageRequest{}, invalidf("per_page must be a positive integer")
		}
		req.PerPage = perPage
	}
	return req, nil
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalidf("%s must be a valid UUID", name)
	}
	return id, nil
}

func parseOptionalUUIDQuery(r *http.Request, name string) (*uuid.UUID, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil, invalidf("%s must be a valid UUID", name)
	}
	return &id, nil
}

func (a *api) createOrganization(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	var in CreateOrganizationInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:organization:create", tenantFact(tenantID)) {
		return
	}
	org, err := a.service.CreateOrganization(r.Context(), tenantID, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (a *api) listOrganizations(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	page, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:organization:read", tenantFact(tenantID)) {
		return
	}
	result, err := a.service.ListOrganizations(r.Context(), tenantID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newListPage(result))
}

func (a *api) getOrganization(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	org, err := a.service.GetOrganization(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:organization:read", organizationFact(org)) {
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (a *api) updateOrganization(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	var in UpdateOrganizationInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetOrganization(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:organization:update", organizationFact(current)) {
		return
	}
	org, err := a.service.UpdateOrganization(r.Context(), tenantID, id, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (a *api) deleteOrganization(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetOrganization(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:organization:delete", organizationFact(current)) {
		return
	}
	if err := a.service.DeleteOrganization(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createWorkspace(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	var in CreateWorkspaceInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	// A workspace is an organization child; authorize against the loaded parent
	// organization. A missing parent identifier falls back to the tenant fact,
	// which fails closed because workspace creation is organization-scoped.
	resource := tenantFact(tenantID)
	if in.OrganizationID != uuid.Nil {
		organization, err := a.service.GetOrganization(r.Context(), tenantID, in.OrganizationID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = organizationFact(organization)
	}
	if !a.authorize(w, r, "tenancy:workspace:create", resource) {
		return
	}
	workspace, err := a.service.CreateWorkspace(r.Context(), tenantID, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (a *api) listWorkspaces(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	organizationID, err := parseOptionalUUIDQuery(r, "organization_id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	// Organization-filtered lists authorize against the loaded organization;
	// unfiltered tenant-wide lists authorize against the tenant itself.
	resource := tenantFact(tenantID)
	if organizationID != nil {
		organization, err := a.service.GetOrganization(r.Context(), tenantID, *organizationID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = organizationFact(organization)
	}
	if !a.authorize(w, r, "tenancy:workspace:read", resource) {
		return
	}
	result, err := a.service.ListWorkspaces(r.Context(), tenantID, organizationID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newListPage(result))
}

func (a *api) getWorkspace(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	workspace, err := a.service.GetWorkspace(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:workspace:read", workspaceFact(workspace)) {
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *api) updateWorkspace(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	var in UpdateWorkspaceInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetWorkspace(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:workspace:update", workspaceFact(current)) {
		return
	}
	workspace, err := a.service.UpdateWorkspace(r.Context(), tenantID, id, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *api) deleteWorkspace(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetWorkspace(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:workspace:delete", workspaceFact(current)) {
		return
	}
	if err := a.service.DeleteWorkspace(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createMembership(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	var in AddMembershipInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	// Membership actions are organization-scoped; a workspace-scoped membership
	// still authorizes against the owning organization. Rows without either
	// parent fall back to the tenant fact, which fails closed.
	resource := tenantFact(tenantID)
	organizationID, err := a.organizationIDForMembership(r.Context(), tenantID, in.OrganizationID, in.WorkspaceID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if organizationID != nil {
		organization, err := a.service.GetOrganization(r.Context(), tenantID, *organizationID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = organizationFact(organization)
	}
	if !a.authorize(w, r, "tenancy:membership:add", resource) {
		return
	}
	membership, err := a.service.AddMembership(r.Context(), tenantID, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, membership)
}

func (a *api) listMemberships(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	organizationID, err := parseOptionalUUIDQuery(r, "organization_id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	workspaceID, err := parseOptionalUUIDQuery(r, "workspace_id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	// Parent-filtered lists authorize against the loaded organization (resolved
	// from the workspace filter when needed); unfiltered tenant-wide lists
	// authorize against the tenant itself.
	resource := tenantFact(tenantID)
	ownerID, err := a.organizationIDForMembership(r.Context(), tenantID, organizationID, workspaceID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if ownerID != nil {
		organization, err := a.service.GetOrganization(r.Context(), tenantID, *ownerID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = organizationFact(organization)
	}
	if !a.authorize(w, r, "tenancy:membership:read", resource) {
		return
	}
	result, err := a.service.ListMemberships(r.Context(), tenantID, organizationID, workspaceID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newListPage(result))
}

// findMembership loads a tenant-scoped membership row before authorization. The
// tenancy service exposes no single-row membership getter, so the row is located
// through bounded, tenant-scoped pages of ListMemberships.
func (a *api) findMembership(ctx context.Context, tenantID, id uuid.UUID) (*Membership, error) {
	const (
		maxPages    = 10
		perPageSize = 100
	)
	for page := 1; page <= maxPages; page++ {
		result, err := a.service.ListMemberships(ctx, tenantID, nil, nil, PageRequest{Page: page, PerPage: perPageSize})
		if err != nil {
			return nil, err
		}
		for i := range result.Items {
			if result.Items[i].ID == id {
				return &result.Items[i], nil
			}
		}
		if result.TotalPages <= page {
			break
		}
	}
	return nil, nil
}

func (a *api) deleteMembership(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	membership, err := a.findMembership(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if membership == nil {
		// Missing or cross-tenant rows must look like 404 before authorization.
		httperr.Write(w, r, httperr.NotFound("resource not found"))
		return
	}
	resource := tenantFact(tenantID)
	switch {
	case membership.OrganizationID != nil:
		organization, err := a.service.GetOrganization(r.Context(), tenantID, *membership.OrganizationID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = organizationFact(organization)
	case membership.WorkspaceID != nil:
		workspace, err := a.service.GetWorkspace(r.Context(), tenantID, *membership.WorkspaceID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = workspaceFact(workspace)
	}
	// Tenant-level membership rows (no organization or workspace parent) keep
	// the tenant fact as the audited, fail-closed fallback.
	if !a.authorize(w, r, "tenancy:membership:remove", resource) {
		return
	}
	if err := a.service.RemoveMembership(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createProject(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	var in CreateProjectInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	// A project is a workspace child; authorize against the loaded parent
	// workspace. A missing parent identifier falls back to the tenant fact,
	// which fails closed because project creation is workspace-scoped.
	resource := tenantFact(tenantID)
	if in.WorkspaceID != uuid.Nil {
		workspace, err := a.service.GetWorkspace(r.Context(), tenantID, in.WorkspaceID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = workspaceFact(workspace)
	}
	if !a.authorize(w, r, "tenancy:project:create", resource) {
		return
	}
	project, err := a.service.CreateProject(r.Context(), tenantID, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (a *api) listProjects(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	organizationID, err := parseOptionalUUIDQuery(r, "organization_id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	workspaceID, err := parseOptionalUUIDQuery(r, "workspace_id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	// Workspace-filtered lists authorize against the loaded workspace;
	// unfiltered tenant-wide lists authorize against the tenant itself.
	resource := tenantFact(tenantID)
	if workspaceID != nil {
		workspace, err := a.service.GetWorkspace(r.Context(), tenantID, *workspaceID)
		if err != nil {
			writeServiceError(w, r, err)
			return
		}
		resource = workspaceFact(workspace)
	}
	if !a.authorize(w, r, "tenancy:project:read", resource) {
		return
	}
	result, err := a.service.ListProjects(r.Context(), tenantID, organizationID, workspaceID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newListPage(result))
}

func (a *api) getProject(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	project, err := a.service.GetProject(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project:read", projectFact(project)) {
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (a *api) updateProject(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	var in UpdateProjectInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetProject(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project:update", projectFact(current)) {
		return
	}
	project, err := a.service.UpdateProject(r.Context(), tenantID, id, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (a *api) deleteProject(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	current, err := a.service.GetProject(r.Context(), tenantID, id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project:delete", projectFact(current)) {
		return
	}
	if err := a.service.DeleteProject(r.Context(), tenantID, id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listProjectResources(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	projectID, err := parseUUIDParam(r, "projectID")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	project, err := a.service.GetProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project_resource:read", projectFact(project)) {
		return
	}
	result, err := a.service.ListProjectResources(r.Context(), tenantID, projectID, page)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newListPage(result))
}

func (a *api) addProjectResource(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	projectID, err := parseUUIDParam(r, "projectID")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	var in AddProjectResourceInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeServiceError(w, r, err)
		return
	}
	project, err := a.service.GetProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project_resource:add", projectFact(project)) {
		return
	}
	resource, err := a.service.AddProjectResource(r.Context(), tenantID, projectID, in)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, resource)
}

func (a *api) deleteProjectResource(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	projectID, err := parseUUIDParam(r, "projectID")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	resourceID, err := parseUUIDParam(r, "resourceID")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	project, err := a.service.GetProject(r.Context(), tenantID, projectID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	if !a.authorize(w, r, "tenancy:project_resource:remove", projectFact(project)) {
		return
	}
	if err := a.service.RemoveProjectResource(r.Context(), tenantID, projectID, resourceID); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
