package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

// Store is the persistence boundary for objects and links.
type Store interface {
	CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error)
	GetObject(ctx context.Context, tenantID, id uuid.UUID) (domain.Object, error)
	ListObjects(ctx context.Context, tenantID uuid.UUID, objectTypeID uuid.UUID, limit, offset int) ([]domain.Object, int64, error)
	UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error)
	DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error
	CreateLink(ctx context.Context, tenantID, linkTypeID, sourceID, targetID uuid.UUID, properties map[string]any) (domain.Link, error)
	ListLinks(ctx context.Context, tenantID, objectID uuid.UUID, limit, offset int) ([]domain.Link, int64, error)
	DeleteLink(ctx context.Context, tenantID, id uuid.UUID) error
}

// PostgresStore implements Store on the object_data schema.
type PostgresStore struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Object{}, fmt.Errorf("generate object id: %w", err)
	}
	payload, err := json.Marshal(properties)
	if err != nil {
		return domain.Object{}, fmt.Errorf("encode properties: %w", err)
	}
	const q = `INSERT INTO object_data.objects (id, tenant_id, object_type_id, properties)
VALUES ($1, $2, $3, $4::jsonb)
RETURNING id, tenant_id, object_type_id, version, properties, created_at, updated_at`
	var o domain.Object
	var raw []byte
	err = s.pool.QueryRow(ctx, q, id, tenantID, objectTypeID, payload).Scan(
		&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return domain.Object{}, err
	}
	_ = json.Unmarshal(raw, &o.Properties)
	return o, nil
}

func (s *PostgresStore) GetObject(ctx context.Context, tenantID, id uuid.UUID) (domain.Object, error) {
	const q = `SELECT id, tenant_id, object_type_id, version, properties, created_at, updated_at
FROM object_data.objects WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	var o domain.Object
	var raw []byte
	err := s.pool.QueryRow(ctx, q, tenantID, id).Scan(
		&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Object{}, err
	}
	_ = json.Unmarshal(raw, &o.Properties)
	return o, nil
}

func (s *PostgresStore) ListObjects(ctx context.Context, tenantID uuid.UUID, objectTypeID uuid.UUID, limit, offset int) ([]domain.Object, int64, error) {
	query := `SELECT id, tenant_id, object_type_id, version, properties, created_at, updated_at
FROM object_data.objects
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	args := []any{tenantID, limit, offset}
	countQuery := `SELECT count(*) FROM object_data.objects WHERE tenant_id = $1 AND deleted_at IS NULL`
	countArgs := []any{tenantID}
	if objectTypeID != uuid.Nil {
		query = `SELECT id, tenant_id, object_type_id, version, properties, created_at, updated_at
FROM object_data.objects
WHERE tenant_id = $1 AND object_type_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC LIMIT $3 OFFSET $4`
		args = []any{tenantID, objectTypeID, limit, offset}
		countQuery = `SELECT count(*) FROM object_data.objects WHERE tenant_id = $1 AND object_type_id = $2 AND deleted_at IS NULL`
		countArgs = []any{tenantID, objectTypeID}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Object
	for rows.Next() {
		var o domain.Object
		var raw []byte
		if err := rows.Scan(&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &o.Properties)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *PostgresStore) UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error) {
	payload, err := json.Marshal(properties)
	if err != nil {
		return domain.Object{}, fmt.Errorf("encode properties: %w", err)
	}
	const q = `UPDATE object_data.objects
SET properties = CASE WHEN $3 THEN $4::jsonb ELSE properties || $4::jsonb END,
    version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING id, tenant_id, object_type_id, version, properties, created_at, updated_at`
	var o domain.Object
	var raw []byte
	err = s.pool.QueryRow(ctx, q, tenantID, id, replace, payload).Scan(
		&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Object{}, err
	}
	_ = json.Unmarshal(raw, &o.Properties)
	return o, nil
}

func (s *PostgresStore) DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE object_data.objects SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateLink(ctx context.Context, tenantID, linkTypeID, sourceID, targetID uuid.UUID, properties map[string]any) (domain.Link, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Link{}, fmt.Errorf("generate link id: %w", err)
	}
	payload, err := json.Marshal(properties)
	if err != nil {
		return domain.Link{}, fmt.Errorf("encode link properties: %w", err)
	}
	const q = `INSERT INTO object_data.object_links (id, tenant_id, link_type_id, source_id, target_id, properties)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)
RETURNING id, tenant_id, link_type_id, source_id, target_id, properties, created_at, updated_at`
	var l domain.Link
	var raw []byte
	err = s.pool.QueryRow(ctx, q, id, tenantID, linkTypeID, sourceID, targetID, payload).Scan(
		&l.ID, &l.TenantID, &l.LinkTypeID, &l.SourceID, &l.TargetID, &raw, &l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.Link{}, domain.ErrConflict
		}
		return domain.Link{}, err
	}
	_ = json.Unmarshal(raw, &l.Properties)
	return l, nil
}

func (s *PostgresStore) ListLinks(ctx context.Context, tenantID, objectID uuid.UUID, limit, offset int) ([]domain.Link, int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, link_type_id, source_id, target_id, properties, created_at, updated_at
FROM object_data.object_links
WHERE tenant_id = $1 AND (source_id = $2 OR target_id = $2) AND deleted_at IS NULL
ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenantID, objectID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Link
	for rows.Next() {
		var l domain.Link
		var raw []byte
		if err := rows.Scan(&l.ID, &l.TenantID, &l.LinkTypeID, &l.SourceID, &l.TargetID, &raw, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &l.Properties)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM object_data.object_links
WHERE tenant_id = $1 AND (source_id = $2 OR target_id = $2) AND deleted_at IS NULL`, tenantID, objectID).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *PostgresStore) DeleteLink(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE object_data.object_links SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
