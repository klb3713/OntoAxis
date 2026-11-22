package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/klb3713/ontoaxis/internal/modules/objects/domain"
)

// Object event types consumed from jobs.outbox.
const (
	EventObjectCreated = "objects.object.created.v1"
	EventObjectUpdated = "objects.object.updated.v1"
	EventObjectDeleted = "objects.object.deleted.v1"
)

const (
	maxAttemptBackoff = 5 * time.Minute
	maxLastErrorBytes = 1024
)

// ErrObjectNotFound can be returned by an ObjectReader when the object does
// not exist or has been soft-deleted. domain.ErrNotFound is treated the same.
var ErrObjectNotFound = errors.New("search: object not found")

// OutboxEvent is one jobs.outbox row relevant to the search index.
type OutboxEvent struct {
	ID            string
	TenantID      string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
	CreatedAt     time.Time
}

// ObjectReader reads the current object state for a read-through event. It
// must return domain.ErrNotFound (or ErrObjectNotFound) for missing objects.
type ObjectReader interface {
	GetObjectForSearch(ctx context.Context, tenantID, objectID string) (domain.Object, error)
}

// Applier applies document mutations to an index. ClientApplier adapts
// *Client to this interface and adds task waiting when desired.
type Applier interface {
	UpsertDocuments(ctx context.Context, indexUID string, docs []Document) error
	DeleteDocuments(ctx context.Context, indexUID string, ids []string) error
}

// ClientApplier adapts a *Client to Applier and optionally waits for the
// Meilisearch task to finish before reporting success.
type ClientApplier struct {
	Client      *Client
	WaitTimeout time.Duration
}

// UpsertDocuments implements Applier.
func (a ClientApplier) UpsertDocuments(ctx context.Context, indexUID string, docs []Document) error {
	if a.Client == nil {
		return Errorf(ErrUnavailable, "applier", "search client is not configured")
	}
	taskUID, err := a.Client.UpsertDocuments(ctx, indexUID, docs)
	if err != nil {
		return err
	}
	if taskUID == "" {
		return nil
	}
	return a.Client.WaitTask(ctx, indexUID, taskUID, a.WaitTimeout)
}

// DeleteDocuments implements Applier.
func (a ClientApplier) DeleteDocuments(ctx context.Context, indexUID string, ids []string) error {
	if a.Client == nil {
		return Errorf(ErrUnavailable, "applier", "search client is not configured")
	}
	taskUID, err := a.Client.DeleteDocuments(ctx, indexUID, ids)
	if err != nil {
		return err
	}
	if taskUID == "" {
		return nil
	}
	return a.Client.WaitTask(ctx, indexUID, taskUID, a.WaitTimeout)
}

// EnsureIndex creates the index and applies settings, waiting for completion.
func (a ClientApplier) EnsureIndex(ctx context.Context, indexUID string, settings json.RawMessage) error {
	if a.Client == nil {
		return Errorf(ErrUnavailable, "applier", "search client is not configured")
	}
	return a.Client.EnsureIndex(ctx, indexUID, settings)
}

// Recorder is the metrics port used by the consumer and rebuilder.
type Recorder interface {
	Event(eventType, result string)
	Lag(seconds float64)
	Batch(result string, d time.Duration)
	DeadLetter(eventType string)
	Rebuild(result string, d time.Duration, docs int)
}

// NopRecorder is a Recorder that discards all observations.
type NopRecorder struct{}

func (NopRecorder) Event(string, string)        {}
func (NopRecorder) Lag(float64)                 {}
func (NopRecorder) Batch(string, time.Duration) {}
func (NopRecorder) DeadLetter(string)           {}
func (NopRecorder) Rebuild(string, time.Duration, int) {
}

// Consumer drains jobs.outbox and keeps Meilisearch in sync.
type Consumer struct {
	pool    *pgxpool.Pool
	client  Applier
	reader  ObjectReader
	config  Config
	metrics Recorder
	logger  *slog.Logger
	tracer  trace.Tracer
}

// NewConsumer constructs a Consumer. Nil logger, metrics, and tracer fall back
// to safe defaults.
func NewConsumer(
	pool *pgxpool.Pool,
	client Applier,
	reader ObjectReader,
	cfg Config,
	metrics Recorder,
	logger *slog.Logger,
	tracer trace.Tracer,
) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = NopRecorder{}
	}
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("ontoaxis-search")
	}
	return &Consumer{
		pool:    pool,
		client:  client,
		reader:  reader,
		config:  cfg,
		metrics: metrics,
		logger:  logger,
		tracer:  tracer,
	}
}

func knownEventType(eventType string) bool {
	switch eventType {
	case EventObjectCreated, EventObjectUpdated, EventObjectDeleted:
		return true
	default:
		return false
	}
}

// RunOnce claims and applies at most ConsumerBatch events. Per-event apply
// failures are recorded in search.outbox_attempt and do not abort the batch;
// only transaction/database failures are returned.
func (c *Consumer) RunOnce(ctx context.Context) (processed int, err error) {
	start := time.Now()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		c.metrics.Batch(result, time.Since(start))
	}()
	if c.pool == nil {
		return 0, fmt.Errorf("search consumer: nil pool")
	}
	if c.client == nil {
		return 0, fmt.Errorf("search consumer: nil client")
	}
	batch := c.config.ConsumerBatch
	if batch <= 0 {
		batch = defaultConsumerBatch
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("search consumer: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	events, err := Claim(ctx, tx, batch)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		processed++
		c.metrics.Lag(time.Since(event.CreatedAt).Seconds())
		if err := c.processEvent(ctx, tx, event); err != nil {
			return processed, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return processed, fmt.Errorf("search consumer: commit: %w", err)
	}
	return processed, nil
}

// Claim selects up to limit pending, non-dead, due outbox rows inside tx and
// locks them with FOR UPDATE SKIP LOCKED. It is exported so tests and embedding
// callers can exercise claim semantics without starting the worker loop.
func Claim(ctx context.Context, tx pgx.Tx, limit int) ([]OutboxEvent, error) {
	return claimEvents(ctx, tx, limit)
}

// claimEvents selects pending, non-dead, due outbox rows. The caller must hold
// a transaction; FOR UPDATE SKIP LOCKED lets multiple consumers run safely.
func claimEvents(ctx context.Context, tx pgx.Tx, limit int) ([]OutboxEvent, error) {
	const query = `
SELECT o.id::text, o.tenant_id::text, o.aggregate_type, o.aggregate_id::text, o.event_type, o.payload, o.created_at
FROM jobs.outbox AS o
WHERE o.published_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM search.outbox_attempt AS a
      WHERE a.event_id = o.id AND a.dead_at IS NOT NULL
  )
  AND NOT EXISTS (
      SELECT 1 FROM search.outbox_attempt AS a
      WHERE a.event_id = o.id
        AND a.next_attempt_at IS NOT NULL
        AND a.next_attempt_at > now()
  )
ORDER BY o.created_at, o.id
LIMIT $1
FOR UPDATE OF o SKIP LOCKED`
	rows, err := tx.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search consumer: claim: %w", err)
	}
	defer rows.Close()
	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.AggregateType,
			&event.AggregateID,
			&event.EventType,
			&event.Payload,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("search consumer: scan: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search consumer: rows: %w", err)
	}
	return events, nil
}

func (c *Consumer) processEvent(ctx context.Context, tx pgx.Tx, event OutboxEvent) error {
	if !knownEventType(event.EventType) {
		if _, err := tx.Exec(ctx, `UPDATE jobs.outbox SET published_at = now() WHERE id = $1::uuid`, event.ID); err != nil {
			return fmt.Errorf("search consumer: mark unknown event published: %w", err)
		}
		c.metrics.Event(event.EventType, "skipped")
		c.logger.DebugContext(ctx, "search consumer skipped unknown event", "event_id", event.ID, "event_type", event.EventType)
		return nil
	}
	if err := c.ApplyEvent(ctx, event); err != nil {
		return c.recordFailure(ctx, tx, event, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs.outbox SET published_at = now() WHERE id = $1::uuid`, event.ID); err != nil {
		return fmt.Errorf("search consumer: mark published: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM search.outbox_attempt WHERE event_id = $1::uuid`, event.ID); err != nil {
		return fmt.Errorf("search consumer: clear attempt: %w", err)
	}
	c.metrics.Event(event.EventType, "success")
	return nil
}

// ApplyEvent reads through to the current object state and applies the
// corresponding index mutation. It does not touch outbox bookkeeping, which
// makes it reusable from rebuild replay.
func (c *Consumer) ApplyEvent(ctx context.Context, event OutboxEvent) error {
	if !knownEventType(event.EventType) {
		return nil
	}
	if c.client == nil {
		return Errorf(ErrUnavailable, "apply_event", "search client is not configured")
	}
	if c.reader == nil {
		return fmt.Errorf("search consumer: nil object reader")
	}
	ctx, span := c.tracer.Start(ctx, "search.apply_event")
	defer span.End()

	version := c.config.IndexVersion
	if version == "" {
		version = defaultSearchIndexVersion
	}
	indexUID := IndexName(version, event.TenantID)
	if ensurer, ok := c.client.(interface {
		EnsureIndex(ctx context.Context, indexUID string, settings json.RawMessage) error
	}); ok {
		if err := ensurer.EnsureIndex(ctx, indexUID, SettingsJSON()); err != nil {
			return err
		}
	}
	object, err := c.reader.GetObjectForSearch(ctx, event.TenantID, event.AggregateID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, ErrObjectNotFound) {
			return c.client.DeleteDocuments(ctx, indexUID, []string{event.AggregateID})
		}
		return fmt.Errorf("search consumer: read object: %w", err)
	}
	document, err := BuildDocument(object)
	if err != nil {
		return fmt.Errorf("search consumer: build document: %w", err)
	}
	return c.client.UpsertDocuments(ctx, indexUID, []Document{document})
}

func (c *Consumer) recordFailure(ctx context.Context, tx pgx.Tx, event OutboxEvent, applyErr error) error {
	maxAttempts := c.config.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultConsumerMaxAttempts
	}
	lastError := applyErr.Error()
	if len(lastError) > maxLastErrorBytes {
		lastError = lastError[:maxLastErrorBytes]
	}
	var attempts int
	err := tx.QueryRow(ctx, `
INSERT INTO search.outbox_attempt (event_id, attempts, next_attempt_at, last_error, updated_at)
VALUES ($1::uuid, 1, now() + interval '1 second', $2, now())
ON CONFLICT (event_id) DO UPDATE SET
    attempts = search.outbox_attempt.attempts + 1,
    last_error = EXCLUDED.last_error,
    updated_at = now()
RETURNING attempts`, event.ID, lastError).Scan(&attempts)
	if err != nil {
		return fmt.Errorf("search consumer: record attempt: %w", err)
	}

	if attempts >= maxAttempts {
		if _, err := tx.Exec(ctx, `UPDATE search.outbox_attempt SET dead_at = now(), next_attempt_at = NULL WHERE event_id = $1::uuid`, event.ID); err != nil {
			return fmt.Errorf("search consumer: mark dead: %w", err)
		}
		// Dead events must leave the claim loop; the attempt row retains the
		// failure history and ReplayDead can revive them.
		if _, err := tx.Exec(ctx, `UPDATE jobs.outbox SET published_at = now() WHERE id = $1::uuid`, event.ID); err != nil {
			return fmt.Errorf("search consumer: mark dead event published: %w", err)
		}
		c.metrics.Event(event.EventType, "dead")
		c.metrics.DeadLetter(event.EventType)
		c.logger.WarnContext(ctx, "search consumer dead-lettered event",
			"event_id", event.ID, "event_type", event.EventType, "attempts", attempts)
		return nil
	}

	backoff := backoffForAttempt(attempts)
	if _, err := tx.Exec(ctx, `UPDATE search.outbox_attempt SET next_attempt_at = now() + ($2 * interval '1 second') WHERE event_id = $1::uuid`, event.ID, backoff.Seconds()); err != nil {
		return fmt.Errorf("search consumer: schedule retry: %w", err)
	}
	c.metrics.Event(event.EventType, "retry")
	c.logger.WarnContext(ctx, "search consumer deferred event",
		"event_id", event.ID, "event_type", event.EventType, "attempts", attempts, "next_attempt_in", backoff.String())
	return nil
}

func backoffForAttempt(attempts int) time.Duration {
	if attempts <= 1 {
		return time.Second
	}
	shift := attempts - 1
	if shift > 20 {
		shift = 20
	}
	backoff := time.Second * time.Duration(int64(1)<<uint(shift))
	if backoff > maxAttemptBackoff || backoff <= 0 {
		backoff = maxAttemptBackoff
	}
	return backoff
}

// ReplayDead clears dead-letter state for up to limit events and returns their
// jobs.outbox rows to the pending queue.
func (c *Consumer) ReplayDead(ctx context.Context, limit int) (int, error) {
	if c.pool == nil {
		return 0, fmt.Errorf("search consumer: nil pool")
	}
	if limit <= 0 {
		limit = 100
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("search replay: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
SELECT a.event_id::text
FROM search.outbox_attempt AS a
WHERE a.dead_at IS NOT NULL
ORDER BY a.dead_at
LIMIT $1
FOR UPDATE OF a SKIP LOCKED`, limit)
	if err != nil {
		return 0, fmt.Errorf("search replay: select: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("search replay: scan: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("search replay: rows: %w", err)
	}
	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("search replay: commit: %w", err)
		}
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE search.outbox_attempt
SET dead_at = NULL, attempts = 0, next_attempt_at = NULL, last_error = NULL, updated_at = now()
WHERE event_id::text = ANY($1::text[])`, ids); err != nil {
		return 0, fmt.Errorf("search replay: clear attempts: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs.outbox SET published_at = NULL WHERE id::text = ANY($1::text[])`, ids); err != nil {
		return 0, fmt.Errorf("search replay: requeue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("search replay: commit: %w", err)
	}
	return len(ids), nil
}
