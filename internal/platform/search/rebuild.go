package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

const rebuildBatchSize = 500

// RebuildClient is the subset of *Client used by Rebuilder. It is an interface
// so rebuild orchestration can be tested without a live Meilisearch.
type RebuildClient interface {
	EnsureIndex(ctx context.Context, uid string, settings json.RawMessage) error
	DeleteIndex(ctx context.Context, uid string) error
	UpsertDocuments(ctx context.Context, uid string, docs []Document) (string, error)
	DeleteDocuments(ctx context.Context, uid string, ids []string) (string, error)
	Search(ctx context.Context, uid string, req SearchRequest) (SearchResult, error)
	SwapIndexes(ctx context.Context, pairs [][2]string) error
	WaitTask(ctx context.Context, uid, taskUID string, timeout time.Duration) error
}

// Summary describes one completed tenant rebuild.
type Summary struct {
	TenantID  uuid.UUID     `json:"tenant_id"`
	IndexUID  string        `json:"index_uid"`
	Documents int64         `json:"documents"`
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration"`
	Verified  bool          `json:"verified"`
}

// Rebuilder rebuilds a tenant index out-of-band: it indexes into a staging
// index, atomically swaps it into place, and replays outbox events that raced
// with the scan.
type Rebuilder struct {
	pool    *pgxpool.Pool
	client  RebuildClient
	config  Config
	metrics Recorder
	logger  *slog.Logger
	tracer  trace.Tracer
}

// NewRebuilder constructs a Rebuilder.
func NewRebuilder(
	pool *pgxpool.Pool,
	client RebuildClient,
	cfg Config,
	metrics Recorder,
	logger *slog.Logger,
	tracer trace.Tracer,
) *Rebuilder {
	if metrics == nil {
		metrics = NopRecorder{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ontoaxis-search")
	}
	return &Rebuilder{
		pool:    pool,
		client:  client,
		config:  cfg,
		metrics: metrics,
		logger:  logger,
		tracer:  tracer,
	}
}

// Rebuild indexes all live objects of a tenant (optionally limited to one
// object type) into a staging index, swaps it with the active index, replays
// the outbox window, and optionally verifies the document count.
func (r *Rebuilder) Rebuild(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID, verify bool) (summary Summary, err error) {
	startedAt := time.Now().UTC()
	summary = Summary{TenantID: tenantID, StartedAt: startedAt}
	if tenantID == uuid.Nil {
		return summary, fmt.Errorf("search rebuild: tenant id is empty")
	}
	if r.pool == nil {
		return summary, fmt.Errorf("search rebuild: nil pool")
	}
	if r.client == nil {
		return summary, fmt.Errorf("search rebuild: nil client")
	}
	version := r.config.IndexVersion
	if version == "" {
		version = defaultSearchIndexVersion
	}
	indexUID := IndexName(version, tenantID.String())
	stagingUID := StagingIndexName(indexUID, startedAt.Unix())
	summary.IndexUID = indexUID

	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		if r.metrics != nil {
			r.metrics.Rebuild(result, time.Since(startedAt), int(summary.Documents))
		}
	}()

	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			r.deleteIndexBestEffort(stagingUID)
		}
	}()

	if err = r.client.EnsureIndex(ctx, stagingUID, SettingsJSON()); err != nil {
		return summary, fmt.Errorf("search rebuild: ensure staging index: %w", err)
	}
	documents, err := r.indexTenantObjects(ctx, tenantID, objectTypeID, indexUID, stagingUID)
	if err != nil {
		return summary, err
	}
	summary.Documents = documents

	// The swap endpoint requires both indexes to exist.
	if err = r.client.EnsureIndex(ctx, indexUID, SettingsJSON()); err != nil {
		return summary, fmt.Errorf("search rebuild: ensure target index: %w", err)
	}
	if err = r.client.SwapIndexes(ctx, [][2]string{{stagingUID, indexUID}}); err != nil {
		return summary, fmt.Errorf("search rebuild: swap indexes: %w", err)
	}
	cleanupStaging = false // staging now holds the previous index generation

	if err = r.replayOutbox(ctx, tenantID, startedAt); err != nil {
		return summary, err
	}
	if err = r.recordIndexState(ctx, tenantID, version, indexUID, startedAt); err != nil {
		return summary, err
	}
	if verify {
		if err = r.verifyTenantCount(ctx, tenantID, objectTypeID, indexUID); err != nil {
			return summary, err
		}
		summary.Verified = true
	}
	summary.Duration = time.Since(startedAt)
	return summary, nil
}

func (r *Rebuilder) indexTenantObjects(
	ctx context.Context,
	tenantID uuid.UUID,
	objectTypeID *uuid.UUID,
	indexUID, stagingUID string,
) (int64, error) {
	var (
		lastID    = uuid.Nil
		documents int64
	)
	for {
		objects, nextID, err := r.scanObjectBatch(ctx, tenantID, objectTypeID, lastID)
		if err != nil {
			return documents, err
		}
		if len(objects) == 0 {
			return documents, nil
		}
		docs := make([]Document, 0, len(objects))
		for _, object := range objects {
			document, err := BuildDocument(object)
			if err != nil {
				return documents, fmt.Errorf("search rebuild: build document %s: %w", object.ID, err)
			}
			docs = append(docs, document)
		}
		taskUID, err := r.client.UpsertDocuments(ctx, stagingUID, docs)
		if err != nil {
			return documents, fmt.Errorf("search rebuild: upsert batch: %w", err)
		}
		if taskUID != "" {
			if err := r.client.WaitTask(ctx, stagingUID, taskUID, rebuildWaitTimeout(r.config)); err != nil {
				return documents, fmt.Errorf("search rebuild: wait upsert task: %w", err)
			}
		}
		documents += int64(len(docs))
		lastID = nextID
		if len(objects) < rebuildBatchSize {
			return documents, nil
		}
	}
}

func (r *Rebuilder) scanObjectBatch(
	ctx context.Context,
	tenantID uuid.UUID,
	objectTypeID *uuid.UUID,
	after uuid.UUID,
) ([]domain.Object, uuid.UUID, error) {
	const columns = `SELECT id::text, tenant_id::text, object_type_id::text, properties, version, created_at, updated_at
FROM object_data.objects
WHERE tenant_id = $1 AND deleted_at IS NULL`
	query := columns + ` AND id > $2 ORDER BY id LIMIT $3`
	args := []any{tenantID, after, rebuildBatchSize}
	if objectTypeID != nil {
		query = columns + ` AND object_type_id = $2 AND id > $3 ORDER BY id LIMIT $4`
		args = []any{tenantID, *objectTypeID, after, rebuildBatchSize}
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("search rebuild: scan objects: %w", err)
	}
	defer rows.Close()

	objects := make([]domain.Object, 0, rebuildBatchSize)
	nextID := after
	for rows.Next() {
		object, err := scanObjectRow(rows)
		if err != nil {
			return nil, uuid.Nil, fmt.Errorf("search rebuild: scan object row: %w", err)
		}
		objects = append(objects, object)
		nextID = object.ID
	}
	if err := rows.Err(); err != nil {
		return nil, uuid.Nil, fmt.Errorf("search rebuild: object rows: %w", err)
	}
	return objects, nextID, nil
}

func (r *Rebuilder) replayOutbox(ctx context.Context, tenantID uuid.UUID, since time.Time) error {
	rows, err := r.pool.Query(ctx, `
SELECT id::text, tenant_id::text, aggregate_type, aggregate_id::text, event_type, payload, created_at
FROM jobs.outbox
WHERE tenant_id = $1 AND created_at >= $2
ORDER BY created_at, id`, tenantID, since)
	if err != nil {
		return fmt.Errorf("search rebuild: query outbox replay: %w", err)
	}
	defer rows.Close()

	consumer := NewConsumer(r.pool, rebuildApplier{client: r.client, wait: rebuildWaitTimeout(r.config)}, dbObjectReader{pool: r.pool}, r.config, r.metrics, r.logger, r.tracer)
	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(&event.ID, &event.TenantID, &event.AggregateType, &event.AggregateID, &event.EventType, &event.Payload, &event.CreatedAt); err != nil {
			return fmt.Errorf("search rebuild: scan outbox replay: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("search rebuild: outbox replay rows: %w", err)
	}
	// Release the connection before applying events; dbObjectReader uses the
	// same pool and a small pool must not deadlock on an open cursor.
	rows.Close()
	for _, event := range events {
		if !knownEventType(event.EventType) {
			continue
		}
		if err := consumer.ApplyEvent(ctx, event); err != nil {
			return fmt.Errorf("search rebuild: replay event %s: %w", event.ID, err)
		}
	}
	return nil
}

func (r *Rebuilder) recordIndexState(ctx context.Context, tenantID uuid.UUID, version, indexUID string, since time.Time) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO search.index_state (tenant_id, index_version, index_uid, last_event_at, last_success_at, rebuilt_at)
VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (tenant_id, index_version) DO UPDATE SET
    index_uid = EXCLUDED.index_uid,
    last_event_at = GREATEST(COALESCE(search.index_state.last_event_at, EXCLUDED.last_event_at), EXCLUDED.last_event_at),
    last_success_at = now(),
    rebuilt_at = now()`, tenantID, version, indexUID, since)
	if err != nil {
		return fmt.Errorf("search rebuild: record index state: %w", err)
	}
	return nil
}

// Verify checks that the active index document count matches the database for
// the tenant. It returns an error on any mismatch or backend failure.
func (r *Rebuilder) Verify(ctx context.Context, tenantID uuid.UUID) error {
	if r == nil {
		return fmt.Errorf("search verify: nil rebuilder")
	}
	if r.pool == nil {
		return fmt.Errorf("search verify: nil pool")
	}
	if r.client == nil {
		return fmt.Errorf("search verify: nil client")
	}
	version := r.config.IndexVersion
	if version == "" {
		version = defaultSearchIndexVersion
	}
	indexUID := IndexName(version, tenantID.String())
	return r.verifyTenantCount(ctx, tenantID, nil, indexUID)
}

func (r *Rebuilder) verifyTenantCount(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID, indexUID string) error {
	dbCount, err := r.countDBObjects(ctx, tenantID, objectTypeID)
	if err != nil {
		return err
	}
	result, err := r.client.Search(ctx, indexUID, SearchRequest{
		Q:      "",
		Filter: TenantFilter(tenantID.String()),
		Limit:  1,
		Offset: 0,
	})
	if err != nil {
		return fmt.Errorf("search verify: query index: %w", err)
	}
	if result.EstimatedTotalHits != dbCount {
		return fmt.Errorf("search verify: tenant %s document count mismatch: database=%d index=%d", tenantID, dbCount, result.EstimatedTotalHits)
	}
	return nil
}

func (r *Rebuilder) countDBObjects(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID) (int64, error) {
	query := `SELECT count(*) FROM object_data.objects WHERE tenant_id = $1 AND deleted_at IS NULL`
	args := []any{tenantID}
	if objectTypeID != nil {
		query += ` AND object_type_id = $2`
		args = append(args, *objectTypeID)
	}
	var count int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("search verify: count database objects: %w", err)
	}
	return count, nil
}

func (r *Rebuilder) deleteIndexBestEffort(indexUID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.client.DeleteIndex(ctx, indexUID); err != nil {
		if r.logger != nil {
			r.logger.Warn("search rebuild: could not delete staging index", "index_uid", indexUID, "error", err)
		}
	}
}

// TenantFilter returns the mandatory tenant filter expression for searches.
func TenantFilter(tenantID string) string {
	return fmt.Sprintf(`tenant_id = "%s"`, tenantID)
}

type rebuildApplier struct {
	client RebuildClient
	wait   time.Duration
}

func (a rebuildApplier) UpsertDocuments(ctx context.Context, indexUID string, docs []Document) error {
	taskUID, err := a.client.UpsertDocuments(ctx, indexUID, docs)
	if err != nil {
		return err
	}
	if taskUID == "" {
		return nil
	}
	return a.client.WaitTask(ctx, indexUID, taskUID, a.wait)
}

func (a rebuildApplier) DeleteDocuments(ctx context.Context, indexUID string, ids []string) error {
	taskUID, err := a.client.DeleteDocuments(ctx, indexUID, ids)
	if err != nil {
		return err
	}
	if taskUID == "" {
		return nil
	}
	return a.client.WaitTask(ctx, indexUID, taskUID, a.wait)
}

// NewPostgresObjectReader returns an ObjectReader backed by object_data.objects.
func NewPostgresObjectReader(pool *pgxpool.Pool) ObjectReader {
	return dbObjectReader{pool: pool}
}

type dbObjectReader struct {
	pool *pgxpool.Pool
}

func (r dbObjectReader) GetObjectForSearch(ctx context.Context, tenantID, objectID string) (domain.Object, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		return domain.Object{}, fmt.Errorf("search object reader: parse tenant id: %w", err)
	}
	oid, err := uuid.Parse(objectID)
	if err != nil {
		return domain.Object{}, fmt.Errorf("search object reader: parse object id: %w", err)
	}
	row := r.pool.QueryRow(ctx, `
SELECT id::text, tenant_id::text, object_type_id::text, properties, version, created_at, updated_at
FROM object_data.objects
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`, tid, oid)
	object, err := scanObjectRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Object{}, fmt.Errorf("search object reader: %w", err)
	}
	return object, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanObjectRow(row rowScanner) (domain.Object, error) {
	var (
		id, tenantID, objectTypeID string
		properties                 []byte
		version                    int64
		createdAt, updatedAt       time.Time
	)
	if err := row.Scan(&id, &tenantID, &objectTypeID, &properties, &version, &createdAt, &updatedAt); err != nil {
		return domain.Object{}, err
	}
	objectID, err := uuid.Parse(id)
	if err != nil {
		return domain.Object{}, fmt.Errorf("parse object id: %w", err)
	}
	tenantUUID, err := uuid.Parse(tenantID)
	if err != nil {
		return domain.Object{}, fmt.Errorf("parse tenant id: %w", err)
	}
	objectTypeUUID, err := uuid.Parse(objectTypeID)
	if err != nil {
		return domain.Object{}, fmt.Errorf("parse object type id: %w", err)
	}
	props := map[string]any{}
	if len(properties) > 0 {
		if err := json.Unmarshal(properties, &props); err != nil {
			return domain.Object{}, fmt.Errorf("decode object properties: %w", err)
		}
	}
	return domain.Object{
		ID:           objectID,
		TenantID:     tenantUUID,
		ObjectTypeID: objectTypeUUID,
		Version:      version,
		Properties:   props,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

func rebuildWaitTimeout(cfg Config) time.Duration {
	timeout := cfg.SyncWindow * 4
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	if timeout > 2*time.Minute {
		timeout = 2 * time.Minute
	}
	return timeout
}
