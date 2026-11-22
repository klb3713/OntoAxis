// Package repo implements the Ontology persistence port on PostgreSQL. It is
// internal to the ontology module and every statement is scoped by tenant_id.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
	"github.com/klb3713/ontoaxis/internal/modules/ontology/service"
)

var _ service.Store = (*PostgresStore)(nil)

// PostgresStore is the PostgreSQL implementation of service.Store.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// New creates a repository backed by pool.
func New(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// withTenantTx runs fn inside a transaction whose local app.tenant_id setting
// drives the row-level security policies of the ontology schema.
func (s *PostgresStore) withTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("ontology: database pool is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("set app.tenant_id: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant transaction: %w", err)
	}
	return nil
}

func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return domain.ErrAlreadyExists
		case "23503": // foreign_key_violation
			return domain.ErrNotFound
		case "23514": // check_violation
			return domain.ErrInvalidArgument
		}
	}
	return err
}

const objectTypeColumns = `id, tenant_id, name, display_name, description, primary_key_property,
	COALESCE(backing_dataset_id::text, ''), COALESCE(dataset_sync_mode, ''), status, created_at, updated_at`

func scanObjectType(row pgx.Row) (*domain.ObjectType, error) {
	var objectType domain.ObjectType
	var status string
	err := row.Scan(
		&objectType.ID,
		&objectType.TenantID,
		&objectType.Name,
		&objectType.DisplayName,
		&objectType.Description,
		&objectType.PrimaryKeyProperty,
		&objectType.BackingDatasetID,
		&objectType.DatasetSyncMode,
		&status,
		&objectType.CreatedAt,
		&objectType.UpdatedAt,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	objectType.Status = domain.Status(status)
	return &objectType, nil
}

const propertyColumns = `id, tenant_id, object_type_id, name, display_name, description, property_type,
	required, unique_constraint, config, created_at, updated_at`

func scanProperty(row pgx.Row) (*domain.Property, error) {
	var property domain.Property
	var config []byte
	err := row.Scan(
		&property.ID,
		&property.TenantID,
		&property.ObjectTypeID,
		&property.Name,
		&property.DisplayName,
		&property.Description,
		&property.PropertyType,
		&property.Required,
		&property.UniqueConstraint,
		&config,
		&property.CreatedAt,
		&property.UpdatedAt,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	property.Config = map[string]any{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &property.Config); err != nil {
			return nil, fmt.Errorf("decode property config: %w", err)
		}
	}
	return &property, nil
}

const linkTypeColumns = `id, tenant_id, name, display_name, description, source_type_id, target_type_id,
	cardinality, created_at, updated_at`

func scanLinkType(row pgx.Row) (*domain.LinkType, error) {
	var linkType domain.LinkType
	var cardinality string
	err := row.Scan(
		&linkType.ID,
		&linkType.TenantID,
		&linkType.Name,
		&linkType.DisplayName,
		&linkType.Description,
		&linkType.SourceTypeID,
		&linkType.TargetTypeID,
		&cardinality,
		&linkType.CreatedAt,
		&linkType.UpdatedAt,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	linkType.Cardinality = domain.Cardinality(cardinality)
	return &linkType, nil
}

func (s *PostgresStore) CreateObjectType(ctx context.Context, objectType *domain.ObjectType) error {
	query := `INSERT INTO ontology.object_types
		(id, tenant_id, name, display_name, description, primary_key_property, backing_dataset_id, dataset_sync_mode, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, NULLIF($8, ''), $9, $10, $11)`
	err := s.withTenantTx(ctx, objectType.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query,
			objectType.ID,
			objectType.TenantID,
			objectType.Name,
			objectType.DisplayName,
			objectType.Description,
			objectType.PrimaryKeyProperty,
			objectType.BackingDatasetID,
			objectType.DatasetSyncMode,
			string(objectType.Status),
			objectType.CreatedAt,
			objectType.UpdatedAt,
		)
		return mapPgError(err)
	})
	if err != nil {
		return fmt.Errorf("create object type: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetObjectType(ctx context.Context, tenantID, id string) (*domain.ObjectType, error) {
	query := `SELECT ` + objectTypeColumns + ` FROM ontology.object_types
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	var result *domain.ObjectType
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		result, err = scanObjectType(tx.QueryRow(ctx, query, tenantID, id))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get object type: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListObjectTypes(ctx context.Context, tenantID string, offset, limit int) ([]domain.ObjectType, int, error) {
	var items []domain.ObjectType
	var total int
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ontology.object_types WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
			return mapPgError(err)
		}
		rows, err := tx.Query(ctx, `SELECT `+objectTypeColumns+` FROM ontology.object_types
			WHERE tenant_id = $1 AND deleted_at IS NULL
			ORDER BY created_at, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return mapPgError(err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanObjectType(rows)
			if err != nil {
				return err
			}
			items = append(items, *item)
		}
		return mapPgError(rows.Err())
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list object types: %w", err)
	}
	if items == nil {
		items = []domain.ObjectType{}
	}
	return items, total, nil
}

func (s *PostgresStore) UpdateObjectType(ctx context.Context, objectType *domain.ObjectType) error {
	query := `UPDATE ontology.object_types
		SET display_name = $3, description = $4, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	err := s.withTenantTx(ctx, objectType.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, objectType.TenantID, objectType.ID, objectType.DisplayName, objectType.Description, objectType.UpdatedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update object type: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteObjectType(ctx context.Context, tenantID, id string, deletedAt time.Time) error {
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var referenced bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM ontology.link_types
			WHERE tenant_id = $1 AND deleted_at IS NULL AND (source_type_id = $2 OR target_type_id = $2)
		)`, tenantID, id).Scan(&referenced); err != nil {
			return mapPgError(err)
		}
		if referenced {
			return domain.ErrFailedPrecondition
		}
		tag, err := tx.Exec(ctx, `UPDATE ontology.object_types
			SET deleted_at = $3, updated_at = $3
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id, deletedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE ontology.properties
			SET deleted_at = $3, updated_at = $3
			WHERE tenant_id = $1 AND object_type_id = $2 AND deleted_at IS NULL`, tenantID, id, deletedAt); err != nil {
			return mapPgError(err)
		}
		return nil
	})
}

func (s *PostgresStore) HasLinkTypesReferencing(ctx context.Context, tenantID, objectTypeID string) (bool, error) {
	var referenced bool
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return mapPgError(tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM ontology.link_types
			WHERE tenant_id = $1 AND deleted_at IS NULL AND (source_type_id = $2 OR target_type_id = $2)
		)`, tenantID, objectTypeID).Scan(&referenced))
	})
	if err != nil {
		return false, fmt.Errorf("check link type references: %w", err)
	}
	return referenced, nil
}

func (s *PostgresStore) CreateProperty(ctx context.Context, property *domain.Property) error {
	config, err := json.Marshal(property.Config)
	if err != nil {
		return fmt.Errorf("encode property config: %w", err)
	}
	query := `INSERT INTO ontology.properties
		(id, tenant_id, object_type_id, name, display_name, description, property_type, required, unique_constraint, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	err = s.withTenantTx(ctx, property.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query,
			property.ID,
			property.TenantID,
			property.ObjectTypeID,
			property.Name,
			property.DisplayName,
			property.Description,
			property.PropertyType,
			property.Required,
			property.UniqueConstraint,
			config,
			property.CreatedAt,
			property.UpdatedAt,
		)
		return mapPgError(err)
	})
	if err != nil {
		return fmt.Errorf("create property: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetProperty(ctx context.Context, tenantID, id string) (*domain.Property, error) {
	query := `SELECT ` + propertyColumns + ` FROM ontology.properties
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	var result *domain.Property
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		result, err = scanProperty(tx.QueryRow(ctx, query, tenantID, id))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get property: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListProperties(ctx context.Context, tenantID, objectTypeID string) ([]domain.Property, error) {
	var items []domain.Property
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+propertyColumns+` FROM ontology.properties
			WHERE tenant_id = $1 AND object_type_id = $2 AND deleted_at IS NULL
			ORDER BY name, id`, tenantID, objectTypeID)
		if err != nil {
			return mapPgError(err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanProperty(rows)
			if err != nil {
				return err
			}
			items = append(items, *item)
		}
		return mapPgError(rows.Err())
	})
	if err != nil {
		return nil, fmt.Errorf("list properties: %w", err)
	}
	if items == nil {
		items = []domain.Property{}
	}
	return items, nil
}

func (s *PostgresStore) UpdateProperty(ctx context.Context, property *domain.Property) error {
	query := `UPDATE ontology.properties
		SET display_name = $3, description = $4, required = $5, unique_constraint = $6, updated_at = $7
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	err := s.withTenantTx(ctx, property.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, property.TenantID, property.ID, property.DisplayName, property.Description, property.Required, property.UniqueConstraint, property.UpdatedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update property: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteProperty(ctx context.Context, tenantID, id string, deletedAt time.Time) error {
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ontology.properties SET deleted_at = $3, updated_at = $3
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id, deletedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete property: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateLinkType(ctx context.Context, linkType *domain.LinkType) error {
	query := `INSERT INTO ontology.link_types
		(id, tenant_id, name, display_name, description, source_type_id, target_type_id, cardinality, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	err := s.withTenantTx(ctx, linkType.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, query,
			linkType.ID,
			linkType.TenantID,
			linkType.Name,
			linkType.DisplayName,
			linkType.Description,
			linkType.SourceTypeID,
			linkType.TargetTypeID,
			string(linkType.Cardinality),
			linkType.CreatedAt,
			linkType.UpdatedAt,
		)
		return mapPgError(err)
	})
	if err != nil {
		return fmt.Errorf("create link type: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetLinkType(ctx context.Context, tenantID, id string) (*domain.LinkType, error) {
	query := `SELECT ` + linkTypeColumns + ` FROM ontology.link_types
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	var result *domain.LinkType
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		result, err = scanLinkType(tx.QueryRow(ctx, query, tenantID, id))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get link type: %w", err)
	}
	return result, nil
}

func (s *PostgresStore) ListLinkTypes(ctx context.Context, tenantID string, offset, limit int) ([]domain.LinkType, int, error) {
	var items []domain.LinkType
	var total int
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ontology.link_types WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
			return mapPgError(err)
		}
		rows, err := tx.Query(ctx, `SELECT `+linkTypeColumns+` FROM ontology.link_types
			WHERE tenant_id = $1 AND deleted_at IS NULL
			ORDER BY created_at, id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return mapPgError(err)
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanLinkType(rows)
			if err != nil {
				return err
			}
			items = append(items, *item)
		}
		return mapPgError(rows.Err())
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list link types: %w", err)
	}
	if items == nil {
		items = []domain.LinkType{}
	}
	return items, total, nil
}

func (s *PostgresStore) UpdateLinkType(ctx context.Context, linkType *domain.LinkType) error {
	query := `UPDATE ontology.link_types
		SET display_name = $3, description = $4, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	err := s.withTenantTx(ctx, linkType.TenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, linkType.TenantID, linkType.ID, linkType.DisplayName, linkType.Description, linkType.UpdatedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update link type: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteLinkType(ctx context.Context, tenantID, id string, deletedAt time.Time) error {
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ontology.link_types SET deleted_at = $3, updated_at = $3
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id, deletedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete link type: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetDatasetBinding(ctx context.Context, tenantID, objectTypeID, datasetID, syncMode string, updatedAt time.Time) error {
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ontology.object_types
			SET backing_dataset_id = $3::uuid, dataset_sync_mode = $4, updated_at = $5
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, objectTypeID, datasetID, syncMode, updatedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set dataset binding: %w", err)
	}
	return nil
}

func (s *PostgresStore) ClearDatasetBinding(ctx context.Context, tenantID, objectTypeID string, updatedAt time.Time) error {
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ontology.object_types
			SET backing_dataset_id = NULL, dataset_sync_mode = NULL, updated_at = $3
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, objectTypeID, updatedAt)
		if err != nil {
			return mapPgError(err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clear dataset binding: %w", err)
	}
	return nil
}
