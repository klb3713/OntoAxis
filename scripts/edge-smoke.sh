#!/usr/bin/env bash
# Smoke-test the oa-edge Traefik provider. Requires the `edge` Compose profile.
set -euo pipefail

EDGE_URL="${EDGE_URL:-http://127.0.0.1:18085}"
KEYCLOAK_TOKEN_URL="${KEYCLOAK_TOKEN_URL:-http://127.0.0.1:18083/realms/ontoaxis/protocol/openid-connect/token}"
COMPOSE_FILE="${COMPOSE_FILE:-deploy/compose/compose.yaml}"
EDGE_CORE_SERVICE="${EDGE_CORE_SERVICE:-ontoaxis-core-edge}"

pass=0
fail=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

check() {
  local name="$1" expected="$2" actual="$3" body_file="$4"
  if [ "$expected" = "$actual" ]; then
    pass=$((pass + 1)); echo "PASS ${name} (HTTP ${actual})"
  else
    fail=$((fail + 1)); echo "FAIL ${name}: expected ${expected}, got ${actual}"; sed -n '1,3p' "$body_file" || true
  fi
}

request() {
  local method="$1" path="$2" out="$3" token="${4:-}" extra="${5:-}"
  local args=(-sS -o "$out" -w '%{http_code}' -X "$method" "${EDGE_URL}${path}")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer ${token}")
  [ -n "$extra" ] && args+=(-H "$extra")
  curl "${args[@]}"
}

token=$(curl -sS -X POST "$KEYCLOAK_TOKEN_URL" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=password&client_id=ontoaxis-web&username=admin&password=admin123' |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
[ -n "$token" ] || { echo "FAIL cannot obtain Keycloak token"; exit 1; }

code=$(request GET /healthz "$tmp/health" ""); check "public /healthz" 200 "$code" "$tmp/health"
code=$(request GET /readyz "$tmp/ready" ""); check "public /readyz" 200 "$code" "$tmp/ready"
code=$(request GET /metrics "$tmp/metrics" ""); check "internal /metrics hidden" 404 "$code" "$tmp/metrics"
code=$(request GET /api/v1/unknown "$tmp/unknown" ""); check "unknown route 404" 404 "$code" "$tmp/unknown"
code=$(request GET /api/v1/tenancy/organizations "$tmp/noauth" ""); check "missing JWT 401" 401 "$code" "$tmp/noauth"
code=$(request GET /api/v1/tenancy/organizations "$tmp/bogus" "bogus"); check "bogus JWT 401" 401 "$code" "$tmp/bogus"
code=$(request GET /api/v1/tenancy/organizations "$tmp/auth" "$token"); check "valid JWT 200" 200 "$code" "$tmp/auth"
code=$(request GET /api/v1/tenancy/organizations "$tmp/forged" "" 'x-ontoaxis-tenant-id: 11111111-1111-4111-8111-111111111111'); check "forged identity header 403" 403 "$code" "$tmp/forged"
code=$(request POST /healthz "$tmp/posthealth" ""); check "public unsupported method 405" 405 "$code" "$tmp/posthealth"
code=$(request PUT /api/v1/tenancy/organizations "$tmp/puttenancy" "$token"); check "protected unsupported method 405" 405 "$code" "$tmp/puttenancy"

# Burst beyond 100 + 50/s; expect at least one 429.
seq 1 200 | xargs -P 20 -I{} curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer ${token}" "${EDGE_URL}/api/v1/tenancy/organizations" >"$tmp/ratecodes" || true
if grep -q '^429$' "$tmp/ratecodes"; then
  pass=$((pass + 1)); echo "PASS rate limit returns 429 ($(grep -c '^429$' "$tmp/ratecodes") of 200)"
else
  fail=$((fail + 1)); echo "FAIL rate limit did not return 429"
fi

if [ "${EDGE_TEST_FAIL_CLOSED:-0}" = "1" ]; then
  docker compose -f "$COMPOSE_FILE" --profile edge stop "$EDGE_CORE_SERVICE" >/dev/null
  code=$(request GET /api/v1/tenancy/organizations "$tmp/down" "$token" || true)
  check "auth backend down fail-closed 5xx" 500 "$code" "$tmp/down"
  docker compose -f "$COMPOSE_FILE" --profile edge start "$EDGE_CORE_SERVICE" >/dev/null
fi

echo "edge smoke: ${pass} passed, ${fail} failed"
[ "$fail" -eq 0 ]
