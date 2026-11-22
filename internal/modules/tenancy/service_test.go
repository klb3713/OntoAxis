package tenancy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

var (
	tenantA = uuid.MustParse("11111111-1111-7111-8111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-7222-8222-222222222222")
)

func newTestService() (Service, *fakeStore) {
	store := newFakeStore()
	return NewService(store), store
}

func mustCreateOrganization(t *testing.T, svc Service, tenantID uuid.UUID, name string) *Organization {
	t.Helper()
	org, err := svc.CreateOrganization(context.Background(), tenantID, CreateOrganizationInput{Name: name})
	if err != nil {
		t.Fatalf("CreateOrganization(%q): %v", name, err)
	}
	return org
}

func mustCreateWorkspace(t *testing.T, svc Service, tenantID, organizationID uuid.UUID, name string) *Workspace {
	t.Helper()
	workspace, err := svc.CreateWorkspace(context.Background(), tenantID, CreateWorkspaceInput{
		OrganizationID: organizationID,
		Name:           name,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(%q): %v", name, err)
	}
	return workspace
}

func TestServiceOrganizationLifecycle(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	org, err := svc.CreateOrganization(ctx, tenantA, CreateOrganizationInput{
		Name:        "Acme",
		Description: "primary tenant organization",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if org.ID.Version() != 7 {
		t.Fatalf("organization id version = %d, want 7", org.ID.Version())
	}
	if org.DisplayName != "Acme" {
		t.Fatalf("display_name = %q, want Acme", org.DisplayName)
	}

	got, err := svc.GetOrganization(ctx, tenantA, org.ID)
	if err != nil || got.ID != org.ID {
		t.Fatalf("GetOrganization = %v, %v", got, err)
	}

	page, err := svc.ListOrganizations(ctx, tenantB, PageRequest{})
	if err != nil {
		t.Fatalf("ListOrganizations tenant B: %v", err)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("tenant B sees %d organizations, want 0", page.Total)
	}

	newName := "Acme Updated"
	updated, err := svc.UpdateOrganization(ctx, tenantA, org.ID, UpdateOrganizationInput{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateOrganization: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("updated name = %q, want %q", updated.Name, newName)
	}

	if err := svc.DeleteOrganization(ctx, tenantA, org.ID); err != nil {
		t.Fatalf("DeleteOrganization: %v", err)
	}
	if _, err := svc.GetOrganization(ctx, tenantA, org.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOrganization after delete error = %v, want ErrNotFound", err)
	}
}

func TestServiceWorkspaceProjectAndResourceLifecycle(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	workspace, err := svc.CreateWorkspace(ctx, tenantA, CreateWorkspaceInput{
		OrganizationID: org.ID,
		Name:           "Analytics",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if workspace.OrganizationID != org.ID {
		t.Fatalf("workspace organization = %s, want %s", workspace.OrganizationID, org.ID)
	}

	project, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
		OrganizationID: org.ID,
		WorkspaceID:    workspace.ID,
		Name:           "Risk",
		Description:    "risk project",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if project.WorkspaceID != workspace.ID {
		t.Fatalf("project workspace = %s, want %s", project.WorkspaceID, workspace.ID)
	}

	if _, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
		OrganizationID: org.ID,
		WorkspaceID:    workspace.ID,
		Name:           "Risk",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate project error = %v, want ErrConflict", err)
	}

	resource, err := svc.AddProjectResource(ctx, tenantA, project.ID, AddProjectResourceInput{
		ResourceType: "dataset",
		ResourceID:   "ds-001",
	})
	if err != nil {
		t.Fatalf("AddProjectResource: %v", err)
	}
	if resource.ProjectID != project.ID {
		t.Fatalf("resource project = %s, want %s", resource.ProjectID, project.ID)
	}
	if _, err := svc.AddProjectResource(ctx, tenantA, project.ID, AddProjectResourceInput{
		ResourceType: "dataset",
		ResourceID:   "ds-001",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate resource error = %v, want ErrConflict", err)
	}

	page, err := svc.ListProjectResources(ctx, tenantA, project.ID, PageRequest{})
	if err != nil {
		t.Fatalf("ListProjectResources: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("resource page total = %d len = %d, want 1/1", page.Total, len(page.Items))
	}

	if err := svc.RemoveProjectResource(ctx, tenantA, project.ID, resource.ID); err != nil {
		t.Fatalf("RemoveProjectResource: %v", err)
	}
	page, err = svc.ListProjectResources(ctx, tenantA, project.ID, PageRequest{})
	if err != nil {
		t.Fatalf("ListProjectResources after remove: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("resource page total after remove = %d, want 0", page.Total)
	}
}

func TestServiceMembership(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	membership, err := svc.AddMembership(ctx, tenantA, AddMembershipInput{
		UserID:         "user-1",
		OrganizationID: &org.ID,
	})
	if err != nil {
		t.Fatalf("AddMembership: %v", err)
	}
	if membership.PrincipalType != "user" || membership.PrincipalID != "user-1" || membership.UserID != "user-1" {
		t.Fatalf("membership = %+v, want user principal", membership)
	}
	if membership.Status != "active" || membership.Role != "member" {
		t.Fatalf("membership defaults = %q/%q, want active/member", membership.Status, membership.Role)
	}

	page, err := svc.ListMemberships(ctx, tenantA, &org.ID, nil, PageRequest{})
	if err != nil {
		t.Fatalf("ListMemberships: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("membership total = %d, want 1", page.Total)
	}

	if err := svc.RemoveMembership(ctx, tenantA, membership.ID); err != nil {
		t.Fatalf("RemoveMembership: %v", err)
	}
	if err := svc.RemoveMembership(ctx, tenantA, membership.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second RemoveMembership error = %v, want ErrNotFound", err)
	}
}

func TestServiceValidation(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	workspace := mustCreateWorkspace(t, svc, tenantA, org.ID, "Analytics")
	otherOrg := mustCreateOrganization(t, svc, tenantA, "Other")

	tests := []struct {
		name string
		want error
		call func() error
	}{
		{
			name: "empty organization name",
			call: func() error {
				_, err := svc.CreateOrganization(ctx, tenantA, CreateOrganizationInput{Name: "  "})
				return err
			},
		},
		{
			name: "missing tenant",
			call: func() error {
				_, err := svc.CreateOrganization(ctx, uuid.Nil, CreateOrganizationInput{Name: "Acme"})
				return err
			},
		},
		{
			name: "workspace with unknown organization",
			want: ErrNotFound,
			call: func() error {
				_, err := svc.CreateWorkspace(ctx, tenantA, CreateWorkspaceInput{OrganizationID: uuid.New(), Name: "W"})
				return err
			},
		},
		{
			name: "workspace from other tenant",
			want: ErrNotFound,
			call: func() error {
				_, err := svc.CreateWorkspace(ctx, tenantB, CreateWorkspaceInput{OrganizationID: org.ID, Name: "W"})
				return err
			},
		},
		{
			name: "invalid principal type",
			call: func() error {
				_, err := svc.AddMembership(ctx, tenantA, AddMembershipInput{
					PrincipalType: "robot", PrincipalID: "r-1", OrganizationID: &org.ID,
				})
				return err
			},
		},
		{
			name: "membership without scope",
			call: func() error {
				_, err := svc.AddMembership(ctx, tenantA, AddMembershipInput{
					PrincipalType: "user", PrincipalID: "u-1",
				})
				return err
			},
		},
		{
			name: "invalid membership status",
			call: func() error {
				_, err := svc.AddMembership(ctx, tenantA, AddMembershipInput{
					PrincipalType: "user", PrincipalID: "u-1", OrganizationID: &org.ID, Status: "deleted",
				})
				return err
			},
		},
		{
			name: "project workspace organization mismatch",
			call: func() error {
				_, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
					OrganizationID: otherOrg.ID, WorkspaceID: workspace.ID, Name: "P",
				})
				return err
			},
		},
		{
			name: "invalid resource type",
			call: func() error {
				project, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
					OrganizationID: org.ID, WorkspaceID: workspace.ID, Name: "P",
				})
				if err != nil {
					return err
				}
				_, err = svc.AddProjectResource(ctx, tenantA, project.ID, AddProjectResourceInput{
					ResourceType: "table", ResourceID: "x",
				})
				return err
			},
		},
		{
			name: "page size above maximum",
			call: func() error {
				_, err := svc.ListOrganizations(ctx, tenantA, PageRequest{Page: 1, PerPage: MaxPageSize + 1})
				return err
			},
		},
		{
			name: "empty name on update",
			call: func() error {
				empty := " "
				_, err := svc.UpdateOrganization(ctx, tenantA, org.ID, UpdateOrganizationInput{Name: &empty})
				return err
			},
		},
		{
			name: "invalid uuid parameter",
			call: func() error {
				_, err := svc.GetProject(ctx, tenantA, uuid.Nil)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			want := tt.want
			if want == nil {
				want = ErrInvalidArgument
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestServiceTenantIsolation(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	org := mustCreateOrganization(t, svc, tenantA, "Acme")
	workspace := mustCreateWorkspace(t, svc, tenantA, org.ID, "Analytics")
	project, err := svc.CreateProject(ctx, tenantA, CreateProjectInput{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, Name: "Risk",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if _, err := svc.GetOrganization(ctx, tenantB, org.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B GetOrganization error = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetWorkspace(ctx, tenantB, workspace.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B GetWorkspace error = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetProject(ctx, tenantB, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B GetProject error = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteProject(ctx, tenantB, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B DeleteProject error = %v, want ErrNotFound", err)
	}
	if _, err := svc.ListProjects(ctx, tenantB, nil, nil, PageRequest{}); err != nil {
		t.Fatalf("tenant B ListProjects: %v", err)
	}
	page, err := svc.ListProjects(ctx, tenantB, nil, nil, PageRequest{})
	if err != nil {
		t.Fatalf("tenant B ListProjects: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("tenant B project total = %d, want 0", page.Total)
	}
}
