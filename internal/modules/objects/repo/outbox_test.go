package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	objects "github.com/klb3713/ontoaxis/internal/modules/objects"
	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/modules/objects/repo"
	"github.com/klb3713/ontoaxis/internal/platform/db"
)

// outboxTestPool opens the integration database named by
// ONTOAXIS_TEST_DATABASE_URL, applies the platform and objects migrations and
// returns a pool. Tests are skipped when the variable is unset.
func outboxTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("ONTOAXIS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ONTOAXIS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := db.MigratePlatform(ctx, dsn); err != nil {
		t.Fatalf("migrate platform schema: %v", err)
	}
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	statements, err := objects.MigrationStatements()
	if err != nil {
		t.Fatalf("load objects migration statements: %v", err)
	}
	if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		t.Fatalf("migrate objects schema: %v", err)
	}
	return pool
}

func cleanupTenant(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM jobs.outbox WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM object_data.objects WHERE tenant_id = $1`, tenantID)
	})
}

type outboxRow struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	EventType     string
	Payload       []byte
	PublishedAt   *time.Time
}

func loadOutboxRows(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) []outboxRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT id, tenant_id, aggregate_type, aggregate_id, event_type, payload, published_at
FROM jobs.outbox WHERE tenant_id = $1 ORDER BY created_at, id`, tenantID)
	if err != nil {
		t.Fatalf("query jobs.outbox: %v", err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.ID, &row.TenantID, &row.AggregateType, &row.AggregateID, &row.EventType, &row.Payload, &row.PublishedAt); err != nil {
			t.Fatalf("scan jobs.outbox: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate jobs.outbox: %v", err)
	}
	return out
}

func assertObjectEvent(t *testing.T, row outboxRow, tenantID, objectID, objectTypeID uuid.UUID, eventType, operation string, version int64) {
	t.Helper()
	if row.TenantID != tenantID {
		t.Fatalf("event tenant = %s, want %s", row.TenantID, tenantID)
	}
	if row.AggregateType != repo.OutboxAggregateType {
		t.Fatalf("aggregate_type = %q, want %q", row.AggregateType, repo.OutboxAggregateType)
	}
	if row.AggregateID != objectID {
		t.Fatalf("aggregate_id = %s, want %s", row.AggregateID, objectID)
	}
	if row.EventType != eventType {
		t.Fatalf("event_type = %q, want %q", row.EventType, eventType)
	}
	if row.ID.Version() != 7 {
		t.Fatalf("event id version = %d, want 7 (uuid v7)", row.ID.Version())
	}
	if row.PublishedAt != nil {
		t.Fatalf("published_at = %v, want NULL for a fresh event", row.PublishedAt)
	}

	var payload repo.ObjectEventPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v; payload = %s", err, row.Payload)
	}
	if payload.SchemaVersion != repo.ObjectEventSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", payload.SchemaVersion, repo.ObjectEventSchemaVersion)
	}
	if payload.ObjectID != objectID.String() {
		t.Fatalf("payload object_id = %q, want %q", payload.ObjectID, objectID)
	}
	if payload.ObjectTypeID != objectTypeID.String() {
		t.Fatalf("payload object_type_id = %q, want %q", payload.ObjectTypeID, objectTypeID)
	}
	if payload.Version != version {
		t.Fatalf("payload version = %d, want %d", payload.Version, version)
	}
	if payload.Operation != operation {
		t.Fatalf("payload operation = %q, want %q", payload.Operation, operation)
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.OccurredAt); err != nil {
		t.Fatalf("payload occurred_at = %q is not RFC3339Nano: %v", payload.OccurredAt, err)
	}

	var rawKeys map[string]json.RawMessage
	if err := json.Unmarshal(row.Payload, &rawKeys); err != nil {
		t.Fatalf("decode payload keys: %v", err)
	}
	if len(rawKeys) != 6 {
		t.Fatalf("payload keys = %d, want exactly 6: %s", len(rawKeys), row.Payload)
	}
	for _, key := range []string{"schema_version", "object_id", "object_type_id", "version", "operation", "occurred_at"} {
		if _, ok := rawKeys[key]; !ok {
			t.Fatalf("payload is missing %q: %s", key, row.Payload)
		}
	}
	if _, ok := rawKeys["properties"]; ok {
		t.Fatalf("payload must not contain properties: %s", row.Payload)
	}
}

func TestOutboxStoreLifecycleWritesEvents(t *testing.T) {
	pool := outboxTestPool(t)
	store := repo.NewOutbox(pool)
	ctx := context.Background()
	tenantID := uuid.New()
	objectTypeID := uuid.New()
	cleanupTenant(t, pool, tenantID)

	object, err := store.CreateObject(ctx, tenantID, objectTypeID, map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	if object.Version != 1 {
		t.Fatalf("created version = %d, want 1", object.Version)
	}
	rows := loadOutboxRows(t, pool, tenantID)
	if len(rows) != 1 {
		t.Fatalf("outbox rows after create = %d, want 1", len(rows))
	}
	assertObjectEvent(t, rows[0], tenantID, object.ID, objectTypeID, repo.ObjectCreatedEventType, repo.OutboxOperationCreate, 1)

	updated, err := store.UpdateObject(ctx, tenantID, object.ID, map[string]any{"status": "done"}, false)
	if err != nil {
		t.Fatalf("UpdateObject: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("updated version = %d, want 2", updated.Version)
	}
	rows = loadOutboxRows(t, pool, tenantID)
	if len(rows) != 2 {
		t.Fatalf("outbox rows after update = %d, want 2", len(rows))
	}
	assertObjectEvent(t, rows[1], tenantID, object.ID, objectTypeID, repo.ObjectUpdatedEventType, repo.OutboxOperationUpdate, 2)

	// Link operations delegate to the embedded PostgresStore.
	linkTypeID := uuid.New()
	link, err := store.CreateLink(ctx, tenantID, linkTypeID, object.ID, object.ID, map[string]any{"role": "self"})
	if err != nil {
		t.Fatalf("CreateLink through OutboxStore: %v", err)
	}
	links, total, err := store.ListLinks(ctx, tenantID, object.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListLinks through OutboxStore: %v", err)
	}
	if total != 1 || len(links) != 1 || links[0].ID != link.ID {
		t.Fatalf("links = %+v total = %d, want the created link", links, total)
	}
	if err := store.DeleteLink(ctx, tenantID, link.ID); err != nil {
		t.Fatalf("DeleteLink through OutboxStore: %v", err)
	}

	if err := store.DeleteObject(ctx, tenantID, object.ID); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	rows = loadOutboxRows(t, pool, tenantID)
	if len(rows) != 3 {
		t.Fatalf("outbox rows after delete = %d, want 3", len(rows))
	}
	assertObjectEvent(t, rows[2], tenantID, object.ID, objectTypeID, repo.ObjectDeletedEventType, repo.OutboxOperationDelete, 2)

	if _, err := store.GetObject(ctx, tenantID, object.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetObject after delete = %v, want domain.ErrNotFound", err)
	}
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM object_data.objects WHERE tenant_id = $1 AND id = $2`, tenantID, object.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("deleted_at = NULL, want a soft delete timestamp")
	}

	// Deleting again is not found and must not append another event.
	if err := store.DeleteObject(ctx, tenantID, object.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteObject = %v, want domain.ErrNotFound", err)
	}
	if rows = loadOutboxRows(t, pool, tenantID); len(rows) != 3 {
		t.Fatalf("outbox rows after failed delete = %d, want 3", len(rows))
	}
}

// installOutboxRejectTrigger creates a trigger that rejects every outbox insert
// for tenantID. It returns a cleanup function.
func installOutboxRejectTrigger(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")
	functionName := "outbox_test_reject_" + suffix
	triggerName := "outbox_test_reject_" + suffix
	ctx := context.Background()
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	RAISE EXCEPTION 'outbox insert rejected by test trigger';
END;
$$`, functionName)); err != nil {
		t.Fatalf("create test trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON jobs.outbox
FOR EACH ROW WHEN (NEW.tenant_id = '%s'::uuid) EXECUTE FUNCTION %s()`, triggerName, tenantID, functionName)); err != nil {
		t.Fatalf("create test trigger: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON jobs.outbox`, triggerName))
		_, _ = pool.Exec(cleanupCtx, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})
}

func TestOutboxStoreCreateRollsBackWhenEventInsertFails(t *testing.T) {
	pool := outboxTestPool(t)
	store := repo.NewOutbox(pool)
	ctx := context.Background()
	tenantID := uuid.New()
	cleanupTenant(t, pool, tenantID)
	installOutboxRejectTrigger(t, pool, tenantID)

	if _, err := store.CreateObject(ctx, tenantID, uuid.New(), map[string]any{"name": "should-not-exist"}); err == nil {
		t.Fatal("CreateObject succeeded, want error when the outbox insert fails")
	}

	var objectCount, outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_data.objects WHERE tenant_id = $1`, tenantID).Scan(&objectCount); err != nil {
		t.Fatalf("count objects: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs.outbox WHERE tenant_id = $1`, tenantID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if objectCount != 0 {
		t.Fatalf("object rows = %d, want 0 after rollback", objectCount)
	}
	if outboxCount != 0 {
		t.Fatalf("outbox rows = %d, want 0 after rollback", outboxCount)
	}
}

func TestOutboxStoreDeleteRollsBackWhenEventInsertFails(t *testing.T) {
	pool := outboxTestPool(t)
	store := repo.NewOutbox(pool)
	ctx := context.Background()
	tenantID := uuid.New()
	objectTypeID := uuid.New()
	cleanupTenant(t, pool, tenantID)

	object, err := store.CreateObject(ctx, tenantID, objectTypeID, map[string]any{"name": "keep-me"})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	installOutboxRejectTrigger(t, pool, tenantID)

	if err := store.DeleteObject(ctx, tenantID, object.ID); err == nil {
		t.Fatal("DeleteObject succeeded, want error when the outbox insert fails")
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM object_data.objects WHERE tenant_id = $1 AND id = $2`, tenantID, object.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	if deletedAt != nil {
		t.Fatalf("deleted_at = %v, want NULL after rollback", deletedAt)
	}
	rows := loadOutboxRows(t, pool, tenantID)
	if len(rows) != 1 {
		t.Fatalf("outbox rows = %d, want only the original created event", len(rows))
	}
	if _, err := store.GetObject(ctx, tenantID, object.ID); err != nil {
		t.Fatalf("GetObject after rolled-back delete: %v", err)
	}
}
