package tenancy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the pgx-backed Store implementation. Every statement takes
// an explicit tenant_id; the core tables currently rely on repository-level
// scoping because RLS is not enabled by the platform change yet.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore builds a store on top of a shared pgx pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	return uuid.UUID(id.Bytes)
}

func storeErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: duplicate record", ErrConflict)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: referenced record does not exist", ErrConflict)
		}
	}
	return err
}

const organizationColumns = `id, tenant_id, name, display_name, description, slug, owner_id, parent_id, created_at, updated_at`

func scanOrganization(row pgx.Row) (*Organization, error) {
	var o Organization
	var parent pgtype.UUID
	err := row.Scan(&o.ID, &o.TenantID, &o.Name, &o.DisplayName, &o.Description, &o.Slug, &o.OwnerID, &parent, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, storeErr(err)
	}
	if parent.Valid {
		id := fromPgUUID(parent)
		o.ParentID = &id
	}
	return &o, nil
}

func (s *PostgresStore) CreateOrganization(ctx context.Context, o *Organization) error {
	const query = `INSERT INTO core.organizations
		(id, tenant_id, name, display_name, description, slug, owner_id, parent_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	var parent pgtype.UUID
	if o.ParentID != nil {
		parent = pgUUID(*o.ParentID)
	}
	_, err := s.pool.Exec(ctx, query,
		pgUUID(o.ID), pgUUID(o.TenantID), o.Name, o.DisplayName, o.Description,
		o.Slug, o.OwnerID, parent, o.CreatedAt, o.UpdatedAt)
	return storeErr(err)
}

func (s *PostgresStore) GetOrganization(ctx context.Context, tenantID, id uuid.UUID) (*Organization, error) {
	const query = `SELECT ` + organizationColumns + `
		FROM core.organizations WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	return scanOrganization(s.pool.QueryRow(ctx, query, pgUUID(tenantID), pgUUID(id)))
}

func (s *PostgresStore) ListOrganizations(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Organization, int64, error) {
	const countQuery = `SELECT count(*) FROM core.organizations WHERE tenant_id = $1 AND deleted_at IS NULL`
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, pgUUID(tenantID)).Scan(&total); err != nil {
		return nil, 0, storeErr(err)
	}
	const query = `SELECT ` + organizationColumns + `
		FROM core.organizations WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, query, pgUUID(tenantID), limit, offset)
	if err != nil {
		return nil, 0, storeErr(err)
	}
	defer rows.Close()
	items := make([]Organization, 0)
	for rows.Next() {
		item, err := scanOrganization(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, storeErr(err)
	}
	return items, total, nil
}

func (s *PostgresStore) UpdateOrganization(ctx context.Context, o *Organization) error {
	const query = `UPDATE core.organizations SET
		name = $3, display_name = $4, description = $5, slug = $6, owner_id = $7, parent_id = $8, updated_at = $9
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	var parent pgtype.UUID
	if o.ParentID != nil {
		parent = pgUUID(*o.ParentID)
	}
	tag, err := s.pool.Exec(ctx, query, pgUUID(o.TenantID), pgUUID(o.ID), o.Name, o.DisplayName, o.Description, o.Slug, o.OwnerID, parent, o.UpdatedAt)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteOrganization(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	const query = `UPDATE core.organizations SET deleted_at = $3, updated_at = $3
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := s.pool.Exec(ctx, query, pgUUID(tenantID), pgUUID(id), at)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const workspaceColumns = `id, tenant_id, organization_id, name, display_name, description, slug, created_at, updated_at`

func scanWorkspace(row pgx.Row) (*Workspace, error) {
	var w Workspace
	err := row.Scan(&w.ID, &w.TenantID, &w.OrganizationID, &w.Name, &w.DisplayName, &w.Description, &w.Slug, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, storeErr(err)
	}
	return &w, nil
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, w *Workspace) error {
	const query = `INSERT INTO core.workspaces
		(id, tenant_id, organization_id, name, display_name, description, slug, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := s.pool.Exec(ctx, query,
		pgUUID(w.ID), pgUUID(w.TenantID), pgUUID(w.OrganizationID), w.Name, w.DisplayName,
		w.Description, w.Slug, w.CreatedAt, w.UpdatedAt)
	return storeErr(err)
}

func (s *PostgresStore) GetWorkspace(ctx context.Context, tenantID, id uuid.UUID) (*Workspace, error) {
	const query = `SELECT ` + workspaceColumns + `
		FROM core.workspaces WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	return scanWorkspace(s.pool.QueryRow(ctx, query, pgUUID(tenantID), pgUUID(id)))
}

func (s *PostgresStore) ListWorkspaces(ctx context.Context, tenantID uuid.UUID, organizationID *uuid.UUID, limit, offset int) ([]Workspace, int64, error) {
	var orgFilter pgtype.UUID
	if organizationID != nil {
		orgFilter = pgUUID(*organizationID)
	}
	const countQuery = `SELECT count(*) FROM core.workspaces
		WHERE tenant_id = $1 AND deleted_at IS NULL AND ($2::uuid IS NULL OR organization_id = $2)`
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, pgUUID(tenantID), orgFilter).Scan(&total); err != nil {
		return nil, 0, storeErr(err)
	}
	const query = `SELECT ` + workspaceColumns + `
		FROM core.workspaces
		WHERE tenant_id = $1 AND deleted_at IS NULL AND ($2::uuid IS NULL OR organization_id = $2)
		ORDER BY created_at DESC, id LIMIT $3 OFFSET $4`
	rows, err := s.pool.Query(ctx, query, pgUUID(tenantID), orgFilter, limit, offset)
	if err != nil {
		return nil, 0, storeErr(err)
	}
	defer rows.Close()
	items := make([]Workspace, 0)
	for rows.Next() {
		item, err := scanWorkspace(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, storeErr(err)
	}
	return items, total, nil
}

func (s *PostgresStore) UpdateWorkspace(ctx context.Context, w *Workspace) error {
	const query = `UPDATE core.workspaces SET
		name = $3, display_name = $4, description = $5, slug = $6, updated_at = $7
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := s.pool.Exec(ctx, query, pgUUID(w.TenantID), pgUUID(w.ID), w.Name, w.DisplayName, w.Description, w.Slug, w.UpdatedAt)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteWorkspace(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	const query = `UPDATE core.workspaces SET deleted_at = $3, updated_at = $3
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := s.pool.Exec(ctx, query, pgUUID(tenantID), pgUUID(id), at)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const membershipColumns = `id, tenant_id, principal_type, principal_id, organization_id, workspace_id, role, status, created_at, updated_at`

func scanMembership(row pgx.Row) (*Membership, error) {
	var m Membership
	var orgID, workspaceID pgtype.UUID
	err := row.Scan(&m.ID, &m.TenantID, &m.PrincipalType, &m.PrincipalID, &orgID, &workspaceID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, storeErr(err)
	}
	if orgID.Valid {
		id := fromPgUUID(orgID)
		m.OrganizationID = &id
	}
	if workspaceID.Valid {
		id := fromPgUUID(workspaceID)
		m.WorkspaceID = &id
	}
	if m.PrincipalType == "user" {
		m.UserID = m.PrincipalID
	}
	return &m, nil
}

func (s *PostgresStore) CreateMembership(ctx context.Context, m *Membership) error {
	const query = `INSERT INTO core.memberships
		(id, tenant_id, principal_type, principal_id, organization_id, workspace_id, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	var orgID, workspaceID pgtype.UUID
	if m.OrganizationID != nil {
		orgID = pgUUID(*m.OrganizationID)
	}
	if m.WorkspaceID != nil {
		workspaceID = pgUUID(*m.WorkspaceID)
	}
	_, err := s.pool.Exec(ctx, query,
		pgUUID(m.ID), pgUUID(m.TenantID), m.PrincipalType, m.PrincipalID,
		orgID, workspaceID, m.Role, m.Status, m.CreatedAt, m.UpdatedAt)
	return storeErr(err)
}

func (s *PostgresStore) ListMemberships(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Membership, int64, error) {
	var orgFilter, workspaceFilter pgtype.UUID
	if organizationID != nil {
		orgFilter = pgUUID(*organizationID)
	}
	if workspaceID != nil {
		workspaceFilter = pgUUID(*workspaceID)
	}
	const countQuery = `SELECT count(*) FROM core.memberships
		WHERE tenant_id = $1
		AND ($2::uuid IS NULL OR organization_id = $2)
		AND ($3::uuid IS NULL OR workspace_id = $3)`
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, pgUUID(tenantID), orgFilter, workspaceFilter).Scan(&total); err != nil {
		return nil, 0, storeErr(err)
	}
	const query = `SELECT ` + membershipColumns + `
		FROM core.memberships
		WHERE tenant_id = $1
		AND ($2::uuid IS NULL OR organization_id = $2)
		AND ($3::uuid IS NULL OR workspace_id = $3)
		ORDER BY created_at DESC, id LIMIT $4 OFFSET $5`
	rows, err := s.pool.Query(ctx, query, pgUUID(tenantID), orgFilter, workspaceFilter, limit, offset)
	if err != nil {
		return nil, 0, storeErr(err)
	}
	defer rows.Close()
	items := make([]Membership, 0)
	for rows.Next() {
		item, err := scanMembership(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, storeErr(err)
	}
	return items, total, nil
}

func (s *PostgresStore) DeleteMembership(ctx context.Context, tenantID, id uuid.UUID) error {
	const query = `DELETE FROM core.memberships WHERE tenant_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, query, pgUUID(tenantID), pgUUID(id))
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const projectColumns = `id, tenant_id, organization_id, workspace_id, name, display_name, description, created_by, created_at, updated_at`

func scanProject(row pgx.Row) (*Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.TenantID, &p.OrganizationID, &p.WorkspaceID, &p.Name, &p.DisplayName, &p.Description, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, storeErr(err)
	}
	return &p, nil
}

func (s *PostgresStore) CreateProject(ctx context.Context, p *Project) error {
	const query = `INSERT INTO core.projects
		(id, tenant_id, organization_id, workspace_id, name, display_name, description, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := s.pool.Exec(ctx, query,
		pgUUID(p.ID), pgUUID(p.TenantID), pgUUID(p.OrganizationID), pgUUID(p.WorkspaceID),
		p.Name, p.DisplayName, p.Description, p.CreatedBy, p.CreatedAt, p.UpdatedAt)
	return storeErr(err)
}

func (s *PostgresStore) GetProject(ctx context.Context, tenantID, id uuid.UUID) (*Project, error) {
	const query = `SELECT ` + projectColumns + `
		FROM core.projects WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	return scanProject(s.pool.QueryRow(ctx, query, pgUUID(tenantID), pgUUID(id)))
}

func (s *PostgresStore) ListProjects(ctx context.Context, tenantID uuid.UUID, organizationID, workspaceID *uuid.UUID, limit, offset int) ([]Project, int64, error) {
	var orgFilter, workspaceFilter pgtype.UUID
	if organizationID != nil {
		orgFilter = pgUUID(*organizationID)
	}
	if workspaceID != nil {
		workspaceFilter = pgUUID(*workspaceID)
	}
	const countQuery = `SELECT count(*) FROM core.projects
		WHERE tenant_id = $1 AND deleted_at IS NULL
		AND ($2::uuid IS NULL OR organization_id = $2)
		AND ($3::uuid IS NULL OR workspace_id = $3)`
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, pgUUID(tenantID), orgFilter, workspaceFilter).Scan(&total); err != nil {
		return nil, 0, storeErr(err)
	}
	const query = `SELECT ` + projectColumns + `
		FROM core.projects
		WHERE tenant_id = $1 AND deleted_at IS NULL
		AND ($2::uuid IS NULL OR organization_id = $2)
		AND ($3::uuid IS NULL OR workspace_id = $3)
		ORDER BY created_at DESC, id LIMIT $4 OFFSET $5`
	rows, err := s.pool.Query(ctx, query, pgUUID(tenantID), orgFilter, workspaceFilter, limit, offset)
	if err != nil {
		return nil, 0, storeErr(err)
	}
	defer rows.Close()
	items := make([]Project, 0)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, storeErr(err)
	}
	return items, total, nil
}

func (s *PostgresStore) UpdateProject(ctx context.Context, p *Project) error {
	const query = `UPDATE core.projects SET
		name = $3, display_name = $4, description = $5, updated_at = $6
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := s.pool.Exec(ctx, query, pgUUID(p.TenantID), pgUUID(p.ID), p.Name, p.DisplayName, p.Description, p.UpdatedAt)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteProject(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	const query = `UPDATE core.projects SET deleted_at = $3, updated_at = $3
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := s.pool.Exec(ctx, query, pgUUID(tenantID), pgUUID(id), at)
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const projectResourceColumns = `id, tenant_id, project_id, resource_type, resource_id, created_at`

func scanProjectResource(row pgx.Row) (*ProjectResource, error) {
	var r ProjectResource
	err := row.Scan(&r.ID, &r.TenantID, &r.ProjectID, &r.ResourceType, &r.ResourceID, &r.CreatedAt)
	if err != nil {
		return nil, storeErr(err)
	}
	return &r, nil
}

func (s *PostgresStore) CreateProjectResource(ctx context.Context, r *ProjectResource) error {
	const query = `INSERT INTO core.project_resources
		(id, tenant_id, project_id, resource_type, resource_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.pool.Exec(ctx, query,
		pgUUID(r.ID), pgUUID(r.TenantID), pgUUID(r.ProjectID), r.ResourceType, r.ResourceID, r.CreatedAt)
	return storeErr(err)
}

func (s *PostgresStore) ListProjectResources(ctx context.Context, tenantID, projectID uuid.UUID, limit, offset int) ([]ProjectResource, int64, error) {
	const countQuery = `SELECT count(*) FROM core.project_resources WHERE tenant_id = $1 AND project_id = $2`
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, pgUUID(tenantID), pgUUID(projectID)).Scan(&total); err != nil {
		return nil, 0, storeErr(err)
	}
	const query = `SELECT ` + projectResourceColumns + `
		FROM core.project_resources WHERE tenant_id = $1 AND project_id = $2
		ORDER BY created_at DESC, id LIMIT $3 OFFSET $4`
	rows, err := s.pool.Query(ctx, query, pgUUID(tenantID), pgUUID(projectID), limit, offset)
	if err != nil {
		return nil, 0, storeErr(err)
	}
	defer rows.Close()
	items := make([]ProjectResource, 0)
	for rows.Next() {
		item, err := scanProjectResource(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, storeErr(err)
	}
	return items, total, nil
}

func (s *PostgresStore) DeleteProjectResource(ctx context.Context, tenantID, projectID, id uuid.UUID) error {
	const query = `DELETE FROM core.project_resources WHERE tenant_id = $1 AND project_id = $2 AND id = $3`
	tag, err := s.pool.Exec(ctx, query, pgUUID(tenantID), pgUUID(projectID), pgUUID(id))
	if err != nil {
		return storeErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
