#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE_FILE=deploy/compose/compose.yaml
BASE_URL=http://127.0.0.1:${ONTOAXIS_CORE_HOST_PORT:-18080}
METRICS_URL=http://127.0.0.1:${ONTOAXIS_METRICS_HOST_PORT:-19090}

cleanup() {
  docker compose -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose -f "$COMPOSE_FILE" up -d --build

for _ in $(seq 1 60); do
  if curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

curl -fsS "$BASE_URL/healthz" | grep -q '"status":"ok"'
curl -fsS "$BASE_URL/readyz" | grep -q '"status":"ok"'
curl -fsS "$METRICS_URL/metrics" | grep -q 'ontoaxis_http_requests_total'
status=$(curl -s -o /tmp/ontoaxis-smoke-404.json -w '%{http_code}' "$BASE_URL/api/v1/not-registered")
test "$status" = "404"
grep -q '"code":"not_found"' /tmp/ontoaxis-smoke-404.json

echo "PASS: ontoaxis-core foundation smoke (/healthz, /readyz, /metrics, 404)"
