package tenancy

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// fakeStore is an in-memory Store used by unit and HTTP tests. It enforces the
// same tenant scoping, soft-delete and uniqueness rules as the SQL schema.
type fakeStore struct {
	mu          sync.Mutex
	orgs        map[uuid.UUID]*Organization
	workspaces  map[uuid.UUID]*Workspace
	memberships map[uuid.UUID]*Membership
	projects    map[uuid.UUID]*Project
	resources   map[uuid.UUID]*ProjectResource
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		orgs:        make(map[uuid.UUID]*Organization),
		workspaces:  make(map[uuid.UUID]*Workspace),
		memberships: make(map[uuid.UUID]*Membership),
		projects:    make(map[uuid.UUID]*Project),
		resources:   make(map[uuid.UUID]*ProjectResource),
	}
}

func cloneOrganization(o *Organization) *Organization {
	c := *o
	if o.ParentID != nil {
		id := *o.ParentID
		c.ParentID = &id
	}
	return &c
}

func cloneWorkspace(w *Workspace) *Workspace {
	c := *w
	return &c
}

func cloneMembership(m *Membership) *Membership {
	c := *m
	if m.OrganizationID != nil {
		id := *m.OrganizationID
		c.OrganizationID = &id
	}
	if m.WorkspaceID != nil {
		id := *m.WorkspaceID
		c.WorkspaceID = &id
	}
	return &c
}

func cloneProject(p *Project) *Project {
	c := *p
	return &c
}

func cloneProjectResource(r *ProjectResource) *ProjectResource {
	c := *r
	return &c
}

func paginateSlice[T any](items []T, limit, offset int) ([]T, int64) {
	total := int64(len(items))
	if offset >= len(items) {
		return []T{}, total
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total
}

func (f *fakeStore) CreateOrganization(_ context.Context, o *Organization) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o.ParentID != nil {
		parent, ok := f.orgs[*o.ParentID]
		if !ok || parent.TenantID != o.TenantID || parent.DeletedAt != nil {
			return ErrConflict
		}
	}
	for _, other := range f.orgs {
		if other.TenantID == o.TenantID && other.DeletedAt == nil && other.Slug == o.Slug {
			return ErrConflict
		}
	}
	f.orgs[o.ID] = cloneOrganization(o)
	return nil
}

func (f *fakeStore) GetOrganization(_ context.Context, tenantID, id uuid.UUID) (*Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.orgs[id]
	if !ok || org.TenantID != tenantID || org.DeletedAt != nil {
		return nil, ErrNotFound
	}
	return cloneOrganization(org), nil
}

func (f *fakeStore) ListOrganizations(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]Organization, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]Organization, 0)
	for _, org := range f.orgs {
		if org.TenantID == tenantID && org.DeletedAt == nil {
			items = append(items, *cloneOrganization(org))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := paginateSlice(items, limit, offset)
	return page, total, nil
}

func (f *fakeStore) UpdateOrganization(_ context.Context, o *Organization) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.orgs[o.ID]
	if !ok || current.TenantID != o.TenantID || current.DeletedAt != nil {
		return ErrNotFound
	}
	for _, other := range f.orgs {
		if other.ID != o.ID && other.TenantID == o.TenantID && other.DeletedAt == nil && other.Slug == o.Slug {
			return ErrConflict
		}
	}
	f.orgs[o.ID] = cloneOrganization(o)
	return nil
}

func (f *fakeStore) DeleteOrganization(_ context.Context, tenantID, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.orgs[id]
	if !ok || org.TenantID != tenantID || org.DeletedAt != nil {
		return ErrNotFound
	}
	now := at
	org.DeletedAt = &now
	org.UpdatedAt = at
	return nil
}

func (f *fakeStore) CreateWorkspace(_ context.Context, w *Workspace) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.orgs[w.OrganizationID]
	if !ok || org.TenantID != w.TenantID || org.DeletedAt != nil {
		return ErrConflict
	}
	for _, other := range f.workspaces {
		if other.TenantID == w.TenantID && other.OrganizationID == w.OrganizationID && other.DeletedAt == nil && other.Slug == w.Slug {
			return ErrConflict
		}
	}
	f.workspaces[w.ID] = cloneWorkspace(w)
	return nil
}

func (f *fakeStore) GetWorkspace(_ context.Context, tenantID, id uuid.UUID) (*Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	workspace, ok := f.workspaces[id]
	if !ok || workspace.TenantID != tenantID || workspace.DeletedAt != nil {
		return nil, ErrNotFound
	}
	return cloneWorkspace(workspace), nil
}

func (f *fakeStore) ListWorkspaces(_ context.Context, tenantID uuid.UUID, organizationID *uuid.UUID, limit, offset int) ([]Workspace, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]Workspace, 0)
	for _, workspace := range f.workspaces {
		if workspace.TenantID != tenantID || workspace.DeletedAt != nil {
			continue
		}
		if organizationID != nil && workspace.OrganizationID != *organizationID {
			continue
		}
		items = append(items, *cloneWorkspace(workspace))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := paginateSlice(items, limit, offset)
	return page, total, nil
}

func (f *fakeStore) UpdateWorkspace(_ context.Context, w *Workspace) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.workspaces[w.ID]
	if !ok || current.TenantID != w.TenantID || current.DeletedAt != nil {
		return ErrNotFound
	}
	for _, other := range f.workspaces {
		if other.ID != w.ID && other.TenantID == w.TenantID && other.OrganizationID == w.OrganizationID && other.DeletedAt == nil && other.Slug == w.Slug {
			return ErrConflict
		}
	}
	f.workspaces[w.ID] = cloneWorkspace(w)
	return nil
}

func (f *fakeStore) DeleteWorkspace(_ context.Context, tenantID, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	workspace, ok := f.workspaces[id]
	if !ok || workspace.TenantID != tenantID || workspace.DeletedAt != nil {
		return ErrNotFound
	}
	now := at
	workspace.DeletedAt = &now
	workspace.UpdatedAt = at
	return nil
}

func (f *fakeStore) CreateMembership(_ context.Context, m *Membership) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memberships[m.ID] = cloneMembership(m)
	return nil
}

func (f *fakeStore) ListMemberships(_ context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Membership, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]Membership, 0)
	for _, membership := range f.memberships {
		if membership.TenantID != tenantID {
			continue
		}
		if organizationID != nil && (membership.OrganizationID == nil || *membership.OrganizationID != *organizationID) {
			continue
		}
		if workspaceID != nil && (membership.WorkspaceID == nil || *membership.WorkspaceID != *workspaceID) {
			continue
		}
		items = append(items, *cloneMembership(membership))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := paginateSlice(items, limit, offset)
	return page, total, nil
}

func (f *fakeStore) DeleteMembership(_ context.Context, tenantID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	membership, ok := f.memberships[id]
	if !ok || membership.TenantID != tenantID {
		return ErrNotFound
	}
	delete(f.memberships, id)
	return nil
}

func (f *fakeStore) CreateProject(_ context.Context, p *Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.orgs[p.OrganizationID]
	if !ok || org.TenantID != p.TenantID || org.DeletedAt != nil {
		return ErrConflict
	}
	workspace, ok := f.workspaces[p.WorkspaceID]
	if !ok || workspace.TenantID != p.TenantID || workspace.DeletedAt != nil || workspace.OrganizationID != p.OrganizationID {
		return ErrConflict
	}
	for _, other := range f.projects {
		if other.TenantID == p.TenantID && other.OrganizationID == p.OrganizationID && other.DeletedAt == nil && other.Name == p.Name {
			return ErrConflict
		}
	}
	f.projects[p.ID] = cloneProject(p)
	return nil
}

func (f *fakeStore) GetProject(_ context.Context, tenantID, id uuid.UUID) (*Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[id]
	if !ok || project.TenantID != tenantID || project.DeletedAt != nil {
		return nil, ErrNotFound
	}
	return cloneProject(project), nil
}

func (f *fakeStore) ListProjects(_ context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Project, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]Project, 0)
	for _, project := range f.projects {
		if project.TenantID != tenantID || project.DeletedAt != nil {
			continue
		}
		if organizationID != nil && project.OrganizationID != *organizationID {
			continue
		}
		if workspaceID != nil && project.WorkspaceID != *workspaceID {
			continue
		}
		items = append(items, *cloneProject(project))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := paginateSlice(items, limit, offset)
	return page, total, nil
}

func (f *fakeStore) UpdateProject(_ context.Context, p *Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.projects[p.ID]
	if !ok || current.TenantID != p.TenantID || current.DeletedAt != nil {
		return ErrNotFound
	}
	for _, other := range f.projects {
		if other.ID != p.ID && other.TenantID == p.TenantID && other.OrganizationID == p.OrganizationID && other.DeletedAt == nil && other.Name == p.Name {
			return ErrConflict
		}
	}
	f.projects[p.ID] = cloneProject(p)
	return nil
}

func (f *fakeStore) DeleteProject(_ context.Context, tenantID, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[id]
	if !ok || project.TenantID != tenantID || project.DeletedAt != nil {
		return ErrNotFound
	}
	now := at
	project.DeletedAt = &now
	project.UpdatedAt = at
	return nil
}

func (f *fakeStore) CreateProjectResource(_ context.Context, r *ProjectResource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	project, ok := f.projects[r.ProjectID]
	if !ok || project.TenantID != r.TenantID || project.DeletedAt != nil {
		return ErrConflict
	}
	for _, other := range f.resources {
		if other.TenantID == r.TenantID && other.ProjectID == r.ProjectID && other.ResourceType == r.ResourceType && other.ResourceID == r.ResourceID {
			return ErrConflict
		}
	}
	f.resources[r.ID] = cloneProjectResource(r)
	return nil
}

func (f *fakeStore) ListProjectResources(_ context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]ProjectResource, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]ProjectResource, 0)
	for _, resource := range f.resources {
		if resource.TenantID == tenantID && resource.ProjectID == projectID {
			items = append(items, *cloneProjectResource(resource))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID.String() < items[j].ID.String()
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, total := paginateSlice(items, limit, offset)
	return page, total, nil
}

func (f *fakeStore) DeleteProjectResource(_ context.Context, tenantID, projectID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	resource, ok := f.resources[id]
	if !ok || resource.TenantID != tenantID || resource.ProjectID != projectID {
		return ErrNotFound
	}
	delete(f.resources, id)
	return nil
}
