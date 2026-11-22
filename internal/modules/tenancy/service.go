// Package tenancy owns organizations, workspaces, memberships, projects and
// project resources. It is the root business module: every other module may
// depend on tenancy, but tenancy depends on no business module.
package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors returned by the service and store layers. The HTTP adapter
// maps them to stable httperr codes without exposing database details.
var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrInvalidArgument = errors.New("invalid argument")
)

// ValidationError carries a client-safe validation message. It matches
// ErrInvalidArgument through errors.Is.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) Is(target error) bool { return target == ErrInvalidArgument }

func invalidf(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Organization is a tenant-owned organization.
type Organization struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name,omitempty"`
	Description string     `json:"description,omitempty"`
	Slug        string     `json:"slug"`
	OwnerID     string     `json:"owner_id,omitempty"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"-"`
}

// Workspace is an organization-scoped workspace.
type Workspace struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	DisplayName    string     `json:"display_name,omitempty"`
	Description    string     `json:"description,omitempty"`
	Slug           string     `json:"slug"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"-"`
}

// Membership binds a principal to an organization and/or workspace.
type Membership struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	PrincipalType  string     `json:"principal_type"`
	PrincipalID    string     `json:"principal_id"`
	UserID         string     `json:"user_id,omitempty"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	WorkspaceID    *uuid.UUID `json:"workspace_id,omitempty"`
	Role           string     `json:"role"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Project is a tenant-owned project inside an organization and workspace.
type Project struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	WorkspaceID    uuid.UUID  `json:"workspace_id"`
	Name           string     `json:"name"`
	DisplayName    string     `json:"display_name,omitempty"`
	Description    string     `json:"description,omitempty"`
	CreatedBy      string     `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"-"`
}

// ProjectResource binds one platform resource to a project.
type ProjectResource struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	ProjectID    uuid.UUID `json:"project_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// PageRequest is the normalized pagination input shared by all list methods.
type PageRequest struct {
	Page    int
	PerPage int
}

// Page is the list envelope returned by the service and serialized by the API.
type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

const (
	// DefaultPageSize is used when per_page is omitted.
	DefaultPageSize = 20
	// MaxPageSize is the upper bound accepted for per_page.
	MaxPageSize = 100
)

// Normalize validates pagination. The zero value means "not supplied".
func (p PageRequest) Normalize() (PageRequest, error) {
	if p.Page < 0 {
		return PageRequest{}, invalidf("page must be greater than or equal to 1")
	}
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PerPage < 0 {
		return PageRequest{}, invalidf("per_page must be greater than or equal to 1")
	}
	if p.PerPage == 0 {
		p.PerPage = DefaultPageSize
	}
	if p.PerPage > MaxPageSize {
		return PageRequest{}, invalidf("per_page must be less than or equal to %d", MaxPageSize)
	}
	return p, nil
}

func (p PageRequest) offset() int { return (p.Page - 1) * p.PerPage }

func newPage[T any](items []T, req PageRequest, total int64) Page[T] {
	if items == nil {
		items = []T{}
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(req.PerPage) - 1) / int64(req.PerPage))
	}
	return Page[T]{Items: items, Page: req.Page, PerPage: req.PerPage, Total: total, TotalPages: pages}
}

// CreateOrganizationInput is the API input for creating an organization.
type CreateOrganizationInput struct {
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Description string     `json:"description"`
	Slug        string     `json:"slug"`
	OwnerID     string     `json:"owner_id"`
	ParentID    *uuid.UUID `json:"parent_id"`
}

// UpdateOrganizationInput uses pointers for PATCH semantics.
type UpdateOrganizationInput struct {
	Name        *string    `json:"name"`
	DisplayName *string    `json:"display_name"`
	Description *string    `json:"description"`
	Slug        *string    `json:"slug"`
	OwnerID     *string    `json:"owner_id"`
	ParentID    *uuid.UUID `json:"parent_id"`
}

// CreateWorkspaceInput is the API input for creating a workspace.
type CreateWorkspaceInput struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	DisplayName    string    `json:"display_name"`
	Description    string    `json:"description"`
	Slug           string    `json:"slug"`
}

// UpdateWorkspaceInput uses pointers for PATCH semantics.
type UpdateWorkspaceInput struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
	Slug        *string `json:"slug"`
}

// AddMembershipInput is the API input for adding a membership.
type AddMembershipInput struct {
	PrincipalType  string     `json:"principal_type"`
	PrincipalID    string     `json:"principal_id"`
	UserID         string     `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id"`
	WorkspaceID    *uuid.UUID `json:"workspace_id"`
	Role           string     `json:"role"`
	Status         string     `json:"status"`
}

// CreateProjectInput is the API input for creating a project.
type CreateProjectInput struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Name           string    `json:"name"`
	DisplayName    string    `json:"display_name"`
	Description    string    `json:"description"`
	CreatedBy      string    `json:"created_by"`
}

// UpdateProjectInput uses pointers for PATCH semantics.
type UpdateProjectInput struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
}

// AddProjectResourceInput is the API input for binding a resource to a project.
type AddProjectResourceInput struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

// Store is the persistence contract used by the service. Implementations must
// scope every operation to tenantID and must not leak storage errors upward.
type Store interface {
	CreateOrganization(ctx context.Context, o *Organization) error
	GetOrganization(ctx context.Context, tenantID, id uuid.UUID) (*Organization, error)
	ListOrganizations(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Organization, int64, error)
	UpdateOrganization(ctx context.Context, o *Organization) error
	DeleteOrganization(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error

	CreateWorkspace(ctx context.Context, w *Workspace) error
	GetWorkspace(ctx context.Context, tenantID, id uuid.UUID) (*Workspace, error)
	ListWorkspaces(ctx context.Context, tenantID uuid.UUID, organizationID *uuid.UUID, limit, offset int) ([]Workspace, int64, error)
	UpdateWorkspace(ctx context.Context, w *Workspace) error
	DeleteWorkspace(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error

	CreateMembership(ctx context.Context, m *Membership) error
	ListMemberships(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Membership, int64, error)
	DeleteMembership(ctx context.Context, tenantID, id uuid.UUID) error

	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, tenantID, id uuid.UUID) (*Project, error)
	ListProjects(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Project, int64, error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error

	CreateProjectResource(ctx context.Context, r *ProjectResource) error
	ListProjectResources(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]ProjectResource, int64, error)
	DeleteProjectResource(ctx context.Context, tenantID, projectID, id uuid.UUID) error
}

// Service is the domain API consumed by HTTP handlers and other modules.
type Service interface {
	CreateOrganization(ctx context.Context, tenantID uuid.UUID, in CreateOrganizationInput) (*Organization, error)
	GetOrganization(ctx context.Context, tenantID, id uuid.UUID) (*Organization, error)
	ListOrganizations(ctx context.Context, tenantID uuid.UUID, page PageRequest) (Page[Organization], error)
	UpdateOrganization(ctx context.Context, tenantID, id uuid.UUID, in UpdateOrganizationInput) (*Organization, error)
	DeleteOrganization(ctx context.Context, tenantID, id uuid.UUID) error

	CreateWorkspace(ctx context.Context, tenantID uuid.UUID, in CreateWorkspaceInput) (*Workspace, error)
	GetWorkspace(ctx context.Context, tenantID, id uuid.UUID) (*Workspace, error)
	ListWorkspaces(ctx context.Context, tenantID uuid.UUID, organizationID *uuid.UUID, page PageRequest) (Page[Workspace], error)
	UpdateWorkspace(ctx context.Context, tenantID, id uuid.UUID, in UpdateWorkspaceInput) (*Workspace, error)
	DeleteWorkspace(ctx context.Context, tenantID, id uuid.UUID) error

	AddMembership(ctx context.Context, tenantID uuid.UUID, in AddMembershipInput) (*Membership, error)
	ListMemberships(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, page PageRequest) (Page[Membership], error)
	RemoveMembership(ctx context.Context, tenantID, id uuid.UUID) error

	CreateProject(ctx context.Context, tenantID uuid.UUID, in CreateProjectInput) (*Project, error)
	GetProject(ctx context.Context, tenantID, id uuid.UUID) (*Project, error)
	ListProjects(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, page PageRequest) (Page[Project], error)
	UpdateProject(ctx context.Context, tenantID, id uuid.UUID, in UpdateProjectInput) (*Project, error)
	DeleteProject(ctx context.Context, tenantID, id uuid.UUID) error

	AddProjectResource(ctx context.Context, tenantID, projectID uuid.UUID, in AddProjectResourceInput) (*ProjectResource, error)
	ListProjectResources(ctx context.Context, tenantID, projectID uuid.UUID, page PageRequest) (Page[ProjectResource], error)
	RemoveProjectResource(ctx context.Context, tenantID, projectID, id uuid.UUID) error
}

type service struct {
	store Store
	now   func() time.Time
	newID func() (uuid.UUID, error)
}

// NewService constructs the tenancy service on top of a Store.
func NewService(store Store) Service {
	return &service{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		newID: uuid.NewV7,
	}
}

func (s *service) nextID() (uuid.UUID, error) {
	id, err := s.newID()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate uuidv7: %w", err)
	}
	return id, nil
}

func requireTenant(tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return invalidf("tenant id is required")
	}
	return nil
}

func requireID(name string, id uuid.UUID) error {
	if id == uuid.Nil {
		return invalidf("%s is required", name)
	}
	return nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidf("name is required")
	}
	return name, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func makeSlug(value, fallback string) string {
	slug := strings.ToLower(strings.TrimSpace(value))
	slug = nonSlug.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug != "" {
		return slug
	}
	slug = strings.ToLower(strings.TrimSpace(fallback))
	slug = nonSlug.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug != "" {
		return slug
	}
	return "item"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func validPrincipalType(value string) bool {
	switch value {
	case "user", "group", "service":
		return true
	default:
		return false
	}
}

func validMembershipStatus(value string) bool {
	switch value {
	case "active", "invited", "suspended":
		return true
	default:
		return false
	}
}

func validResourceType(value string) bool {
	switch value {
	case "object_type", "dataset", "pipeline", "dashboard":
		return true
	default:
		return false
	}
}

func (s *service) CreateOrganization(ctx context.Context, tenantID uuid.UUID, in CreateOrganizationInput) (*Organization, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	org := &Organization{
		ID:          id,
		TenantID:    tenantID,
		Name:        name,
		DisplayName: defaultString(in.DisplayName, name),
		Description: strings.TrimSpace(in.Description),
		Slug:        makeSlug(in.Slug, name+"-"+id.String()[:8]),
		OwnerID:     strings.TrimSpace(in.OwnerID),
		ParentID:    in.ParentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateOrganization(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func (s *service) GetOrganization(ctx context.Context, tenantID, id uuid.UUID) (*Organization, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if err := requireID("organization id", id); err != nil {
		return nil, err
	}
	return s.store.GetOrganization(ctx, tenantID, id)
}

func (s *service) ListOrganizations(ctx context.Context, tenantID uuid.UUID, page PageRequest) (Page[Organization], error) {
	if err := requireTenant(tenantID); err != nil {
		return Page[Organization]{}, err
	}
	req, err := page.Normalize()
	if err != nil {
		return Page[Organization]{}, err
	}
	items, total, err := s.store.ListOrganizations(ctx, tenantID, req.PerPage, req.offset())
	if err != nil {
		return Page[Organization]{}, err
	}
	return newPage(items, req, total), nil
}

func (s *service) UpdateOrganization(ctx context.Context, tenantID, id uuid.UUID, in UpdateOrganizationInput) (*Organization, error) {
	org, err := s.GetOrganization(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name, err := cleanName(*in.Name)
		if err != nil {
			return nil, err
		}
		org.Name = name
		if org.DisplayName == "" {
			org.DisplayName = name
		}
	}
	if in.DisplayName != nil {
		org.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Description != nil {
		org.Description = strings.TrimSpace(*in.Description)
	}
	if in.Slug != nil {
		org.Slug = makeSlug(*in.Slug, org.Name+"-"+org.ID.String()[:8])
	}
	if in.OwnerID != nil {
		org.OwnerID = strings.TrimSpace(*in.OwnerID)
	}
	if in.ParentID != nil {
		org.ParentID = in.ParentID
	}
	org.UpdatedAt = s.now()
	if err := s.store.UpdateOrganization(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func (s *service) DeleteOrganization(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := requireID("organization id", id); err != nil {
		return err
	}
	return s.store.DeleteOrganization(ctx, tenantID, id, s.now())
}

func (s *service) CreateWorkspace(ctx context.Context, tenantID uuid.UUID, in CreateWorkspaceInput) (*Workspace, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if err := requireID("organization id", in.OrganizationID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetOrganization(ctx, tenantID, in.OrganizationID); err != nil {
		return nil, err
	}
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	workspace := &Workspace{
		ID:             id,
		TenantID:       tenantID,
		OrganizationID: in.OrganizationID,
		Name:           name,
		DisplayName:    defaultString(in.DisplayName, name),
		Description:    strings.TrimSpace(in.Description),
		Slug:           makeSlug(in.Slug, name+"-"+id.String()[:8]),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateWorkspace(ctx, workspace); err != nil {
		return nil, err
	}
	return workspace, nil
}

func (s *service) GetWorkspace(ctx context.Context, tenantID, id uuid.UUID) (*Workspace, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if err := requireID("workspace id", id); err != nil {
		return nil, err
	}
	return s.store.GetWorkspace(ctx, tenantID, id)
}

func (s *service) ListWorkspaces(ctx context.Context, tenantID uuid.UUID, organizationID *uuid.UUID, page PageRequest) (Page[Workspace], error) {
	if err := requireTenant(tenantID); err != nil {
		return Page[Workspace]{}, err
	}
	req, err := page.Normalize()
	if err != nil {
		return Page[Workspace]{}, err
	}
	items, total, err := s.store.ListWorkspaces(ctx, tenantID, organizationID, req.PerPage, req.offset())
	if err != nil {
		return Page[Workspace]{}, err
	}
	return newPage(items, req, total), nil
}

func (s *service) UpdateWorkspace(ctx context.Context, tenantID, id uuid.UUID, in UpdateWorkspaceInput) (*Workspace, error) {
	workspace, err := s.GetWorkspace(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name, err := cleanName(*in.Name)
		if err != nil {
			return nil, err
		}
		workspace.Name = name
		if workspace.DisplayName == "" {
			workspace.DisplayName = name
		}
	}
	if in.DisplayName != nil {
		workspace.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Description != nil {
		workspace.Description = strings.TrimSpace(*in.Description)
	}
	if in.Slug != nil {
		workspace.Slug = makeSlug(*in.Slug, workspace.Name+"-"+workspace.ID.String()[:8])
	}
	workspace.UpdatedAt = s.now()
	if err := s.store.UpdateWorkspace(ctx, workspace); err != nil {
		return nil, err
	}
	return workspace, nil
}

func (s *service) DeleteWorkspace(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := requireID("workspace id", id); err != nil {
		return err
	}
	return s.store.DeleteWorkspace(ctx, tenantID, id, s.now())
}

func (s *service) AddMembership(ctx context.Context, tenantID uuid.UUID, in AddMembershipInput) (*Membership, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	principalType := strings.TrimSpace(in.PrincipalType)
	principalID := strings.TrimSpace(in.PrincipalID)
	userID := strings.TrimSpace(in.UserID)
	if principalType == "" && userID != "" {
		principalType = "user"
	}
	if principalID == "" && userID != "" {
		principalID = userID
	}
	if !validPrincipalType(principalType) {
		return nil, invalidf("principal_type must be one of user, group, service")
	}
	if principalID == "" {
		return nil, invalidf("principal_id is required")
	}
	if in.OrganizationID == nil && in.WorkspaceID == nil {
		return nil, invalidf("organization_id or workspace_id is required")
	}
	if in.OrganizationID != nil {
		if err := requireID("organization id", *in.OrganizationID); err != nil {
			return nil, err
		}
		if _, err := s.store.GetOrganization(ctx, tenantID, *in.OrganizationID); err != nil {
			return nil, err
		}
	}
	if in.WorkspaceID != nil {
		if err := requireID("workspace id", *in.WorkspaceID); err != nil {
			return nil, err
		}
		workspace, err := s.store.GetWorkspace(ctx, tenantID, *in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if in.OrganizationID != nil && workspace.OrganizationID != *in.OrganizationID {
			return nil, invalidf("workspace does not belong to the organization")
		}
	}
	status := defaultString(in.Status, "active")
	if !validMembershipStatus(status) {
		return nil, invalidf("status must be one of active, invited, suspended")
	}
	role := defaultString(in.Role, "member")
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	membership := &Membership{
		ID:             id,
		TenantID:       tenantID,
		PrincipalType:  principalType,
		PrincipalID:    principalID,
		OrganizationID: in.OrganizationID,
		WorkspaceID:    in.WorkspaceID,
		Role:           role,
		Status:         status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if principalType == "user" {
		membership.UserID = principalID
	}
	if err := s.store.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}
	return membership, nil
}

func (s *service) ListMemberships(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, page PageRequest) (Page[Membership], error) {
	if err := requireTenant(tenantID); err != nil {
		return Page[Membership]{}, err
	}
	req, err := page.Normalize()
	if err != nil {
		return Page[Membership]{}, err
	}
	items, total, err := s.store.ListMemberships(ctx, tenantID, organizationID, workspaceID, req.PerPage, req.offset())
	if err != nil {
		return Page[Membership]{}, err
	}
	return newPage(items, req, total), nil
}

func (s *service) RemoveMembership(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := requireID("membership id", id); err != nil {
		return err
	}
	return s.store.DeleteMembership(ctx, tenantID, id)
}

// resolveProjectOrganization returns the requested organization, or ensures a
// default organization exists for the tenant. Stage 1 keeps project creation
// usable from the web console before organization/workspace management UI is
// implemented.
func (s *service) resolveProjectOrganization(ctx context.Context, tenantID, requested uuid.UUID) (*Organization, error) {
	if requested != uuid.Nil {
		if err := requireID("organization id", requested); err != nil {
			return nil, err
		}
		return s.store.GetOrganization(ctx, tenantID, requested)
	}
	page, err := s.ListOrganizations(ctx, tenantID, PageRequest{Page: 1, PerPage: 1})
	if err != nil {
		return nil, err
	}
	if len(page.Items) > 0 {
		return &page.Items[0], nil
	}
	return s.CreateOrganization(ctx, tenantID, CreateOrganizationInput{
		Name:        "default",
		DisplayName: "Default Organization",
		Description: "Created automatically for the first project.",
	})
}

func (s *service) resolveProjectWorkspace(ctx context.Context, tenantID, organizationID, requested uuid.UUID) (*Workspace, error) {
	if requested != uuid.Nil {
		if err := requireID("workspace id", requested); err != nil {
			return nil, err
		}
		workspace, err := s.store.GetWorkspace(ctx, tenantID, requested)
		if err != nil {
			return nil, err
		}
		if workspace.OrganizationID != organizationID {
			return nil, invalidf("workspace does not belong to the organization")
		}
		return workspace, nil
	}
	page, err := s.ListWorkspaces(ctx, tenantID, &organizationID, PageRequest{Page: 1, PerPage: 1})
	if err != nil {
		return nil, err
	}
	if len(page.Items) > 0 {
		return &page.Items[0], nil
	}
	return s.CreateWorkspace(ctx, tenantID, CreateWorkspaceInput{
		OrganizationID: organizationID,
		Name:           "default",
		DisplayName:    "Default Workspace",
		Description:    "Created automatically for the first project.",
	})
}

func (s *service) CreateProject(ctx context.Context, tenantID uuid.UUID, in CreateProjectInput) (*Project, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	org, err := s.resolveProjectOrganization(ctx, tenantID, in.OrganizationID)
	if err != nil {
		return nil, err
	}
	workspace, err := s.resolveProjectWorkspace(ctx, tenantID, org.ID, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	in.OrganizationID = org.ID
	in.WorkspaceID = workspace.ID
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	project := &Project{
		ID:             id,
		TenantID:       tenantID,
		OrganizationID: in.OrganizationID,
		WorkspaceID:    in.WorkspaceID,
		Name:           name,
		DisplayName:    defaultString(in.DisplayName, name),
		Description:    strings.TrimSpace(in.Description),
		CreatedBy:      strings.TrimSpace(in.CreatedBy),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateProject(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *service) GetProject(ctx context.Context, tenantID, id uuid.UUID) (*Project, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if err := requireID("project id", id); err != nil {
		return nil, err
	}
	return s.store.GetProject(ctx, tenantID, id)
}

func (s *service) ListProjects(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, page PageRequest) (Page[Project], error) {
	if err := requireTenant(tenantID); err != nil {
		return Page[Project]{}, err
	}
	req, err := page.Normalize()
	if err != nil {
		return Page[Project]{}, err
	}
	items, total, err := s.store.ListProjects(ctx, tenantID, organizationID, workspaceID, req.PerPage, req.offset())
	if err != nil {
		return Page[Project]{}, err
	}
	return newPage(items, req, total), nil
}

func (s *service) UpdateProject(ctx context.Context, tenantID, id uuid.UUID, in UpdateProjectInput) (*Project, error) {
	project, err := s.GetProject(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name, err := cleanName(*in.Name)
		if err != nil {
			return nil, err
		}
		project.Name = name
		if project.DisplayName == "" {
			project.DisplayName = name
		}
	}
	if in.DisplayName != nil {
		project.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Description != nil {
		project.Description = strings.TrimSpace(*in.Description)
	}
	project.UpdatedAt = s.now()
	if err := s.store.UpdateProject(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *service) DeleteProject(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := requireID("project id", id); err != nil {
		return err
	}
	return s.store.DeleteProject(ctx, tenantID, id, s.now())
}

func (s *service) AddProjectResource(ctx context.Context, tenantID, projectID uuid.UUID, in AddProjectResourceInput) (*ProjectResource, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	if err := requireID("project id", projectID); err != nil {
		return nil, err
	}
	if _, err := s.store.GetProject(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	resourceType := strings.TrimSpace(in.ResourceType)
	if !validResourceType(resourceType) {
		return nil, invalidf("resource_type must be one of object_type, dataset, pipeline, dashboard")
	}
	resourceID := strings.TrimSpace(in.ResourceID)
	if resourceID == "" {
		return nil, invalidf("resource_id is required")
	}
	id, err := s.nextID()
	if err != nil {
		return nil, err
	}
	resource := &ProjectResource{
		ID:           id,
		TenantID:     tenantID,
		ProjectID:    projectID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		CreatedAt:    s.now(),
	}
	if err := s.store.CreateProjectResource(ctx, resource); err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *service) ListProjectResources(ctx context.Context, tenantID, projectID uuid.UUID, page PageRequest) (Page[ProjectResource], error) {
	if err := requireTenant(tenantID); err != nil {
		return Page[ProjectResource]{}, err
	}
	if err := requireID("project id", projectID); err != nil {
		return Page[ProjectResource]{}, err
	}
	if _, err := s.store.GetProject(ctx, tenantID, projectID); err != nil {
		return Page[ProjectResource]{}, err
	}
	req, err := page.Normalize()
	if err != nil {
		return Page[ProjectResource]{}, err
	}
	items, total, err := s.store.ListProjectResources(ctx, tenantID, projectID, req.PerPage, req.offset())
	if err != nil {
		return Page[ProjectResource]{}, err
	}
	return newPage(items, req, total), nil
}

func (s *service) RemoveProjectResource(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := requireID("project id", projectID); err != nil {
		return err
	}
	if err := requireID("project resource id", id); err != nil {
		return err
	}
	return s.store.DeleteProjectResource(ctx, tenantID, projectID, id)
}
