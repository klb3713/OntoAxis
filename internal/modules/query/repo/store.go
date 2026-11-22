package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/query/domain"
)

// Store persists saved queries in query.saved_queries.
type Store interface {
	CreateSavedQuery(ctx context.Context, tenantID uuid.UUID, name, sql string, ownerID uuid.UUID) (domain.SavedQuery, error)
	GetSavedQuery(ctx context.Context, tenantID, id uuid.UUID) (domain.SavedQuery, error)
	ListSavedQueries(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.SavedQuery, int64, error)
	DeleteSavedQuery(ctx context.Context, tenantID, id uuid.UUID) error
}

type postgresStore struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) Store { return &postgresStore{pool: pool} }

const selectColumns = `id, tenant_id, name, sql, owner_id, created_at, updated_at`

func (s *postgresStore) CreateSavedQuery(ctx context.Context, tenantID uuid.UUID, name, sql string, ownerID uuid.UUID) (domain.SavedQuery, error) {
	if s.pool == nil {
		return domain.SavedQuery{}, fmt.Errorf("query: nil pool")
	}
	id := uuid.Must(uuid.NewV7())
	var out domain.SavedQuery
	err := s.pool.QueryRow(ctx, `INSERT INTO query.saved_queries (id, tenant_id, name, sql, owner_id) VALUES ($1,$2,$3,$4,$5) RETURNING `+selectColumns, id, tenantID, name, sql, ownerID).Scan(&out.ID, &out.TenantID, &out.Name, &out.SQL, &out.OwnerID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.SavedQuery{}, domain.ErrConflict
		}
		return domain.SavedQuery{}, fmt.Errorf("create saved query: %w", err)
	}
	return out, nil
}

func (s *postgresStore) GetSavedQuery(ctx context.Context, tenantID, id uuid.UUID) (domain.SavedQuery, error) {
	if s.pool == nil {
		return domain.SavedQuery{}, fmt.Errorf("query: nil pool")
	}
	var out domain.SavedQuery
	err := s.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM query.saved_queries WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.Name, &out.SQL, &out.OwnerID, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SavedQuery{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.SavedQuery{}, fmt.Errorf("get saved query: %w", err)
	}
	return out, nil
}

func (s *postgresStore) ListSavedQueries(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]domain.SavedQuery, int64, error) {
	if s.pool == nil {
		return nil, 0, fmt.Errorf("query: nil pool")
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM query.saved_queries WHERE tenant_id=$1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count saved queries: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+selectColumns+` FROM query.saved_queries WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list saved queries: %w", err)
	}
	defer rows.Close()
	out := make([]domain.SavedQuery, 0)
	for rows.Next() {
		var item domain.SavedQuery
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.SQL, &item.OwnerID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan saved query: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate saved queries: %w", err)
	}
	return out, total, nil
}

func (s *postgresStore) DeleteSavedQuery(ctx context.Context, tenantID, id uuid.UUID) error {
	if s.pool == nil {
		return fmt.Errorf("query: nil pool")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE query.saved_queries SET deleted_at = now(), updated_at = now() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete saved query: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
