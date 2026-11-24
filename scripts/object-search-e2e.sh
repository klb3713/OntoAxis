#!/usr/bin/env bash
# Local-only object-search E2E; starts only a new, labelled Compose project.
# Run: bash scripts/object-search-e2e.sh
# Optional: E2E_PROJECT (must not exist), KEEP_E2E_STACK=1,
# E2E_KEYCLOAK_PORT, E2E_CORE_PORT, E2E_MEILI_PORT, E2E_WORKER_PORT,
# E2E_START_TIMEOUT (seconds, default 240).
# Unset/0 ports are chosen from available loopback TCP ports.
set -euo pipefail
umask 077

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SUPPORT="${ROOT_DIR}/scripts/object-search-e2e-support.py"
OWNER="$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
PROJECT="${E2E_PROJECT:-ontoaxis-search-e2e-${OWNER}}"
if [[ ! "${PROJECT}" =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]]; then
  echo 'FAIL invalid E2E_PROJECT (use 1–63 lowercase letters, digits, underscores or hyphens)' >&2
  exit 2
fi
# Decline old external endpoints, arbitrary Compose files, and profile overrides
# instead of silently sending test traffic to a caller-provided environment.
if [[ -n "${E2E_CORE_URL:-}${E2E_MEILI_URL:-}${E2E_KEYCLOAK_TOKEN_URL:-}${E2E_MEILI_KEY:-}${E2E_TENANT_B:-}${E2E_PROFILE:-}${COMPOSE_BASE:-}${COMPOSE_SEARCH:-}" ]]; then
  echo 'FAIL endpoint, identity and Compose overrides are not supported; set E2E_*_PORT instead' >&2
  exit 2
fi
TENANT_A=11111111-1111-4111-8111-111111111111
TENANT_B=22222222-2222-4222-8222-222222222222
AUTHZ_LOCAL="${ROOT_DIR}/deploy/compose/authz/compose.authz.yaml"
COMPOSE=(env -i "HOME=${HOME}" "PATH=${PATH}" docker compose --env-file /dev/null -p "${PROJECT}" --profile search
  -f "${ROOT_DIR}/deploy/compose/compose.yaml"
  -f "${ROOT_DIR}/deploy/compose/search/compose.search.yaml" -f "${AUTHZ_LOCAL}")
DOCKER=(env -i "HOME=${HOME}" "PATH=${PATH}" docker)
pass=0
fail=0
startup_attempted=0
paused_openfga=''
stopped_meili=''
project_lock="/tmp/ontoaxis-search-e2e-${UID}-${PROJECT}.lock"
lock_owned=0
tmp="$(mktemp -d)"

cleanup() {
  local code=$?
  trap - EXIT
  # Release after this run has completely finished touching the Docker project.
  release_project_lock() {
    if [[ "${lock_owned}" == 1 && -d "${project_lock}" ]]; then
      rmdir -- "${project_lock}" || code=1
    fi
  }
  if [[ "${startup_attempted}" == 1 ]]; then
    if [[ "${KEEP_E2E_STACK:-0}" == 1 ]]; then
      # Resume only the immutable IDs operated on by this run.
      if [[ -n "${paused_openfga}" ]]; then "${DOCKER[@]}" unpause "${paused_openfga}" >/dev/null || code=1; fi
      if [[ -n "${stopped_meili}" ]]; then "${DOCKER[@]}" start "${stopped_meili}" >/dev/null || code=1; fi
      printf 'Kept E2E project %s and private files at %s\n' "${PROJECT}" "${tmp}" >&2
      release_project_lock
      exit "${code}"
    fi
    if ! python3 "${SUPPORT}" cleanup "${PROJECT}" "${OWNER}"; then
      printf 'FAIL cleanup incomplete; owned project %s and private files at %s\n' "${PROJECT}" "${tmp}" >&2
      release_project_lock
      exit 1
    fi
  fi
  # tmp is the absolute directory allocated above by mktemp for this process.
  if [[ "${tmp}" == /* && -d "${tmp}" ]]; then rm -rf -- "${tmp:?}" || code=1; fi
  release_project_lock
  exit "${code}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "${expected}" == "${actual}" ]]; then
    pass=$((pass + 1))
    printf 'PASS  %-60s %s\n' "${name}" "${actual}"
  else
    fail=$((fail + 1))
    printf 'FAIL  %-60s expected=%s actual=%s\n' "${name}" "${expected}" "${actual}"
  fi
}

fatal() { printf 'FAIL  %s\n' "$*" >&2; exit 1; }
helper() { python3 "${SUPPORT}" "$@"; }

# Check the actual context/inventory; do not trust `compose ls` or just the
# running container port listing (which miss stopped containers and volumes).
helper context || fatal 'local Docker context preflight'
mkdir -- "${project_lock}" || fatal 'another E2E process already reserved this project (or left a lock)'
lock_owned=1
helper preflight "${PROJECT}" || fatal 'existing or unverifiable Compose project'
"${COMPOSE[@]}" config --format json >"${tmp}/base.json" || fatal 'base Compose render'
password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
key="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
ports="$(helper generate "${tmp}" "${PROJECT}" "${OWNER}" \
  "${ROOT_DIR}/deploy/compose/keycloak/realm-ontoaxis.json" "${tmp}/base.json" "${password}" "${key}" \
  "${E2E_KEYCLOAK_PORT:-0}" "${E2E_CORE_PORT:-0}" "${E2E_MEILI_PORT:-0}" "${E2E_WORKER_PORT:-0}")" || fatal 'select local ports and generate isolated Compose override'
unset password key
read -r kc_port core_port meili_port worker_port <<<"${ports}"
COMPOSE+=(-f "${tmp}/isolation.yaml")
"${COMPOSE[@]}" config --format json >"${tmp}/isolated.json" || fatal 'isolated Compose render (requires Compose !override support)'
helper check "${tmp}/isolated.json" "${PROJECT}" "${OWNER}" "${kc_port}" "${core_port}" "${meili_port}" "${worker_port}" \
  || fatal 'Compose loopback, labels and resources preflight'
helper preflight "${PROJECT}" || fatal 'project was claimed during preflight'
helper ports "${kc_port}" "${core_port}" "${meili_port}" "${worker_port}" || fatal 'loopback port claimed during preflight'
check 'local context, empty project, owned config and ports' yes yes

CORE_URL="http://127.0.0.1:${core_port}"
MEILI_URL="http://127.0.0.1:${meili_port}"
WORKER_URL="http://127.0.0.1:${worker_port}"
KEYCLOAK_URL="http://127.0.0.1:${kc_port}/realms/ontoaxis"
HTTP=(curl -q --noproxy '*' --connect-timeout 3 --max-time 10)

status() {
  local out="$1"; shift
  local value
  : >"${out}"
  value="$("${HTTP[@]}" -sS -o "${out}" -w '%{http_code}' "$@" 2>"${tmp}/curl.err")" || value=000
  printf '%s\n' "${value}"
}

wait_http() {
  local url="$1" timeout="$2" want="${3:-200}" deadline=$((SECONDS + $2)) value=000
  while (( SECONDS < deadline )); do
    value="$(status "${tmp}/probe.json" "${url}")"
    [[ "${value}" == "${want}" ]] && break
    sleep 1
  done
  printf '%s\n' "${value}"
}

api() {
  local actor="$1" method="$2" path="$3" out="$4" body="${5:-}"
  local args=(-X "${method}" "${CORE_URL}${path}" -D "${out}.headers")
  if [[ "${actor}" != anonymous ]]; then args+=(-H "@${tmp}/${actor}.header"); fi
  if [[ -n "${body}" ]]; then args+=(-H 'Content-Type: application/json' --data-binary "${body}"); fi
  status "${out}" "${args[@]}"
}

json() {
  # Expressions below are script literals. Never evaluate response contents.
  python3 -c "import json,sys,uuid; data=json.load(open(sys.argv[1])); print(${1})" "$2" 2>/dev/null || printf 'invalid-json\n'
}

require_id() {
  local id
  id="$(json 'uuid.UUID(data["id"])' "$1")"
  [[ "${id}" != invalid-json ]] || return 1
  printf '%s\n' "${id}"
}

query_path() {
  local escaped
  escaped="$(python3 -c 'from urllib.parse import quote; import sys; print(quote(sys.argv[1],safe=""))' "$1")"
  printf '/api/v1/objects/search?q=%s&limit=10%s\n' "${escaped}" "${2:-}"
}

# Match an exact response item, not just an estimated total/search substring.
wait_hit() {
  local actor="$1" query="$2" id="$3" name="$4" want="$5" deadline=$((SECONDS + $6)) path out="${tmp}/hit.json" code value=unavailable
  path="$(query_path "${query}")"
  while (( SECONDS < deadline )); do
    code="$(api "${actor}" GET "${path}" "${out}")"
    value="http-${code}"
    if [[ "${code}" == 200 ]]; then
      value="$(python3 - "${out}" "${id}" "${name}" <<'PY'
import json,sys
try:
    items = json.load(open(sys.argv[1]))['items']
    matches = [row for row in items if row['id'] == sys.argv[2]]
    print('absent' if not matches else 'present' if len(matches) == 1 and matches[0]['display_name'] == sys.argv[3] else 'mismatch')
except (ValueError, TypeError, KeyError):
    print('invalid-json')
PY
)"
    fi
    [[ "${value}" == "${want}" ]] && break
    sleep 1
  done
  printf '%s\n' "${value}"
}

sign_in() {
  local actor="$1" username="$2" tenant="$3" code
  code="$(status "${tmp}/${actor}.token" -X POST "${KEYCLOAK_URL}/protocol/openid-connect/token" \
    --data-urlencode 'grant_type=password' --data-urlencode 'client_id=ontoaxis-web' \
    --data-urlencode "username=${username}" --data-urlencode "password@${tmp}/password")"
  check "Keycloak signs local account ${actor}" 200 "${code}"
  [[ "${code}" == 200 ]] || fatal "could not obtain a Keycloak token for ${actor}"
  # Inspect claims only to reject a misconfigured fixture; core still verifies
  # the signature via its real JWKS. Never print or forge an access token.
  python3 - "${tmp}/${actor}.token" "${tmp}/${actor}.header" "${tenant}" "${actor}" "${KEYCLOAK_URL}" <<'PY' || fatal "unexpected Keycloak-issued claims for ${actor}"
import base64,json,sys
from pathlib import Path
try:
    token=json.loads(Path(sys.argv[1]).read_text())['access_token']
    payload=token.split('.')[1]
    claims=json.loads(base64.urlsafe_b64decode(payload+'='*(-len(payload)%4)))
    roles=set(claims.get('roles', []))
    if (claims.get('tenant_id') != sys.argv[3] or claims.get('iss') != sys.argv[5]
        or 'ontoaxis-api' not in claims.get('aud', [])
        or (sys.argv[4] != 'denied' and 'tenant-admin' not in roles)
        or (sys.argv[4] == 'denied' and roles.intersection({'platform-admin','tenant-admin','workspace-admin','member','viewer'}))):
        raise ValueError('unexpected fixture roles/tenant/issuer/audience')
    Path(sys.argv[2]).write_text('Authorization: Bearer '+token+'\n')
except (OSError, ValueError, KeyError, IndexError, TypeError):
    sys.exit(1)
PY
}

check_error() {
  local label="$1" expect_status="$2" expect_code="$3" out="$4" actual="$5"
  check "${label} HTTP" "${expect_status}" "${actual}"
  check "${label} error code" "${expect_code}" "$(json 'data["error"]["code"]' "${out}")"
  check "${label} exposes no items or total" True "$(json 'not ({"items", "total"} & data.keys())' "${out}")"
}

make_type() {
  local actor="$1" file="${tmp}/type-$1.json" code
  code="$(api "${actor}" POST /api/v1/ontology/object-types "${file}" \
    "{\"name\":\"e2e_material_${actor}\",\"display_name\":\"E2E 物料 ${actor}\",\"primary_key_property\":\"code\"}")"
  check "${actor} creates own object type" 201 "${code}"
  [[ "${code}" == 201 ]] || fatal 'object type fixture creation failed'
}

run_worker() { helper bounded 120 "${COMPOSE[@]}" run --rm --no-deps -T search-worker "$@"; }
sql() { helper bounded 15 "${COMPOSE[@]}" exec -T postgres psql -U ontoaxis -d ontoaxis -v ON_ERROR_STOP=1 -tAc "$1"; }

rebuild() {
  local label="$1" tenant="$2" code=0
  run_worker rebuild --tenant "${tenant}" --verify >"${tmp}/${label}.log" 2>&1 || code=$?
  check "${label} rebuild --verify exit" 0 "${code}"
  if [[ "${code}" == 0 ]] && helper rebuild "${tmp}/${label}.log" >"${tmp}/${label}.summary"; then
    check "${label} verified JSON summary" yes yes
    printf 'EVIDENCE rebuild %s: %s\n' "${label}" "$(<"${tmp}/${label}.summary")"
  else
    check "${label} verified JSON summary" yes no
  fi
}

echo "== starting isolated project ${PROJECT} (loopback: ${ports}) =="
startup_attempted=1
helper bounded "${E2E_START_TIMEOUT:-240}" "${COMPOSE[@]}" up -d --build postgres keycloak openfga authz-init meilisearch ontoaxis-core search-worker \
  || fatal 'Compose startup failed (partial startup is cleaned by run labels)'
for target in "${CORE_URL}/readyz" "${MEILI_URL}/health" "${WORKER_URL}/readyz" "${KEYCLOAK_URL}/.well-known/openid-configuration"; do
  code="$(wait_http "${target}" 180)"
  check "startup ${target}" 200 "${code}"
  [[ "${code}" == 200 ]] || fatal 'runtime startup readiness failed'
done
sign_in a e2e-a "${TENANT_A}"
sign_in b e2e-b "${TENANT_B}"
sign_in denied e2e-denied "${TENANT_A}"

# Independently scoped real tenants. No direct DB writes or request-supplied
# identity for either of these fixture types or their instances.
make_type a
make_type b
type_a="$(require_id "${tmp}/type-a.json")" || fatal 'missing tenant A fixture type ID'
type_b="$(require_id "${tmp}/type-b.json")" || fatal 'missing tenant B fixture type ID'
name_a='E2E 检索物料甲 amberwood'
name_updated='E2E 检索物料乙 bluequartz'
name_b='E2E 隔离物料乙 violetsteel'
code="$(api a POST /api/v1/objects "${tmp}/a.json" \
  "{\"object_type_id\":\"${type_a}\",\"properties\":{\"name\":\"${name_a}\",\"code\":\"M-1\",\"secret\":\"hiddene2esecret\"}}")"
check 'A creates object' 201 "${code}"
[[ "${code}" == 201 ]] || fatal 'A object fixture creation failed'
object_a="$(require_id "${tmp}/a.json")" || fatal 'missing tenant A fixture object ID'
check 'A finds exact created object' present "$(wait_hit a '检索物料 amberwood' "${object_a}" "${name_a}" present 60)"
code="$(api b POST /api/v1/objects "${tmp}/b.json" \
  "{\"object_type_id\":\"${type_b}\",\"properties\":{\"name\":\"${name_b}\",\"code\":\"M-2\"}}")"
check 'B creates object' 201 "${code}"
[[ "${code}" == 201 ]] || fatal 'B object fixture creation failed'
object_b="$(require_id "${tmp}/b.json")" || fatal 'missing tenant B fixture object ID'
check 'B finds exact own object' present "$(wait_hit b '隔离物料 violetsteel' "${object_b}" "${name_b}" present 60)"
code="$(api a PATCH "/api/v1/objects/${object_a}" "${tmp}/updated.json" \
  "{\"properties\":{\"name\":\"${name_updated}\"},\"replace\":false}")"
check 'A updates object' 200 "${code}"
check 'A finds updated display name' present "$(wait_hit a '检索物料 bluequartz' "${object_a}" "${name_updated}" present 60)"
check 'search drops stale name' absent "$(wait_hit a amberwood "${object_a}" "${name_a}" absent 30)"

uid_a="$(python3 -c 'import hashlib,sys; print("objects_v1_"+hashlib.sha256(sys.argv[1].encode()).hexdigest()[:16])' "${TENANT_A}")"
code="$(status "${tmp}/secret.json" -X POST "${MEILI_URL}/indexes/${uid_a}/search" -H "@${tmp}/meili-header" \
  -H 'Content-Type: application/json' --data-binary '{"q":"hiddene2esecret","limit":10}')"
check 'direct index probe succeeds' 200 "${code}"
check 'sensitive property is not indexed' 0 "$(json 'data["estimatedTotalHits"]' "${tmp}/secret.json")"

# Positive authorization requires the platform decision ID; isolation and
# scope denial are distinct checks. A 200 empty response never counts as 403.
printf '\n== real Keycloak / OpenFGA / Cedar authorization ==\n'
code="$(api a GET "$(query_path bluequartz "&object_type_id=${type_a}")" "${tmp}/scope-allow.json")"
check 'authorized same-type search HTTP' 200 "${code}"
check 'authorized same-type search finds A object' "${object_a}" "$(json 'data["items"][0]["id"]' "${tmp}/scope-allow.json")"
if grep -Eiq '^X-Ontoaxis-Decision-Id:[[:space:]]*[^[:space:]]' "${tmp}/scope-allow.json.headers"; then
  check 'live guard returned a decision ID' yes yes
else
  check 'live guard returned a decision ID' yes no
fi
code="$(api a GET "$(query_path violetsteel)" "${tmp}/isolation-search.json")"
check 'tenant A keyword search HTTP' 200 "${code}"
check 'tenant A keyword response cannot see tenant B object' True "$(json 'data["items"] == [] and data["total"] == 0' "${tmp}/isolation-search.json")"
code="$(api a GET "$(query_path violetsteel "&object_type_id=${type_b}")" "${tmp}/cross.json")"
check_error 'A filtering B type' 403 forbidden "${tmp}/cross.json" "${code}"
code="$(api a GET "$(query_path bluequartz "&object_type_id=${type_a}&object_type_id=${type_b}")" "${tmp}/mixed.json")"
check_error 'A filtering own and B type together' 403 forbidden "${tmp}/mixed.json" "${code}"
code="$(api denied GET "$(query_path bluequartz)" "${tmp}/denied.json")"
check_error 'signed A user without read role' 403 forbidden "${tmp}/denied.json" "${code}"
code="$(api anonymous GET "$(query_path bluequartz)" "${tmp}/anonymous.json")"
# The live core authenticator rejects missing credentials before the search handler.
check_error 'no access token' 401 unauthenticated "${tmp}/anonymous.json" "${code}"

fga_id="$(helper service-id "${PROJECT}" "${OWNER}" openfga)" || fatal 'cannot identify owned OpenFGA container'
"${DOCKER[@]}" pause "${fga_id}" >/dev/null || fatal 'could not pause owned OpenFGA'
paused_openfga="${fga_id}"
code="$(api a GET "$(query_path bluequartz)" "${tmp}/authz-down.json")"
check_error 'OpenFGA unavailable' 503 unavailable "${tmp}/authz-down.json" "${code}"
check 'OpenFGA failure comes from authorization guard' 'authorization temporarily unavailable' \
  "$(json 'data["error"]["message"]' "${tmp}/authz-down.json")"
check 'Meilisearch stays healthy during OpenFGA pause' 200 "$(status "${tmp}/authz-meili.json" "${MEILI_URL}/health")"
"${DOCKER[@]}" unpause "${fga_id}" >/dev/null || fatal 'could not resume owned OpenFGA'
paused_openfga=''
check 'authorized search recovers after OpenFGA resumes' present "$(wait_hit a bluequartz "${object_a}" "${name_updated}" present 30)"

printf '\n== consecutive operator rebuilds ==\n'
rebuild a-first "${TENANT_A}"
rebuild a-second "${TENANT_A}"
if [[ -s "${tmp}/a-first.summary" && -s "${tmp}/a-second.summary" ]] && cmp -s "${tmp}/a-first.summary" "${tmp}/a-second.summary"; then
  check 'two rebuilds: count, index, digest identical' yes yes
else
  check 'two rebuilds: count, index, digest identical' yes no
fi
check 'A still finds exact object after both rebuilds' present "$(wait_hit a bluequartz "${object_a}" "${name_updated}" present 30)"
rebuild b "${TENANT_B}"
check 'B still finds exact object after own rebuild' present "$(wait_hit b violetsteel "${object_b}" "${name_b}" present 30)"

printf '\n== deletes, search outage and recovery ==\n'
code="$(api a DELETE "/api/v1/objects/${object_a}" "${tmp}/deleted.json")"
check 'A deletes object' 204 "${code}"
check 'search removes deleted A object' absent "$(wait_hit a bluequartz "${object_a}" "${name_updated}" absent 60)"
meili_id="$(helper service-id "${PROJECT}" "${OWNER}" meilisearch)" || fatal 'cannot identify owned Meilisearch container'
"${DOCKER[@]}" stop --time 10 "${meili_id}" >/dev/null || fatal 'could not stop owned Meilisearch'
stopped_meili="${meili_id}"
code="$(api a GET "$(query_path violetsteel)" "${tmp}/meili-down.json")"
check_error 'Meilisearch unavailable' 503 unavailable "${tmp}/meili-down.json" "${code}"
check 'Meilisearch outage is retryable' True "$(json 'data["error"]["retryable"] is True' "${tmp}/meili-down.json")"
for path in healthz readyz; do
  check "core /${path} during Meilisearch outage" 200 "$(status "${tmp}/core-${path}.json" "${CORE_URL}/${path}")"
done
check 'worker liveness during Meilisearch outage' 200 "$(status "${tmp}/worker-health.json" "${WORKER_URL}/healthz")"
check 'worker readiness during Meilisearch outage' 503 "$(status "${tmp}/worker-down.json" "${WORKER_URL}/readyz")"
check 'worker readiness names the failing dependency' search "$(json 'data["dependency"]' "${tmp}/worker-down.json")"
name_recovered='E2E 停机期间创建 recoveryslate'
code="$(api a POST /api/v1/objects "${tmp}/outage-create.json" \
  "{\"object_type_id\":\"${type_a}\",\"properties\":{\"name\":\"${name_recovered}\",\"code\":\"M-3\"}}")"
check 'core create still works while search is down' 201 "${code}"
[[ "${code}" == 201 ]] || fatal 'could not create recovery fixture'
object_recovered="$(require_id "${tmp}/outage-create.json")" || fatal 'missing recovery fixture ID'
check 'core get created object while search is down' 200 "$(api a GET "/api/v1/objects/${object_recovered}" "${tmp}/outage-get.json")"
code="$(api a PATCH "/api/v1/objects/${object_recovered}" "${tmp}/outage-patch.json" '{"properties":{"code":"M-3U"},"replace":false}')"
check 'core update while search is down' 200 "${code}"
"${DOCKER[@]}" start "${meili_id}" >/dev/null || fatal 'could not restart owned Meilisearch'
stopped_meili=''
check 'Meilisearch health after restart' 200 "$(wait_http "${MEILI_URL}/health" 120)"
check 'worker ready after restart' 200 "$(wait_http "${WORKER_URL}/readyz" 60)"
check 'consumer automatically catches up after restart (no manual replay)' present \
  "$(wait_hit a recoveryslate "${object_recovered}" "${name_recovered}" present 60)"
check 'core deletes recovery fixture' 204 "$(api a DELETE "/api/v1/objects/${object_recovered}" "${tmp}/recovery-delete.json")"
check 'search removes recovery fixture' absent "$(wait_hit a recoveryslate "${object_recovered}" "${name_recovered}" absent 60)"
pending='unknown'
deadline=$((SECONDS + 45))
while (( SECONDS < deadline )); do
  pending="$(sql "SELECT count(*) FROM jobs.outbox WHERE tenant_id IN ('${TENANT_A}','${TENANT_B}') AND published_at IS NULL" | tr -d '[:space:]')" || pending=sql-error
  [[ "${pending}" == 0 ]] && break
  sleep 2
done
check 'synthetic tenant outboxes drain' 0 "${pending}"

metrics="${tmp}/worker.metrics"
check 'worker metrics endpoint' 200 "$(status "${metrics}" "${WORKER_URL}/metrics")"
if grep -Eq '^ontoaxis_search_index_lag_seconds_count [1-9][0-9]*$' "${metrics}"; then
  check 'worker exposes nonzero index-lag histogram' yes yes
  printf 'EVIDENCE consumer lag histogram (synthetic functional run, not HTTP p95/SLO):\n'
  grep '^ontoaxis_search_index_lag_seconds_' "${metrics}"
else
  check 'worker exposes nonzero index-lag histogram' yes no
fi
printf '\nobject-search e2e: %d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" == 0 ]]
