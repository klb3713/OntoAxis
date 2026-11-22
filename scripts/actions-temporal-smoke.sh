#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/deploy/compose/compose.yaml" -f "$ROOT/deploy/compose/authz/compose.authz.yaml" -f "$ROOT/deploy/compose/action-temporal/compose.temporal.yaml")
CORE=http://localhost:18080
KEYCLOAK=http://localhost:18083
TENANT=11111111-1111-4111-8111-111111111111
TOKEN=""
PASS=0
FAIL=0
RESULTS=()

log() { printf '%s\n' "$*" >&2; }
fail() { log "FAIL: $*"; FAIL=$((FAIL+1)); RESULTS+=("{\"name\":\"$1\",\"status\":\"fail\",\"detail\":\"$2\"}"); }
pass() { log "PASS: $*"; PASS=$((PASS+1)); RESULTS+=("{\"name\":\"$1\",\"status\":\"pass\"}"); }

api() {
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$CORE$path" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -X "$method" "$CORE$path" -H "Authorization: Bearer $TOKEN"
  fi
}

json() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

wait_status() {
  local id="$1" want="$2" tries="${3:-60}"
  for _ in $(seq 1 "$tries"); do
    local s; s=$(api GET "/api/v1/actions/executions/$id" | json 'd["status"]')
    case "$s" in "$want") return 0;; failed|succeeded|cancelled|timed_out|compensation_failed) return 1;; esac
    sleep 1
  done
  return 1
}

wait_any_terminal() {
  local id="$1" tries="${2:-60}"
  for _ in $(seq 1 "$tries"); do
    local s; s=$(api GET "/api/v1/actions/executions/$id" | json 'd["status"]')
    case "$s" in failed|succeeded|cancelled|timed_out|compensation_failed) echo "$s"; return 0;; esac
    sleep 1
  done
  echo "timeout"; return 1
}

make_type() {
  local name="$1" definition="$2" object_type="${3:-}"
  local payload
  payload=$(python3 - "$name" "$definition" "$object_type" <<'PY'
import json,sys
name,definition,object_type=sys.argv[1],json.loads(sys.argv[2]),sys.argv[3]
body={"name":name,"display_name":name,"parameters":{"execution_definition":definition}}
if object_type: body["object_type_id"]=object_type
print(json.dumps(body))
PY
)
  api POST /api/v1/actions/types "$payload" | json 'd["id"]'
}

run_action() { api POST "/api/v1/actions/types/$1/run" "$2" | json 'd["id"]'; }

if [ "${SKIP_START:-0}" != "1" ]; then
  log "starting action-temporal stack..."
  "${COMPOSE[@]}" up -d --build >/tmp/actions-smoke-compose.log 2>&1
  for _ in $(seq 1 90); do
    if curl -sf "$CORE/readyz" >/dev/null 2>&1; then break; fi
    sleep 1
  done
fi

TOKEN=$(curl -sS -X POST "$KEYCLOAK/realms/ontoaxis/protocol/openid-connect/token" -H 'Content-Type: application/x-www-form-urlencoded' -d 'grant_type=password&client_id=ontoaxis-web&username=admin&password=admin123' | json 'd["access_token"]')

RUN_ID=$(date +%s)
OBJ_TYPE_NAME="smoke_object_$RUN_ID"
OT=$(api POST /api/v1/ontology/object-types "{\"name\":\"$OBJ_TYPE_NAME\",\"display_name\":\"Smoke Object\",\"primary_key_property\":\"name\"}" | json 'd["id"]')
OBJ=$(api POST /api/v1/objects "{\"object_type_id\":\"$OT\",\"properties\":{\"name\":\"before\"}}" | json 'd["id"]')

# 1. object.update success
AT=$(make_type "smoke_update_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"object.update","parameters":{"properties":{"name":"after"}},"compensation":{"activity_type":"object.update","parameters":{"properties":{"name":"before"}}}}]}' "$OT")
EID=$(run_action "$AT" "{\"parameters\":{\"object_id\":\"$OBJ\"}}")
if wait_status "$EID" succeeded 30; then
  NAME=$(api GET "/api/v1/objects/$OBJ" | json 'd["properties"]["name"]')
  [ "$NAME" = "after" ] && pass "object_update" || fail "object_update" "object name=$NAME"
else fail "object_update" "execution did not succeed"; fi

# 2. compensation: update then permanent failure
AT2=$(make_type "smoke_comp_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"object.update","parameters":{"properties":{"name":"after"}},"compensation":{"activity_type":"object.update","parameters":{"properties":{"name":"before"}}}},{"id":"s2","activity_type":"test.permanent_failure","read_only":true}]}' "$OT")
OBJ2=$(api POST /api/v1/objects "{\"object_type_id\":\"$OT\",\"properties\":{\"name\":\"before\"}}" | json 'd["id"]')
EID2=$(run_action "$AT2" "{\"parameters\":{\"object_id\":\"$OBJ2\"}}")
S2=$(wait_any_terminal "$EID2" 40 || true)
NAME2=$(api GET "/api/v1/objects/$OBJ2" | json 'd["properties"]["name"]')
if { [ "$S2" = "compensation_failed" ] || [ "$S2" = "failed" ]; } && [ "$NAME2" = "before" ]; then pass "compensation"; else fail "compensation" "status=$S2 object=$NAME2"; fi

# 3. approval approve
AT3=$(make_type "smoke_approve_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"noop","read_only":true,"requires_approval":true}]}')
EID3=$(run_action "$AT3" '{}')
if wait_status "$EID3" waiting_approval 30; then
  TASK=$(api GET "/api/v1/actions/approvals?status=pending" | python3 -c "import sys,json; d=json.load(sys.stdin); print(next(x['id'] for x in d['items'] if x['execution_id']=='$EID3'))")
  curl -sS -X POST "$CORE/api/v1/actions/approvals/$TASK/approve" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: smoke-approve-$EID3" -d '{"comment":"smoke approve"}' >/dev/null
  wait_status "$EID3" succeeded 30 && pass "approval_approve" || fail "approval_approve" "execution did not succeed"
else fail "approval_approve" "did not reach waiting_approval"; fi

# 4. approval reject
AT4=$(make_type "smoke_reject_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"noop","read_only":true,"requires_approval":true}]}')
EID4=$(run_action "$AT4" '{}')
if wait_status "$EID4" waiting_approval 30; then
  TASK=$(api GET "/api/v1/actions/approvals?status=pending" | python3 -c "import sys,json; d=json.load(sys.stdin); print(next(x['id'] for x in d['items'] if x['execution_id']=='$EID4'))")
  curl -sS -X POST "$CORE/api/v1/actions/approvals/$TASK/reject" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: smoke-reject-$EID4" -d '{"comment":"smoke reject"}' >/dev/null
  S4=$(wait_any_terminal "$EID4" 40 || true)
  [ "$S4" = "cancelled" ] && pass "approval_reject" || fail "approval_reject" "status=$S4"
else fail "approval_reject" "did not reach waiting_approval"; fi

# 5. approval timeout (2s)
AT5=$(make_type "smoke_timeout_$RUN_ID" '{"approval_timeout":2000000000,"steps":[{"id":"s1","activity_type":"noop","read_only":true,"requires_approval":true}]}')
EID5=$(run_action "$AT5" '{}')
S5=$(wait_any_terminal "$EID5" 40 || true)
[ "$S5" = "timed_out" ] && pass "approval_timeout" || fail "approval_timeout" "status=$S5"

# 6. retry exhaustion
AT6=$(make_type "smoke_retry_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"test.transient_failure","read_only":true}]}')
EID6=$(run_action "$AT6" '{}')
S6=$(wait_any_terminal "$EID6" 60 || true)
ATT=$("${COMPOSE[@]}" exec -T postgres psql -U ontoaxis -d ontoaxis -tAc "SELECT COALESCE(max(attempt),0) FROM action.execution_steps WHERE execution_id='$EID6'" 2>/dev/null | tr -d '[:space:]')
if [ "$S6" = "failed" ] && [ "${ATT:-0}" -ge 3 ]; then pass "retry_exhaustion"; else fail "retry_exhaustion" "status=$S6 step_attempt=$ATT"; fi

# 7. compensation failure -> compensation_failed
AT7=$(make_type "smoke_compfail_$RUN_ID" '{"steps":[{"id":"s1","activity_type":"object.update","parameters":{"properties":{"name":"after"}},"compensation":{"activity_type":"test.permanent_failure"}},{"id":"s2","activity_type":"test.permanent_failure","read_only":true}]}' "$OT")
OBJ3=$(api POST /api/v1/objects "{\"object_type_id\":\"$OT\",\"properties\":{\"name\":\"before\"}}" | json 'd["id"]')
EID7=$(run_action "$AT7" "{\"parameters\":{\"object_id\":\"$OBJ3\"}}")
S7=$(wait_any_terminal "$EID7" 60 || true)
[ "$S7" = "compensation_failed" ] && pass "compensation_failed" || fail "compensation_failed" "status=$S7"

cat > /tmp/actions-smoke-results.json <<JSON
{"pass":$PASS,"fail":$FAIL,"results":[$(IFS=,; echo "${RESULTS[*]}")]}
JSON
cat /tmp/actions-smoke-results.json
if [ "${STOP_STACK:-1}" = "1" ]; then "${COMPOSE[@]}" down >/dev/null 2>&1 || true; fi
[ "$FAIL" -eq 0 ]
