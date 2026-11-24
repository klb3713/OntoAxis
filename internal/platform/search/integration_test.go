//go:build integration

package search_test

// These tests exercise the object search platform against a real PostgreSQL
// database and a real Meilisearch instance. They are gated behind the
// ONTOAXIS_SEARCH_TEST_* variables so the default unit suite never needs
// Docker:
//
//	ONTOAXIS_SEARCH_TEST_DATABASE_URL='postgres://ontoaxis:ontoaxis@127.0.0.1:55432/ontoaxis?sslmode=disable' \
//	ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL='http://127.0.0.1:17701' \
//	ONTOAXIS_SEARCH_TEST_API_KEY='local-test-master-key' \
//	go test -tags=integration -p 1 -v ./internal/platform/search/
//
// CI sets ONTOAXIS_SEARCH_TEST_REQUIRED=1 so a missing variable fails instead
// of silently skipping the job.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	objects "github.com/klb3713/ontoaxis/internal/modules/objects"
	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
	"github.com/klb3713/ontoaxis/internal/platform/db"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

const (
	envDatabaseURL  = "ONTOAXIS_SEARCH_TEST_DATABASE_URL"
	envMeilisearch  = "ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL"
	envAPIKey       = "ONTOAXIS_SEARCH_TEST_API_KEY"
	envTestRequired = "ONTOAXIS_SEARCH_TEST_REQUIRED"
)

type searchEnv struct {
	pool     *pgxpool.Pool
	client   *search.Client
	cfg      search.Config
	meiliURL string
	apiKey   string
}

var (
	migrateOnce sync.Once
	migrateErr  error
)

// testEnv connects to the integration services, applies platform, objects and
// search schema, and returns the shared handles for one test.
func testEnv(t *testing.T) *searchEnv {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(envDatabaseURL))
	meiliURL := strings.TrimSpace(os.Getenv(envMeilisearch))
	if dsn == "" || meiliURL == "" {
		if os.Getenv(envTestRequired) == "1" {
			t.Fatalf("%s and %s must be set when %s=1 (CI must not skip integration tests)", envDatabaseURL, envMeilisearch, envTestRequired)
		}
		t.Skipf("set %s, %s and %s to run integration tests", envDatabaseURL, envMeilisearch, envAPIKey)
	}
	apiKey := strings.TrimSpace(os.Getenv(envAPIKey))

	cfg := search.DefaultConfig()
	cfg.Enabled = true
	cfg.URL = meiliURL
	cfg.APIKey = apiKey
	cfg.Timeout = 5 * time.Second
	cfg.SyncWindow = 5 * time.Second
	cfg.ConsumerBatch = 50
	cfg.MaxAttempts = 3
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid integration search config: %v", err)
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
	if statements, err := objects.MigrationStatements(); err != nil {
		t.Fatalf("load objects migration statements: %v", err)
	} else if err := db.ExecStatements(ctx, pool, statements...); err != nil {
		t.Fatalf("migrate objects schema: %v", err)
	}
	migrateOnce.Do(func() { migrateErr = search.Migrate(ctx, pool) })
	if migrateErr != nil {
		t.Fatalf("migrate search schema: %v", migrateErr)
	}

	client, err := search.NewClient(meiliURL, apiKey,
		search.WithTimeout(cfg.Timeout),
		search.WithWaitTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("new meilisearch client: %v", err)
	}
	if err := client.Health(ctx); err != nil {
		t.Fatalf("meilisearch health: %v", err)
	}
	return &searchEnv{pool: pool, client: client, cfg: cfg, meiliURL: meiliURL, apiKey: apiKey}
}

func (e *searchEnv) newTenant(t *testing.T) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	e.cleanupTenant(t, tenantID)
	return tenantID
}

func (e *searchEnv) cleanupTenant(t *testing.T, tenantID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = e.pool.Exec(ctx, `DELETE FROM search.outbox_attempt WHERE event_id IN (SELECT id FROM jobs.outbox WHERE tenant_id = $1)`, tenantID)
		_, _ = e.pool.Exec(ctx, `DELETE FROM jobs.outbox WHERE tenant_id = $1`, tenantID)
		_, _ = e.pool.Exec(ctx, `DELETE FROM object_data.objects WHERE tenant_id = $1`, tenantID)
		_, _ = e.pool.Exec(ctx, `DELETE FROM search.index_state WHERE tenant_id = $1`, tenantID)
		e.deleteTenantIndexes(ctx, t, tenantID)
	})
}

func (e *searchEnv) indexUID(tenantID uuid.UUID) string {
	return search.IndexName(e.cfg.IndexVersion, tenantID.String())
}

func (e *searchEnv) deleteTenantIndexes(ctx context.Context, t *testing.T, tenantID uuid.UUID) {
	t.Helper()
	prefix := e.indexUID(tenantID)
	for _, uid := range e.listIndexUIDs(ctx, t) {
		if strings.HasPrefix(uid, prefix) {
			if err := e.client.DeleteIndex(ctx, uid); err != nil {
				t.Logf("cleanup: delete index %s: %v", uid, err)
			}
		}
	}
}

func (e *searchEnv) listIndexUIDs(ctx context.Context, t *testing.T) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.meiliURL+"/indexes?limit=1000", nil)
	if err != nil {
		t.Fatalf("build list indexes request: %v", err)
	}
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read list indexes response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list indexes status = %d body=%s", resp.StatusCode, raw)
	}
	var payload struct {
		Results []struct {
			UID string `json:"uid"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode list indexes: %v", err)
	}
	uids := make([]string, 0, len(payload.Results))
	for _, result := range payload.Results {
		uids = append(uids, result.UID)
	}
	return uids
}

func (e *searchEnv) indexSettings(ctx context.Context, t *testing.T, uid string) map[string]json.RawMessage {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.meiliURL+"/indexes/"+uid+"/settings", nil)
	if err != nil {
		t.Fatalf("build index settings request: %v", err)
	}
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get index settings: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index settings: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get index settings status = %d body=%s", resp.StatusCode, raw)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("decode index settings: %v", err)
	}
	return settings
}

func (e *searchEnv) countDocuments(ctx context.Context, t *testing.T, uid, filter string) int64 {
	t.Helper()
	result, err := e.client.Search(ctx, uid, search.SearchRequest{Q: "", Filter: filter, Limit: 1})
	if err != nil {
		t.Fatalf("count documents in %s (filter %q): %v", uid, filter, err)
	}
	return result.EstimatedTotalHits
}

// documentIDs pages through an index and returns every hit id. It also fails
// when a page repeats an id, which would hide pagination defects.
func (e *searchEnv) documentIDs(ctx context.Context, t *testing.T, uid, filter string) []string {
	t.Helper()
	const pageSize = 1000
	var ids []string
	seen := map[string]struct{}{}
	for offset := 0; ; offset += pageSize {
		result, err := e.client.Search(ctx, uid, search.SearchRequest{Q: "", Filter: filter, Limit: pageSize, Offset: offset})
		if err != nil {
			t.Fatalf("page documents in %s at offset %d: %v", uid, offset, err)
		}
		for _, hit := range result.Hits {
			if _, ok := seen[hit.ID]; ok {
				t.Fatalf("pagination returned duplicate document id %s at offset %d", hit.ID, offset)
			}
			seen[hit.ID] = struct{}{}
			ids = append(ids, hit.ID)
		}
		if len(result.Hits) < pageSize || int64(offset+len(result.Hits)) >= result.EstimatedTotalHits {
			return ids
		}
	}
}

func (e *searchEnv) insertObject(t *testing.T, id, tenantID, objectTypeID uuid.UUID, properties map[string]any) domain.Object {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(properties)
	if err != nil {
		t.Fatalf("encode properties: %v", err)
	}
	var createdAt, updatedAt time.Time
	err = e.pool.QueryRow(ctx, `
INSERT INTO object_data.objects (id, tenant_id, object_type_id, properties, version, created_at, updated_at)
VALUES ($1, $2, $3, $4, 1, now(), now())
RETURNING created_at, updated_at`, id, tenantID, objectTypeID, raw).Scan(&createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("insert object %s: %v", id, err)
	}
	return domain.Object{
		ID:           id,
		TenantID:     tenantID,
		ObjectTypeID: objectTypeID,
		Version:      1,
		Properties:   properties,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}

func (e *searchEnv) updateObjectProperties(t *testing.T, id uuid.UUID, properties map[string]any) {
	t.Helper()
	raw, err := json.Marshal(properties)
	if err != nil {
		t.Fatalf("encode properties: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(), `
UPDATE object_data.objects SET properties = $2, version = version + 1, updated_at = now() WHERE id = $1`, id, raw); err != nil {
		t.Fatalf("update object %s: %v", id, err)
	}
}

func (e *searchEnv) softDeleteObject(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE object_data.objects SET deleted_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("soft delete object %s: %v", id, err)
	}
}

func (e *searchEnv) insertOutboxEvent(t *testing.T, eventID, tenantID, aggregateID uuid.UUID, eventType string, createdAt time.Time) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"object_id":      aggregateID.String(),
		"operation":      strings.TrimSuffix(strings.TrimPrefix(eventType, "objects.object."), ".v1"),
		"occurred_at":    createdAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("encode outbox payload: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(), `
INSERT INTO jobs.outbox (id, tenant_id, aggregate_type, aggregate_id, event_type, payload, created_at)
VALUES ($1, $2, 'object', $3, $4, $5, $6)`, eventID, tenantID, aggregateID, eventType, payload, createdAt); err != nil {
		t.Fatalf("insert outbox event %s: %v", eventID, err)
	}
}

func (e *searchEnv) newRebuilder() *search.Rebuilder {
	return search.NewRebuilder(e.pool, e.client, e.cfg, search.NopRecorder{}, slog.Default(), nil)
}

func newObjectID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generate object id: %v", err)
	}
	return id
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

func waitFor(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, description)
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestIntegrationMigrationUpDownUp covers task 1.4: the worker-owned search
// migration must apply, roll back and re-apply on a real PostgreSQL.
func TestIntegrationMigrationUpDownUp(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()

	assertSearchTables := func(step string) {
		t.Helper()
		want := map[string][]string{
			"outbox_attempt": {"event_id", "attempts", "next_attempt_at", "last_error", "dead_at", "updated_at"},
			"index_state":    {"tenant_id", "index_version", "index_uid", "last_event_at", "last_success_at", "rebuilt_at"},
		}
		for table, columns := range want {
			rows, err := env.pool.Query(ctx, `
SELECT column_name FROM information_schema.columns
WHERE table_schema = 'search' AND table_name = $1 ORDER BY ordinal_position`, table)
			if err != nil {
				t.Fatalf("%s: query columns for search.%s: %v", step, table, err)
			}
			var got []string
			for rows.Next() {
				var column string
				if err := rows.Scan(&column); err != nil {
					rows.Close()
					t.Fatalf("%s: scan column for search.%s: %v", step, table, err)
				}
				got = append(got, column)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatalf("%s: iterate columns for search.%s: %v", step, table, err)
			}
			if strings.Join(got, ",") != strings.Join(columns, ",") {
				t.Fatalf("%s: search.%s columns = %v, want %v", step, table, got, columns)
			}
		}
	}
	assertSearchTables("after up")

	down := gooseDownStatements(t, "migrations/00001_search_schema.up.sql")
	if len(down) == 0 {
		t.Fatal("migration file has no goose Down section")
	}
	for _, statement := range down {
		if _, err := env.pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply goose down statement %q: %v", statement, err)
		}
	}
	var remaining int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'search'`).Scan(&remaining); err != nil {
		t.Fatalf("count search tables after down: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("search tables after down = %d, want 0", remaining)
	}

	if err := search.Migrate(ctx, env.pool); err != nil {
		t.Fatalf("re-apply search migration: %v", err)
	}
	assertSearchTables("after down->up")
	if err := search.Migrate(ctx, env.pool); err != nil {
		t.Fatalf("repeat search migration (idempotency): %v", err)
	}
	assertSearchTables("after repeated up")
}

func gooseDownStatements(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	content := string(raw)
	idx := strings.Index(content, "-- +goose Down")
	if idx < 0 {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(content[idx+len("-- +goose Down"):], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		lines = append(lines, line)
	}
	var statements []string
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

// blockingApplier pauses the first document upsert so a test can hold the
// outbox row lock while another consumer tries to claim.
type blockingApplier struct {
	inner   search.ClientApplier
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *blockingApplier) EnsureIndex(ctx context.Context, uid string, settings json.RawMessage) error {
	return b.inner.EnsureIndex(ctx, uid, settings)
}

func (b *blockingApplier) UpsertDocuments(ctx context.Context, uid string, docs []search.Document) error {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.inner.UpsertDocuments(ctx, uid, docs)
}

func (b *blockingApplier) DeleteDocuments(ctx context.Context, uid string, ids []string) error {
	return b.inner.DeleteDocuments(ctx, uid, ids)
}

// TestIntegrationClaimConcurrent covers task 4.1: while one consumer's
// transaction holds the outbox row lock, a second consumer must skip that row
// (FOR UPDATE SKIP LOCKED) instead of blocking or double-processing, and the
// event must be applied exactly once.
func TestIntegrationClaimConcurrent(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectID := newObjectID(t)
	env.insertObject(t, objectID, tenantID, newObjectID(t), map[string]any{"name": "claim"})
	env.insertOutboxEvent(t, newObjectID(t), tenantID, objectID, "objects.object.created.v1", time.Now().UTC())

	blocking := &blockingApplier{
		inner:   search.ClientApplier{Client: env.client, WaitTimeout: 30 * time.Second},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	firstConsumer := search.NewConsumer(env.pool, blocking, search.NewPostgresObjectReader(env.pool),
		env.cfg, search.NopRecorder{}, slog.Default(), nil)
	secondConsumer := search.NewConsumer(env.pool,
		search.ClientApplier{Client: env.client, WaitTimeout: 30 * time.Second},
		search.NewPostgresObjectReader(env.pool), env.cfg, search.NopRecorder{}, slog.Default(), nil)

	type runResult struct {
		processed int
		err       error
	}
	first := make(chan runResult, 1)
	go func() {
		processed, err := firstConsumer.RunOnce(context.Background())
		first <- runResult{processed: processed, err: err}
	}()
	select {
	case <-blocking.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("first consumer never claimed and applied the event")
	}

	// The first consumer holds the row lock inside its transaction. The second
	// consumer must skip it immediately rather than block on the lock.
	second := make(chan runResult, 1)
	go func() {
		processed, err := secondConsumer.RunOnce(context.Background())
		second <- runResult{processed: processed, err: err}
	}()
	select {
	case result := <-second:
		if result.err != nil {
			t.Fatalf("concurrent consumer: %v", result.err)
		}
		if result.processed != 0 {
			t.Fatalf("concurrent consumer processed %d events, want 0", result.processed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent consumer blocked on the locked outbox row instead of skipping it")
	}

	close(blocking.release)
	select {
	case result := <-first:
		if result.err != nil {
			t.Fatalf("first consumer: %v", result.err)
		}
		if result.processed != 1 {
			t.Fatalf("first consumer processed %d events, want 1", result.processed)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("first consumer did not finish")
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 1 {
		t.Fatalf("documents after concurrent claim = %d, want 1", got)
	}
	assertOutboxPublished(t, env, ctx, tenantID)

	// A later poll observes the published row and claims nothing.
	processed, err := secondConsumer.RunOnce(ctx)
	if err != nil {
		t.Fatalf("consumer after publish: %v", err)
	}
	if processed != 0 {
		t.Fatalf("consumer after publish processed %d events, want 0", processed)
	}
}

// TestIntegrationConsumerAppliesAndPublishes runs the real consumer against
// real PostgreSQL and Meilisearch: read-through create, update and delete.
func TestIntegrationConsumerAppliesAndPublishes(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	objectID := newObjectID(t)
	env.insertObject(t, objectID, tenantID, objectTypeID, map[string]any{"name": "初始名称", "count": 1})
	env.insertOutboxEvent(t, newObjectID(t), tenantID, objectID, "objects.object.created.v1", time.Now().UTC())

	consumer := search.NewConsumer(
		env.pool,
		search.ClientApplier{Client: env.client, WaitTimeout: 30 * time.Second},
		search.NewPostgresObjectReader(env.pool),
		env.cfg,
		search.NopRecorder{},
		slog.Default(),
		nil,
	)
	processed, err := consumer.RunOnce(ctx)
	if err != nil {
		t.Fatalf("consume created event: %v", err)
	}
	if processed < 1 {
		t.Fatalf("processed = %d, want at least 1", processed)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 1 {
		t.Fatalf("documents after create = %d, want 1", got)
	}
	assertOutboxPublished(t, env, ctx, tenantID)

	env.updateObjectProperties(t, objectID, map[string]any{"name": "更新名称", "count": 2})
	env.insertOutboxEvent(t, newObjectID(t), tenantID, objectID, "objects.object.updated.v1", time.Now().UTC())
	if _, err := consumer.RunOnce(ctx); err != nil {
		t.Fatalf("consume updated event: %v", err)
	}
	result, err := env.client.Search(ctx, env.indexUID(tenantID), search.SearchRequest{
		Q: "更新名称", Filter: search.TenantFilter(tenantID.String()), Limit: 10,
	})
	if err != nil {
		t.Fatalf("search updated document: %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].ID != objectID.String() || result.Hits[0].DisplayName != "更新名称" {
		t.Fatalf("updated document hits = %+v", result.Hits)
	}

	env.softDeleteObject(t, objectID)
	env.insertOutboxEvent(t, newObjectID(t), tenantID, objectID, "objects.object.deleted.v1", time.Now().UTC())
	if _, err := consumer.RunOnce(ctx); err != nil {
		t.Fatalf("consume deleted event: %v", err)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 0 {
		t.Fatalf("documents after soft delete = %d, want 0", got)
	}
}

func assertOutboxPublished(t *testing.T, env *searchEnv, ctx context.Context, tenantID uuid.UUID) {
	t.Helper()
	var unpublished int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM jobs.outbox WHERE tenant_id = $1 AND published_at IS NULL`, tenantID).Scan(&unpublished); err != nil {
		t.Fatalf("count unpublished outbox rows: %v", err)
	}
	if unpublished != 0 {
		t.Fatalf("unpublished outbox rows = %d, want 0", unpublished)
	}
}

// TestIntegrationRebuildKeysetPagination covers task 5.2: a rebuild over more
// than two 500-row batches produces exactly the database row set.
func TestIntegrationRebuildKeysetPagination(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)

	const total = 1100
	batch := &pgx.Batch{}
	for i := 0; i < total; i++ {
		id := newObjectID(t)
		batch.Queue(`INSERT INTO object_data.objects (id, tenant_id, object_type_id, properties, created_at, updated_at)
VALUES ($1, $2, $3, '{"name":"批量对象"}'::jsonb, now(), now())`, id, tenantID, objectTypeID)
	}
	if err := env.pool.SendBatch(ctx, batch).Close(); err != nil {
		t.Fatalf("seed %d objects: %v", total, err)
	}

	rebuilder := env.newRebuilder()
	summary, err := rebuilder.Rebuild(ctx, tenantID, nil, true)
	if err != nil {
		t.Fatalf("rebuild tenant: %v", err)
	}
	if summary.Documents != total {
		t.Fatalf("summary.Documents = %d, want %d", summary.Documents, total)
	}
	if !summary.Verified {
		t.Fatal("summary.Verified = false, want true")
	}
	if summary.IndexUID != env.indexUID(tenantID) {
		t.Fatalf("summary.IndexUID = %q, want %q", summary.IndexUID, env.indexUID(tenantID))
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != total {
		t.Fatalf("indexed documents = %d, want %d", got, total)
	}
	var dbIDs []string
	rows, err := env.pool.Query(ctx, `SELECT id::text FROM object_data.objects WHERE tenant_id = $1 ORDER BY id`, tenantID)
	if err != nil {
		t.Fatalf("query seeded ids: %v", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan seeded id: %v", err)
		}
		dbIDs = append(dbIDs, id)
	}
	rows.Close()
	indexed := env.documentIDs(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String()))
	if len(indexed) != total {
		t.Fatalf("paged document count = %d, want %d", len(indexed), total)
	}
	for _, id := range dbIDs {
		if !contains(indexed, id) {
			t.Fatalf("indexed documents are missing database id %s", id)
		}
	}
}

// gatedClient pauses the first staging upsert so a test can observe the live
// index or write concurrent rows while a rebuild is in flight.
type gatedClient struct {
	*search.Client
	stagingPrefix string
	once          sync.Once
	paused        chan struct{}
	resume        chan struct{}
}

func (g *gatedClient) UpsertDocuments(ctx context.Context, uid string, docs []search.Document) (string, error) {
	if strings.HasPrefix(uid, g.stagingPrefix) {
		g.once.Do(func() {
			close(g.paused)
			<-g.resume
		})
	}
	return g.Client.UpsertDocuments(ctx, uid, docs)
}

// TestIntegrationRebuildSwapKeepsPreviousIndex covers task 5.3: queries keep
// working against the live index while a rebuild is in flight, and the swapped
// out generation remains queryable.
func TestIntegrationRebuildSwapKeepsPreviousIndex(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	for i := 0; i < 3; i++ {
		env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": fmt.Sprintf("旧文档-%d", i)})
	}
	if _, err := env.newRebuilder().Rebuild(ctx, tenantID, nil, true); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": "新文档"})

	gate := &gatedClient{
		Client:        env.client,
		stagingPrefix: env.indexUID(tenantID) + "_r",
		paused:        make(chan struct{}),
		resume:        make(chan struct{}),
	}
	rebuilder := search.NewRebuilder(env.pool, gate, env.cfg, search.NopRecorder{}, slog.Default(), nil)
	type rebuildResult struct {
		summary search.Summary
		err     error
	}
	done := make(chan rebuildResult, 1)
	go func() {
		summary, err := rebuilder.Rebuild(context.Background(), tenantID, nil, true)
		done <- rebuildResult{summary: summary, err: err}
	}()

	select {
	case <-gate.paused:
	case <-time.After(30 * time.Second):
		t.Fatal("rebuild never reached the staging upsert")
	}
	// While the swap has not happened, the live index still serves the old
	// generation without errors.
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 3 {
		t.Fatalf("live index documents during rebuild = %d, want 3", got)
	}
	close(gate.resume)
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("rebuild after swap: %v", result.err)
		}
		if result.summary.Documents != 4 || !result.summary.Verified {
			t.Fatalf("summary = %+v, want 4 documents and verified", result.summary)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("rebuild did not finish")
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 4 {
		t.Fatalf("live index documents after rebuild = %d, want 4", got)
	}

	// The staging name now holds the previous (pre-swap) generation: three
	// documents remain queryable there.
	var oldGenerationUID string
	for _, uid := range env.listIndexUIDs(ctx, t) {
		if !strings.HasPrefix(uid, env.indexUID(tenantID)+"_r") {
			continue
		}
		if got := env.countDocuments(ctx, t, uid, search.TenantFilter(tenantID.String())); got == 3 {
			oldGenerationUID = uid
			break
		}
	}
	if oldGenerationUID == "" {
		t.Fatal("no staging index retained the previous 3-document generation after swap")
	}
}

// TestIntegrationRebuildReplaysWindowEvents covers task 5.4: a row committed
// after the scan cursor passed its id is still indexed through the outbox
// replay window.
func TestIntegrationRebuildReplaysWindowEvents(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	for i := 0; i < 3; i++ {
		env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": fmt.Sprintf("扫描前-%d", i)})
	}
	if _, err := env.newRebuilder().Rebuild(ctx, tenantID, nil, true); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}

	gate := &gatedClient{
		Client:        env.client,
		stagingPrefix: env.indexUID(tenantID) + "_r",
		paused:        make(chan struct{}),
		resume:        make(chan struct{}),
	}
	rebuilder := search.NewRebuilder(env.pool, gate, env.cfg, search.NopRecorder{}, slog.Default(), nil)
	type rebuildResult struct {
		summary search.Summary
		err     error
	}
	done := make(chan rebuildResult, 1)
	go func() {
		summary, err := rebuilder.Rebuild(context.Background(), tenantID, nil, true)
		done <- rebuildResult{summary: summary, err: err}
	}()
	select {
	case <-gate.paused:
	case <-time.After(30 * time.Second):
		t.Fatal("rebuild never reached the staging upsert")
	}

	// This id sorts below every seeded UUIDv7 value, so the running keyset scan
	// has already moved past it; only the outbox replay can index it.
	racedID := mustUUID(t, "00000000-0000-4000-8000-000000000001")
	env.insertObject(t, racedID, tenantID, objectTypeID, map[string]any{"name": "窗口内新增"})
	env.insertOutboxEvent(t, newObjectID(t), tenantID, racedID, "objects.object.created.v1", time.Now().UTC())

	close(gate.resume)
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("rebuild with concurrent write: %v", result.err)
		}
		if !result.summary.Verified {
			t.Fatalf("summary.Verified = false after replay: %+v", result.summary)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("rebuild did not finish")
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 4 {
		t.Fatalf("documents after replay = %d, want 4", got)
	}
	if ids := env.documentIDs(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); !contains(ids, racedID.String()) {
		t.Fatalf("window event document %s was not replayed", racedID)
	}
}

// TestIntegrationRebuildVerifyDetectsDrift covers task 5.5: verify detects
// both content drift with an unchanged count and missing documents.
func TestIntegrationRebuildVerifyDetectsDrift(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	first := env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": "对账文档-1"})
	env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": "对账文档-2"})

	rebuilder := env.newRebuilder()
	if _, err := rebuilder.Rebuild(ctx, tenantID, nil, true); err != nil {
		t.Fatalf("consistent rebuild: %v", err)
	}
	if err := rebuilder.Verify(ctx, tenantID); err != nil {
		t.Fatalf("Verify on consistent index: %v", err)
	}

	drifted, err := search.BuildDocument(first)
	if err != nil {
		t.Fatalf("build drift fixture: %v", err)
	}
	// Keep the ID, version, and visible display fields unchanged. Only the
	// full-document read can see drift in properties_text.
	drifted.PropertiesText = "name=changed-without-changing-count"
	taskUID, err := env.client.UpsertDocuments(ctx, env.indexUID(tenantID), []search.Document{drifted})
	if err != nil {
		t.Fatalf("modify one document for drift: %v", err)
	}
	if err := env.client.WaitTask(ctx, env.indexUID(tenantID), taskUID, 30*time.Second); err != nil {
		t.Fatalf("wait drift update task: %v", err)
	}
	if err := rebuilder.Verify(ctx, tenantID); err == nil || !strings.Contains(err.Error(), "document digest mismatch") {
		t.Fatalf("Verify on same-count drift = %v, want digest mismatch", err)
	} else if strings.Contains(err.Error(), drifted.PropertiesText) {
		t.Fatal("Verify leaked indexed content in a digest mismatch")
	}

	taskUID, err = env.client.DeleteDocuments(ctx, env.indexUID(tenantID), []string{first.ID.String()})
	if err != nil {
		t.Fatalf("delete one document for drift: %v", err)
	}
	if taskUID != "" {
		if err := env.client.WaitTask(ctx, env.indexUID(tenantID), taskUID, 30*time.Second); err != nil {
			t.Fatalf("wait drift delete task: %v", err)
		}
	}
	err = rebuilder.Verify(ctx, tenantID)
	if err == nil {
		t.Fatal("Verify on drifted index returned nil, want mismatch error")
	}
	for _, want := range []string{"document count mismatch", "database=2", "index=1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Verify error = %q, want substring %q", err.Error(), want)
		}
	}
}

// failingClient fails staging upserts, exercising the pre-swap cleanup path.
type failingClient struct {
	*search.Client
	stagingPrefix string
}

func (f *failingClient) UpsertDocuments(ctx context.Context, uid string, docs []search.Document) (string, error) {
	if strings.HasPrefix(uid, f.stagingPrefix) {
		return "", fmt.Errorf("injected staging upsert failure")
	}
	return f.Client.UpsertDocuments(ctx, uid, docs)
}

// TestIntegrationRebuildFailureCleansStaging covers task 5.6: a failed rebuild
// leaves the live index untouched and removes the staging index.
func TestIntegrationRebuildFailureCleansStaging(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	for i := 0; i < 3; i++ {
		env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": fmt.Sprintf("稳定文档-%d", i)})
	}
	if _, err := env.newRebuilder().Rebuild(ctx, tenantID, nil, true); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	before := map[string]struct{}{}
	for _, uid := range env.listIndexUIDs(ctx, t) {
		if strings.HasPrefix(uid, env.indexUID(tenantID)+"_r") {
			before[uid] = struct{}{}
		}
	}

	failing := &failingClient{Client: env.client, stagingPrefix: env.indexUID(tenantID) + "_r"}
	_, err := search.NewRebuilder(env.pool, failing, env.cfg, search.NopRecorder{}, slog.Default(), nil).
		Rebuild(ctx, tenantID, nil, false)
	if err == nil {
		t.Fatal("Rebuild succeeded, want injected staging failure")
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 3 {
		t.Fatalf("live index documents after failed rebuild = %d, want 3", got)
	}
	waitFor(t, 10*time.Second, "failed staging index cleanup", func() bool {
		for _, uid := range env.listIndexUIDs(context.Background(), t) {
			if _, existed := before[uid]; existed {
				continue
			}
			if strings.HasPrefix(uid, env.indexUID(tenantID)+"_r") {
				return false
			}
		}
		return true
	})
}

// TestIntegrationRebuildObjectTypePreservesOtherTypes covers task 5.1: a
// type-scoped rebuild refreshes only that type; the tenant's other documents
// stay in the live index and verify still passes.
func TestIntegrationRebuildObjectTypePreservesOtherTypes(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	typeA, typeB := newObjectID(t), newObjectID(t)
	keepB1 := env.insertObject(t, newObjectID(t), tenantID, typeB, map[string]any{"name": "B-保留-1"})
	env.insertObject(t, newObjectID(t), tenantID, typeB, map[string]any{"name": "B-保留-2"})
	staleA := env.insertObject(t, newObjectID(t), tenantID, typeA, map[string]any{"name": "A-旧-1"})
	env.insertObject(t, newObjectID(t), tenantID, typeA, map[string]any{"name": "A-旧-2"})
	if _, err := env.newRebuilder().Rebuild(ctx, tenantID, nil, true); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())); got != 4 {
		t.Fatalf("initial documents = %d, want 4", got)
	}

	// Remove one type-A document and add a new one, then rebuild only type A.
	if _, err := env.pool.Exec(ctx, `DELETE FROM object_data.objects WHERE id = $1`, staleA.ID); err != nil {
		t.Fatalf("delete stale type-A object: %v", err)
	}
	newA := env.insertObject(t, newObjectID(t), tenantID, typeA, map[string]any{"name": "A-新-1"})
	summary, err := env.newRebuilder().Rebuild(ctx, tenantID, &typeA, true)
	if err != nil {
		t.Fatalf("object-type rebuild: %v", err)
	}
	if summary.Documents != 2 || !summary.Verified {
		t.Fatalf("object-type summary = %+v, want 2 documents and verified", summary)
	}
	ids := env.documentIDs(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String()))
	if len(ids) != 4 {
		t.Fatalf("documents after object-type rebuild = %d, want 4 (type B must survive)", len(ids))
	}
	if !contains(ids, keepB1.ID.String()) || !contains(ids, newA.ID.String()) {
		t.Fatalf("documents after object-type rebuild missing %s or %s: %v", keepB1.ID, newA.ID, ids)
	}
	if contains(ids, staleA.ID.String()) {
		t.Fatalf("stale type-A document %s still indexed", staleA.ID)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String())+fmt.Sprintf(` AND object_type_id = "%s"`, typeA)); got != 2 {
		t.Fatalf("type-A documents = %d, want 2", got)
	}
}

// TestIntegrationTenantIsolation covers tenant isolation and index settings.
func TestIntegrationTenantIsolation(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantA := env.newTenant(t)
	tenantB := env.newTenant(t)
	typeA, typeB := newObjectID(t), newObjectID(t)
	docA := env.insertObject(t, newObjectID(t), tenantA, typeA, map[string]any{"name": "共享关键词-租户A"})
	docB := env.insertObject(t, newObjectID(t), tenantB, typeB, map[string]any{"name": "共享关键词-租户B"})

	rebuilder := env.newRebuilder()
	for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
		if _, err := rebuilder.Rebuild(ctx, tenantID, nil, true); err != nil {
			t.Fatalf("rebuild tenant %s: %v", tenantID, err)
		}
	}
	if env.indexUID(tenantA) == env.indexUID(tenantB) {
		t.Fatalf("tenant index names collide: %q", env.indexUID(tenantA))
	}
	if strings.Contains(env.indexUID(tenantA), tenantA.String()) {
		t.Fatalf("index name leaks the raw tenant uuid: %q", env.indexUID(tenantA))
	}

	resultA, err := env.client.Search(ctx, env.indexUID(tenantA), search.SearchRequest{
		Q: "共享关键词", Filter: search.TenantFilter(tenantA.String()), Limit: 10,
	})
	if err != nil {
		t.Fatalf("search tenant A: %v", err)
	}
	if len(resultA.Hits) != 1 || resultA.Hits[0].ID != docA.ID.String() {
		t.Fatalf("tenant A hits = %+v, want only %s", resultA.Hits, docA.ID)
	}
	resultB, err := env.client.Search(ctx, env.indexUID(tenantB), search.SearchRequest{
		Q: "共享关键词", Filter: search.TenantFilter(tenantB.String()), Limit: 10,
	})
	if err != nil {
		t.Fatalf("search tenant B: %v", err)
	}
	if len(resultB.Hits) != 1 || resultB.Hits[0].ID != docB.ID.String() {
		t.Fatalf("tenant B hits = %+v, want only %s", resultB.Hits, docB.ID)
	}
	crossTenant, err := env.client.Search(ctx, env.indexUID(tenantA), search.SearchRequest{
		Q: "共享关键词", Filter: search.TenantFilter(tenantB.String()), Limit: 10,
	})
	if err != nil {
		t.Fatalf("cross-tenant search: %v", err)
	}
	if crossTenant.EstimatedTotalHits != 0 {
		t.Fatalf("cross-tenant filter returned %d hits, want 0", crossTenant.EstimatedTotalHits)
	}
	// The tenant-B document lives only in tenant B's index.
	if got := env.countDocuments(ctx, t, env.indexUID(tenantA), search.TenantFilter(tenantB.String())); got != 0 {
		t.Fatalf("tenant B filter in tenant A index = %d, want 0", got)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantB), search.TenantFilter(tenantB.String())); got != 1 {
		t.Fatalf("tenant B documents = %d, want 1", got)
	}
	if got := env.countDocuments(ctx, t, env.indexUID(tenantB), search.TenantFilter(tenantA.String())); got != 0 {
		t.Fatalf("tenant A filter in tenant B index = %d, want 0", got)
	}

	settings := env.indexSettings(ctx, t, env.indexUID(tenantA))
	var filterable []string
	if err := json.Unmarshal(settings["filterableAttributes"], &filterable); err != nil {
		t.Fatalf("decode filterableAttributes: %v", err)
	}
	for _, want := range []string{"tenant_id", "object_type_id", "updated_at"} {
		if !contains(filterable, want) {
			t.Fatalf("filterableAttributes = %v, missing %q", filterable, want)
		}
	}
	var displayed []string
	if err := json.Unmarshal(settings["displayedAttributes"], &displayed); err != nil {
		t.Fatalf("decode displayedAttributes: %v", err)
	}
	if contains(displayed, "properties") || contains(displayed, "properties_text") {
		t.Fatalf("displayedAttributes leak property text: %v", displayed)
	}
}

// TestIntegrationRebuildSummaryStable covers task 5.7: two rebuilds of the
// same unchanged tenant produce the same summary and index contents.
func TestIntegrationRebuildSummaryStable(t *testing.T) {
	env := testEnv(t)
	ctx := context.Background()
	tenantID := env.newTenant(t)
	objectTypeID := newObjectID(t)
	for i := 0; i < 25; i++ {
		env.insertObject(t, newObjectID(t), tenantID, objectTypeID, map[string]any{"name": fmt.Sprintf("可复现-%d", i)})
	}
	rebuilder := env.newRebuilder()
	first, err := rebuilder.Rebuild(ctx, tenantID, nil, true)
	if err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	firstIDs := env.documentIDs(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String()))
	second, err := rebuilder.Rebuild(ctx, tenantID, nil, true)
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	secondIDs := env.documentIDs(ctx, t, env.indexUID(tenantID), search.TenantFilter(tenantID.String()))
	if first.Documents != second.Documents || first.Documents != 25 {
		t.Fatalf("summary documents differ: first=%d second=%d", first.Documents, second.Documents)
	}
	if first.IndexUID != second.IndexUID || !first.Verified || !second.Verified || len(first.Digest) != 64 || first.Digest != second.Digest {
		t.Fatalf("summary fields differ: first=%+v second=%+v", first, second)
	}
	if strings.Join(firstIDs, ",") != strings.Join(secondIDs, ",") {
		t.Fatalf("index contents differ between rebuilds: first=%d second=%d ids", len(firstIDs), len(secondIDs))
	}
}
