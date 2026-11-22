#!/usr/bin/env bash
# Smoke-test the Envoy Gateway provider. Requires a reachable Envoy Gateway
# listener (kind port-forward or an in-cluster test run).
set -euo pipefail

EDGE_URL="${EDGE_URL:-http://127.0.0.1:18086}"
KEYCLOAK_TOKEN_URL="${KEYCLOAK_TOKEN_URL:-http://127.0.0.1:18083/realms/ontoaxis/protocol/openid-connect/token}"

pass=0
fail=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

check() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    pass=$((pass + 1)); echo "PASS ${name} (HTTP ${actual})"
  else
    fail=$((fail + 1)); echo "FAIL ${name}: expected ${expected}, got ${actual}"
  fi
}

request() {
  local method="$1" path="$2" token="${3:-}" extra="${4:-}"
  local args=(-sS -o /dev/null -w '%{http_code}' -X "$method" "${EDGE_URL}${path}")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer ${token}")
  [ -n "$extra" ] && args+=(-H "$extra")
  curl "${args[@]}" || echo 000
}

token=$(curl -sS -X POST "$KEYCLOAK_TOKEN_URL" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=password&client_id=ontoaxis-web&username=admin&password=admin123' |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
[ -n "$token" ] || { echo "FAIL cannot obtain Keycloak token"; exit 1; }

check "public /healthz" 200 "$(request GET /healthz)"
check "public /readyz" 200 "$(request GET /readyz)"
check "internal /metrics hidden" 404 "$(request GET /metrics)"
check "unknown route 404" 404 "$(request GET /api/v1/unknown)"
check "missing JWT 401" 401 "$(request GET /api/v1/tenancy/organizations)"
check "bogus JWT 401" 401 "$(request GET /api/v1/tenancy/organizations bogus)"
check "valid JWT 200" 200 "$(request GET /api/v1/tenancy/organizations "$token")"
# Envoy strips client identity headers before forwarding; the request must not
# gain the forged tenant identity, so it continues as the JWT subject (200).
check "forged identity header is not honored" 200 "$(request GET /api/v1/tenancy/organizations "$token" 'x-ontoaxis-tenant-id: 11111111-1111-4111-8111-111111111111')"
check "protected unsupported method 405" 405 "$(request PUT /api/v1/tenancy/organizations "$token")"

seq 1 200 | xargs -P 20 -I{} curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer ${token}" "${EDGE_URL}/api/v1/tenancy/organizations" >"$tmp/ratecodes" || true
if grep -q '^429$' "$tmp/ratecodes"; then
  pass=$((pass + 1)); echo "PASS rate limit returns 429 ($(grep -c '^429$' "$tmp/ratecodes") of 200)"
else
  fail=$((fail + 1)); echo "FAIL rate limit did not return 429"
fi

echo "envoy edge smoke: ${pass} passed, ${fail} failed"
[ "$fail" -eq 0 ]
