#!/usr/bin/env bash
# Stage 2 E2E-06（Kestra + dbt → dataset，Compose）：
#   1) 创建 dataset + 含 dbt 任务的 pipeline（dbt 模型物化到 Trino memory catalog）；
#   2) publish + trigger run，断言 run 变为 succeeded（core 会解析 run_results，failed>0 即 fail）；
#   3) 直接查询 Trino memory catalog，断言 dbt 真正产出了数据（表存在且内容符合）。
# 仅使用隔离的本地 Compose 环境与合成数据；结束前清理本脚本启动的栈。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE_ARGS=(
  -f deploy/compose/compose.yaml
  -f deploy/compose/authz/compose.authz.yaml
  -f deploy/compose/search/compose.search.yaml
  -f deploy/compose/pipeline-kestra/compose.pipeline.yaml
  --profile search
  --profile pipeline-kestra
)
CORE_URL="${CORE_URL:-http://127.0.0.1:18080}"
TRINO_URL="${TRINO_URL:-http://127.0.0.1:18081}"
KESTRA_URL="${KESTRA_URL:-http://127.0.0.1:18086}"
KEYCLOAK_URL="${KEYCLOAK_URL:-http://127.0.0.1:18083}/realms/ontoaxis"
export KESTRA_BASIC_AUTH_USERNAME="${KESTRA_BASIC_AUTH_USERNAME:-admin@ontoaxis.local}"
export KESTRA_BASIC_AUTH_PASSWORD="${KESTRA_BASIC_AUTH_PASSWORD:-Admin1234}"
KESTRA_AUTH_HEADER="Authorization: Basic $(printf '%s:%s' "${KESTRA_BASIC_AUTH_USERNAME}" "${KESTRA_BASIC_AUTH_PASSWORD}" | base64 | tr -d '\n')"
HTTP=(curl -q --noproxy '*' --connect-timeout 3 --max-time 20)

pass=0
fail=0
RESULTS=()
tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}"
  docker compose "${COMPOSE_ARGS[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "${expected}" == "${actual}" ]]; then
    pass=$((pass + 1)); RESULTS+=("{\"name\":\"${name}\",\"status\":\"pass\"}"); echo "PASS ${name} (${actual})"
  else
    fail=$((fail + 1)); RESULTS+=("{\"name\":\"${name}\",\"status\":\"fail\",\"detail\":\"expected ${expected}, got ${actual}\"}"); echo "FAIL ${name}: expected ${expected}, got ${actual}"
  fi
}

status() {
  local out="$1"; shift
  local value
  : >"${out}"
  value="$("${HTTP[@]}" -sS -o "${out}" -w '%{http_code}' "$@" 2>"${tmp}/curl.err")" || value=000
  printf '%s\n' "${value}"
}

wait_code() {
  local url="$1" want="$2" timeout="${3:-90}"
  local deadline=$((SECONDS + timeout)) value=000
  while (( SECONDS < deadline )); do
    value="$(status "${tmp}/probe.body" "${url}")"
    [[ "${value}" == "${want}" ]] && break
    sleep 2
  done
  printf '%s\n' "${value}"
}

json() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

# trino_rows "<sql>" —— 打印 Trino 返回的 rows 的 JSON 数组（走 /v1/statement 协议，免鉴权）。
trino_rows() {
  python3 - "$1" <<'PY'
import sys, json, urllib.request, urllib.parse
sql = sys.argv[1]
base = "http://127.0.0.1:18081"

def fetch(url, data=None):
    req = urllib.request.Request(url, data=data,
                                 headers={"X-Trino-User": "e2e", "X-Trino-Catalog": "memory",
                                          "Content-Type": "text/plain"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)

resp = fetch(base + "/v1/statement", sql.encode())
if resp.get("error"):
    sys.stderr.write("TRINO_ERROR: " + json.dumps(resp["error"]) + "\n")
    sys.exit(2)
rows = list(resp.get("data") or [])
nxt = resp.get("nextUri")
guard = 0
while nxt and guard < 120:
    resp = fetch(nxt)
    if resp.get("error"):
        sys.stderr.write("TRINO_ERROR: " + json.dumps(resp["error"]) + "\n")
        sys.exit(2)
    rows += list(resp.get("data") or [])
    nxt = resp.get("nextUri")
    guard += 1
print(json.dumps(rows))
PY
}

echo "== starting pipeline+search stack (base+authz+search+pipeline) =="
docker compose "${COMPOSE_ARGS[@]}" up -d --build \
  postgres keycloak openfga authz-init trino kestra meilisearch search-worker >"${tmp}/compose-up.log" 2>&1 || {
  echo "compose up failed, tail:"; tail -80 "${tmp}/compose-up.log"; exit 1;
}
for _ in $(seq 1 80); do
  code="$(curl -q --noproxy '*' -s -o /dev/null -w '%{http_code}' ${KESTRA_URL}/ 2>/dev/null || echo 000)"
  [ "${code}" != "000" ] && break
  sleep 3
done
docker compose "${COMPOSE_ARGS[@]}" up -d --no-deps ontoaxis-core >>"${tmp}/compose-up.log" 2>&1 || {
  echo "core start failed, tail:"; tail -80 "${tmp}/compose-up.log"; exit 1;
}

check "core /healthz" 200 "$(wait_code "${CORE_URL}/healthz" 200 300)"
check "core /readyz" 200 "$(wait_code "${CORE_URL}/readyz" 200 300)"
check "keycloak realm discovery" 200 "$(wait_code "${KEYCLOAK_URL}/.well-known/openid-configuration" 200 300)"

echo "== authenticating =="
TOKEN=""
for _ in $(seq 1 120); do
  if TOKEN="$(curl -q --noproxy '*' -sS -X POST "${KEYCLOAK_URL}/protocol/openid-connect/token" \
      -H 'Content-Type: application/x-www-form-urlencoded' \
      -d 'grant_type=password&client_id=ontoaxis-web&username=admin&password=admin123' 2>/dev/null | json 'd["access_token"]')" && [ -n "${TOKEN}" ]; then
    break
  fi
  sleep 3
done
[[ -n "${TOKEN}" ]] || { echo "FAIL: could not obtain token"; exit 1; }

api() {
  local method="$1" path="$2" body="${3:-}"
  if [ -n "${body}" ]; then
    "${HTTP[@]}" -sS -X "${method}" "${CORE_URL}${path}" -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d "${body}"
  else
    "${HTTP[@]}" -sS -X "${method}" "${CORE_URL}${path}" -H "Authorization: Bearer ${TOKEN}"
  fi
}
api_code() {
  local method="$1" path="$2" body="${3:-}" out="$4"
  if [ -n "${body}" ]; then
    status "${out}" -X "${method}" "${CORE_URL}${path}" -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d "${body}"
  else
    status "${out}" -X "${method}" "${CORE_URL}${path}" -H "Authorization: Bearer ${TOKEN}"
  fi
}

RUN_ID="$(date +%s)"

echo "== 1. dataset =="
DS_BODY="$(api POST /api/v1/datasets "{\"name\":\"e2e06_ds_${RUN_ID}\",\"display_name\":\"E2E06 Dataset\",\"format\":\"iceberg\"}")"
DS="$(printf '%s' "${DS_BODY}" | json 'd.get("id","")' 2>/dev/null || echo "")"
[ -n "${DS}" ] || echo "DATASET_BODY: ${DS_BODY}"
check "dataset created" ok "$([ -n "${DS}" ] && [ "${DS}" != None ] && echo ok || echo missing)"

echo "== 2. pipeline with a dbt task (materialized table, puts dataset) =="
PIPE_BODY="$(python3 - "${RUN_ID}" "${DS}" <<'PY'
import json, sys
run_id, ds = sys.argv[1], sys.argv[2]
dag = {
    "dag_version": 1,
    "dataset_ids": [ds],
    "tasks": [{
        "id": "pipeline_dbt_model",
        "type": "dbt",
        "model": "pipeline_dbt_model",
        "materialized": "table",
        "sql": "select 42 as answer",
    }],
    "outputs": [{"dataset_id": ds, "model": "pipeline_dbt_model"}],
}
print(json.dumps({
    "name": f"e2e06_pipeline_{run_id}",
    "display_name": "E2E06 dbt Pipeline",
    "description": "E2E-06 Kestra+dbt -> dataset",
    "type": "batch",
    "dag": dag,
}))
PY
)"
PIPE_RESP="$(api POST /api/v1/pipelines "${PIPE_BODY}")"
PIPE="$(printf '%s' "${PIPE_RESP}" | json 'd.get("id","")' 2>/dev/null || echo "")"
[ -n "${PIPE}" ] || echo "PIPELINE_BODY: ${PIPE_RESP}"
check "pipeline created" ok "$([ -n "${PIPE}" ] && [ "${PIPE}" != None ] && echo ok || echo missing)"

echo "== 3. publish + trigger run =="
PUB_CODE="$(api_code POST "/api/v1/pipelines/${PIPE}/publish" '{"published_by":"e2e"}' "${tmp}/publish.json")"
echo "PUBLISH_RESP(code=${PUB_CODE}): $(head -c 400 "${tmp}/publish.json" 2>/dev/null)"
check "pipeline published" 200 "${PUB_CODE}"

RUN_CODE="$(api_code POST "/api/v1/pipelines/${PIPE}/runs" '' "${tmp}/run.json")"
RUN_ID_API="$(python3 -c "import json;print(json.load(open('${tmp}/run.json')).get('id',''))" 2>/dev/null || echo "")"
case "${RUN_CODE}" in 200|201|202) RUN_CREATED=ok;; *) RUN_CREATED="code=${RUN_CODE}";; esac
check "pipeline run triggered" ok "${RUN_CREATED}"

RUN_STATUS="timeout"
if [ -n "${RUN_ID_API}" ]; then
  for _ in $(seq 1 150); do
    RUN_STATUS="$(api GET "/api/v1/pipelines/${PIPE}/runs/${RUN_ID_API}" | json 'd.get("status","")')"
    case "${RUN_STATUS}" in succeeded|failed|cancelled) break;; esac
    sleep 2
  done
fi
if [ "${RUN_STATUS}" = failed ]; then
  echo "RUN_BODY: $(api GET "/api/v1/pipelines/${PIPE}/runs/${RUN_ID_API}" | head -c 800)"
  echo "--- kestra executions ---"
  EXEC_JSON="$(curl -q --noproxy '*' -sS -H "${KESTRA_AUTH_HEADER}" "${KESTRA_URL}/api/v1/main/executions/search?page=1&size=3" || true)"
  echo "${EXEC_JSON}" | head -c 600
  echo
  docker compose "${COMPOSE_ARGS[@]}" logs --no-log-prefix kestra 2>&1 | tail -60 || true
fi
check "pipeline run succeeded (dbt all passed)" succeeded "${RUN_STATUS}"

echo "== 4. assert dbt actually produced data in Trino memory catalog =="
CATALOGS_JSON="$(trino_rows "SHOW CATALOGS" 2>/dev/null || echo '[]')"
HAS_MEMORY="$(printf '%s' "${CATALOGS_JSON}" | python3 -c "import sys,json;rows=json.load(sys.stdin);print('yes' if any(str(c[0])=='memory' for c in rows) else 'no')")"
check "trino memory catalog present" yes "${HAS_MEMORY}"

PIPE8="$(printf '%s' "${PIPE}" | tr -d '-' | cut -c1-8)"
SCHEMAS_JSON="$(trino_rows "SHOW SCHEMAS FROM memory" 2>/dev/null || echo '[]')"
SCHEMA="$(printf '%s' "${SCHEMAS_JSON}" | python3 -c "import sys,json;rows=json.load(sys.stdin);m=[str(r[0]) for r in rows if str(r[0]).endswith('_p'+'${PIPE8}')];print(m[0] if m else '')")"
check "dbt-derived schema exists (memory.oa_*_p${PIPE8})" ok "$([ -n "${SCHEMA}" ] && echo ok || echo "missing")"

if [ -n "${SCHEMA}" ]; then
  DATA_JSON="$(trino_rows "SELECT answer FROM memory.\"${SCHEMA}\".pipeline_dbt_model" 2>/dev/null || echo '[]')"
  echo "TRINO_ROWS: ${DATA_JSON}"
  ANSWER="$(printf '%s' "${DATA_JSON}" | python3 -c "import sys,json;rows=json.load(sys.stdin);print('' if not rows else str(rows[0][0]))")"
  check "dbt model produced expected row (answer=42)" 42 "${ANSWER}"
fi

cat > "${tmp}/results.json" <<JSON
{"pass":${pass},"fail":${fail},"results":[$(IFS=,; echo "${RESULTS[*]}")]}
JSON
cat "${tmp}/results.json"
[ "${fail}" -eq 0 ]
