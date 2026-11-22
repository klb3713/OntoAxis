// Package repo implements the connectors persistence port with pgx. Every SQL
// statement is scoped to the tenant supplied by the service layer.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/modules/connectors/service"
)

var _ service.Store = (*PostgresStore)(nil)

// PostgresStore persists connectors in the connector schema.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// New builds a Postgres-backed connectors store.
func New(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const connectorColumns = `id, tenant_id, name, display_name, connector_type, config, tags, secret_ref, status, created_by, created_at, updated_at, deleted_at`

// CreateConnector inserts a connector and maps unique violations to ErrConflict.
func (s *PostgresStore) CreateConnector(ctx context.Context, connector *domain.Connector) error {
	configJSON, err := marshalJSON(connector.Config)
	if err != nil {
		return fmt.Errorf("marshal connector config: %w", err)
	}
	tagsJSON, err := marshalJSON(connector.Tags)
	if err != nil {
		return fmt.Errorf("marshal connector tags: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO connector.connectors
  (id, tenant_id, name, display_name, connector_type, config, tags, secret_ref, status, created_by, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11, $12)`,
		connector.ID, connector.TenantID, connector.Name, connector.DisplayName, string(connector.Type),
		configJSON, tagsJSON, connector.SecretRef, string(connector.Status),
		nullableUUID(connector.CreatedBy), connector.CreatedAt.UTC(), connector.UpdatedAt.UTC())
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

// GetConnector finds a non-deleted connector within the tenant.
func (s *PostgresStore) GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error) {
	row := s.pool.QueryRow(ctx, `
SELECT `+connectorColumns+`
FROM connector.connectors
WHERE id = $1::uuid AND tenant_id = $2::uuid AND deleted_at IS NULL`, id, tenantID)
	connector, err := scanConnector(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: connector %s", domain.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get connector: %w", err)
	}
	return connector, nil
}

// ListConnectors returns a page and total count, newest first and excluding
// soft-deleted rows.
func (s *PostgresStore) ListConnectors(ctx context.Context, tenantID string, limit, offset int32) ([]domain.Connector, int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `
SELECT count(*) FROM connector.connectors
WHERE tenant_id = $1::uuid AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count connectors: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+connectorColumns+`
FROM connector.connectors
WHERE tenant_id = $1::uuid AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list connectors: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Connector, 0, limit)
	for rows.Next() {
		connector, err := scanConnector(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan connector: %w", err)
		}
		items = append(items, *connector)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate connectors: %w", err)
	}
	return items, total, nil
}

// UpdateConnector replaces mutable columns after checking tenant ownership.
func (s *PostgresStore) UpdateConnector(ctx context.Context, connector *domain.Connector) error {
	configJSON, err := marshalJSON(connector.Config)
	if err != nil {
		return fmt.Errorf("marshal connector config: %w", err)
	}
	tagsJSON, err := marshalJSON(connector.Tags)
	if err != nil {
		return fmt.Errorf("marshal connector tags: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
UPDATE connector.connectors
SET name = $1, display_name = $2, connector_type = $3, config = $4::jsonb,
    tags = $5::jsonb, secret_ref = $6, status = $7, updated_at = $8
WHERE id = $9::uuid AND tenant_id = $10::uuid AND deleted_at IS NULL`,
		connector.Name, connector.DisplayName, string(connector.Type), configJSON, tagsJSON,
		connector.SecretRef, string(connector.Status), connector.UpdatedAt.UTC(),
		connector.ID, connector.TenantID)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: connector %s", domain.ErrNotFound, connector.ID)
	}
	return nil
}

// SoftDeleteConnector marks a connector deleted only within the tenant.
func (s *PostgresStore) SoftDeleteConnector(ctx context.Context, tenantID, id string, deletedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
UPDATE connector.connectors
SET deleted_at = $1, updated_at = $1
WHERE id = $2::uuid AND tenant_id = $3::uuid AND deleted_at IS NULL`, deletedAt.UTC(), id, tenantID)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: connector %s", domain.ErrNotFound, id)
	}
	return nil
}

func scanConnector(row interface {
	Scan(dest ...any) error
}) (*domain.Connector, error) {
	var (
		connector  domain.Connector
		connectorT string
		status     string
		configJSON []byte
		tagsJSON   []byte
		createdBy  *string
	)
	if err := row.Scan(
		&connector.ID, &connector.TenantID, &connector.Name, &connector.DisplayName,
		&connectorT, &configJSON, &tagsJSON, &connector.SecretRef, &status, &createdBy,
		&connector.CreatedAt, &connector.UpdatedAt, &connector.DeletedAt,
	); err != nil {
		return nil, err
	}
	connector.Type = domain.ConnectorType(connectorT)
	connector.Status = domain.Status(status)
	if createdBy != nil {
		connector.CreatedBy = *createdBy
	}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &connector.Config); err != nil {
			return nil, fmt.Errorf("decode connector config: %w", err)
		}
	}
	if connector.Config == nil {
		connector.Config = map[string]any{}
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &connector.Tags); err != nil {
			return nil, fmt.Errorf("decode connector tags: %w", err)
		}
	}
	if connector.Tags == nil {
		connector.Tags = []string{}
	}
	return &connector, nil
}

func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return parsed.String()
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: connector name already exists", domain.ErrConflict)
		case "23514": // check_violation
			return fmt.Errorf("%w: %s", domain.ErrInvalidConfig, pgErr.ConstraintName)
		}
	}
	return fmt.Errorf("write connector: %w", err)
}
