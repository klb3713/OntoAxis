# Object Search Platform

`internal/platform/search` is the Meilisearch-backed object index for OntoAxis.
It derives one index per tenant, builds the document projection from
`object_data.objects`, consumes object events from `jobs.outbox`, and provides
full-rebuild and dead-letter replay operations.

The package deliberately uses only `net/http` for the Meilisearch API; it adds
no SDK or new module dependencies. `internal/platform/search/worker` hosts the
long-running process and Prometheus metrics, while `cmd/ontoaxis-search` is a
thin command-line entry point.

## Configuration

All search settings come from `ONTOAXIS_SEARCH_*` variables. The worker also
requires `ONTOAXIS_DATABASE_URL`.

| Variable | Default | Validation |
| --- | --- | --- |
| `ONTOAXIS_SEARCH_ENABLED` | `false` | Boolean |
| `ONTOAXIS_SEARCH_URL` | empty | Required when enabled; `http`/`https` URL with host |
| `ONTOAXIS_SEARCH_API_KEY` | empty | Required when enabled; from environment/Secret only |
| `ONTOAXIS_SEARCH_TIMEOUT` | `1500ms` | `> 0` and `<= 10s` |
| `ONTOAXIS_SEARCH_INDEX_VERSION` | `v1` | Matches `^v[0-9]+$` |
| `ONTOAXIS_SEARCH_SYNC_WINDOW` | `5s` | `1s`–`60s` |
| `ONTOAXIS_SEARCH_POLL_INTERVAL` | `500ms` | `100ms`–`10s` |
| `ONTOAXIS_SEARCH_CONSUMER_BATCH` | `50` | `1`–`500` |
| `ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS` | `5` | `1`–`20` |
| `ONTOAXIS_SEARCH_HTTP_ADDR` | `:8081` | Worker health/metrics address |
| `ONTOAXIS_SEARCH_METRICS_ADDR` | `:9094` | Optional separate metrics address |

A disabled configuration validates with defaults and never requires a URL or API
key. The API key is never logged or included in errors.

## Database migration

The worker owns schema `search` and runs its embedded migration at startup for
every mode. It is idempotent:

```sql
CREATE SCHEMA IF NOT EXISTS search;
CREATE TABLE IF NOT EXISTS search.outbox_attempt (...);
CREATE TABLE IF NOT EXISTS search.index_state (...);
```

Run it explicitly when preparing a database:

```bash
ONTOAXIS_DATABASE_URL='postgres://...' /ontoaxis-search migrate
```

## Commands

```bash
# Default mode: consume jobs.outbox until SIGINT/SIGTERM.
ONTOAXIS_DATABASE_URL='postgres://...' \
ONTOAXIS_SEARCH_ENABLED=true \
ONTOAXIS_SEARCH_URL='http://meilisearch:7700' \
ONTOAXIS_SEARCH_API_KEY='...' \
/ontoaxis-search consume

# Rebuild one tenant index into a staging index, atomically swap, replay events,
# and optionally verify the document count.
/ontoaxis-search rebuild --tenant 00000000-0000-0000-0000-000000000001 --verify
/ontoaxis-search rebuild --tenant 00000000-0000-0000-0000-000000000001 \
  --object-type 00000000-0000-0000-0000-000000000002

# Requeue dead-letter events (bounded by --limit, default 100).
/ontoaxis-search replay --limit 100
```

All modes support `SIGINT`/`SIGTERM` graceful shutdown. `consume` serves:

- `GET /healthz` — process liveness.
- `GET /readyz` — PostgreSQL `Ping` plus Meilisearch `/health`; returns `503`
  with the unavailable dependency when either fails.
- `GET /metrics` — the worker-owned Prometheus registry (also served on the
  separate metrics address when configured).

## Index invariants

- Index name: `objects_<version>_<hex16(sha256(tenant_uuid))>`. Raw tenant UUIDs
  never appear in index names.
- Every query includes a `tenant_id = "<uuid>"` filter in addition to the
  per-tenant index boundary.
- Documents contain only `id`, `tenant_id`, `object_type_id`, `display_name`,
  `properties_text`, `version`, `created_at`, `updated_at`; raw property JSON is
  never indexed.
- `properties_text` is a deterministic, sorted flatten of scalar properties
  (depth at most two), excludes sensitive key subtrees, and is truncated on a
  UTF-8 boundary.
- Event application is read-through: the consumer reads the current database
  row, so duplicate, stale, and out-of-order events converge. A missing or
  soft-deleted object deletes its index document.
- Apply failures are retried with exponential backoff (`1s * 2^n`, capped at
  `5m`). Once `CONSUMER_MAX_ATTEMPTS` is reached the event leaves the claim loop
  and is recorded in `search.outbox_attempt` as dead; `replay` revives it.
- Rebuilds index into `<index>_r<unix_timestamp>`, wait for each batch task
  (500 rows), then swap atomically. If anything fails before the swap, the
  staging index is deleted and the live index is untouched.

## Metrics

The worker registry exposes:

- `ontoaxis_search_index_events_total{event_type,result}`
- `ontoaxis_search_index_lag_seconds` (histogram)
- `ontoaxis_search_index_batch_duration_seconds{result}` (histogram)
- `ontoaxis_search_dead_letters_total{event_type}`
- `ontoaxis_search_rebuild_documents_total{result}`
- `ontoaxis_search_rebuild_duration_seconds{result}` (histogram)

## Tests

```bash
/tmp/go/bin/go test ./internal/platform/search/... ./cmd/ontoaxis-search/
/tmp/go/bin/go test -race ./internal/platform/search/... ./cmd/ontoaxis-search/
/tmp/go/bin/go vet ./internal/platform/search/... ./cmd/ontoaxis-search/
```

Unit tests use `httptest.Server` and fake appliers/readers; no PostgreSQL or
Meilisearch instance is required. Integration tests that need real services
should be gated behind `ONTOAXIS_SEARCH_TEST_*` variables and skip otherwise.
