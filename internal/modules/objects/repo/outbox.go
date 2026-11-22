package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

// Outbox aggregate and event identifiers. They are part of the cross-module
// contract consumed by the search indexer and MUST stay stable.
const (
	// OutboxAggregateType is the aggregate_type written for object events.
	OutboxAggregateType = "object"

	// ObjectCreatedEventType is emitted after a successful object create.
	ObjectCreatedEventType = "objects.object.created.v1"
	// ObjectUpdatedEventType is emitted after a successful object update.
	ObjectUpdatedEventType = "objects.object.updated.v1"
	// ObjectDeletedEventType is emitted after a successful object delete.
	ObjectDeletedEventType = "objects.object.deleted.v1"

	// ObjectEventSchemaVersion is the schema_version of the event payload.
	ObjectEventSchemaVersion = 1

	// OutboxOperationCreate/Update/Delete are the operation values of the payload.
	OutboxOperationCreate = "create"
	OutboxOperationUpdate = "update"
	OutboxOperationDelete = "delete"
)

// ObjectEventPayload is the outbox payload for object lifecycle events. It is
// deliberately minimal: it carries identifiers and version metadata only, never
// property values, because the indexer resolves the full document separately.
type ObjectEventPayload struct {
	SchemaVersion int    `json:"schema_version"`
	ObjectID      string `json:"object_id"`
	ObjectTypeID  string `json:"object_type_id"`
	Version       int64  `json:"version"`
	Operation     string `json:"operation"`
	OccurredAt    string `json:"occurred_at"`
}

// OutboxStore is a Store implementation that writes an outbox event in the same
// transaction as every object mutation. If the outbox insert fails, the object
// write is rolled back and the caller receives an error: a business write never
// succeeds without its event.
type OutboxStore struct {
	*PostgresStore
}

var _ Store = (*OutboxStore)(nil)

// NewOutbox returns an OutboxStore backed by pool. Link operations delegate to
// the embedded PostgresStore.
func NewOutbox(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{PostgresStore: New(pool)}
}

// CreateObject inserts the object and its created event atomically.
func (s *OutboxStore) CreateObject(ctx context.Context, tenantID, objectTypeID uuid.UUID, properties map[string]any) (domain.Object, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Object{}, fmt.Errorf("generate object id: %w", err)
	}
	payload, err := json.Marshal(properties)
	if err != nil {
		return domain.Object{}, fmt.Errorf("encode properties: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Object{}, fmt.Errorf("begin object transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `INSERT INTO object_data.objects (id, tenant_id, object_type_id, properties)
VALUES ($1, $2, $3, $4::jsonb)
RETURNING id, tenant_id, object_type_id, version, properties, created_at, updated_at`
	var o domain.Object
	var raw []byte
	if err := tx.QueryRow(ctx, q, id, tenantID, objectTypeID, payload).Scan(
		&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return domain.Object{}, err
	}
	_ = json.Unmarshal(raw, &o.Properties)

	if err := insertObjectEvent(ctx, tx, tenantID, o.ID, o.ObjectTypeID, o.Version, ObjectCreatedEventType, OutboxOperationCreate, time.Now().UTC()); err != nil {
		return domain.Object{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Object{}, fmt.Errorf("commit object transaction: %w", err)
	}
	return o, nil
}

// UpdateObject updates the object and writes its updated event atomically.
func (s *OutboxStore) UpdateObject(ctx context.Context, tenantID, id uuid.UUID, properties map[string]any, replace bool) (domain.Object, error) {
	payload, err := json.Marshal(properties)
	if err != nil {
		return domain.Object{}, fmt.Errorf("encode properties: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Object{}, fmt.Errorf("begin object transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `UPDATE object_data.objects
SET properties = CASE WHEN $3 THEN $4::jsonb ELSE properties || $4::jsonb END,
    version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING id, tenant_id, object_type_id, version, properties, created_at, updated_at`
	var o domain.Object
	var raw []byte
	err = tx.QueryRow(ctx, q, tenantID, id, replace, payload).Scan(
		&o.ID, &o.TenantID, &o.ObjectTypeID, &o.Version, &raw, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Object{}, err
	}
	_ = json.Unmarshal(raw, &o.Properties)

	if err := insertObjectEvent(ctx, tx, tenantID, o.ID, o.ObjectTypeID, o.Version, ObjectUpdatedEventType, OutboxOperationUpdate, time.Now().UTC()); err != nil {
		return domain.Object{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Object{}, fmt.Errorf("commit object transaction: %w", err)
	}
	return o, nil
}

// DeleteObject soft-deletes the object and writes its deleted event atomically.
// The object is read inside the transaction first so the event carries the
// object_type_id and the version that was current at deletion time.
func (s *OutboxStore) DeleteObject(ctx context.Context, tenantID, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin object transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const selectQ = `SELECT object_type_id, version FROM object_data.objects
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
FOR UPDATE`
	var objectTypeID uuid.UUID
	var version int64
	if err := tx.QueryRow(ctx, selectQ, tenantID, id).Scan(&objectTypeID, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE object_data.objects SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	if err := insertObjectEvent(ctx, tx, tenantID, id, objectTypeID, version, ObjectDeletedEventType, OutboxOperationDelete, time.Now().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit object transaction: %w", err)
	}
	return nil
}

// insertObjectEvent appends one outbox row inside tx.
func insertObjectEvent(ctx context.Context, tx pgx.Tx, tenantID, objectID, objectTypeID uuid.UUID, version int64, eventType, operation string, occurredAt time.Time) error {
	eventID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate outbox event id: %w", err)
	}
	payload, err := json.Marshal(ObjectEventPayload{
		SchemaVersion: ObjectEventSchemaVersion,
		ObjectID:      objectID.String(),
		ObjectTypeID:  objectTypeID.String(),
		Version:       version,
		Operation:     operation,
		OccurredAt:    occurredAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("encode outbox event payload: %w", err)
	}
	const q = `INSERT INTO jobs.outbox (id, tenant_id, aggregate_type, aggregate_id, event_type, payload)
VALUES ($1, $2, $3, $4, $5, $6::jsonb)`
	if _, err := tx.Exec(ctx, q, eventID, tenantID, OutboxAggregateType, objectID, eventType, payload); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}
