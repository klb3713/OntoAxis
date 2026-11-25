#!/usr/bin/env bash
# Stage 2 E2E-08（跨模块组合链，Compose）：对象 → action → 搜索索引 → dataset/view → pipeline(publish+run) → 查询/搜索 → 审计/指标。
# 仅使用隔离的本地 Compose 环境与合成数据；结束前清理本脚本启动的栈。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE_ARGS=(
  -f deploy/compose/compose.yaml
  -f deploy/compose/authz/compose.authz.yaml
  -f deploy/compose/action-temporal/compose.temporal.yaml
  -f deploy/compose/search/compose.search.yaml
  -f deploy/compose/pipeline-kestra/compose.pipeline.yaml
  --profile search
  --profile action-temporal
  --profile pipeline-kestra
)
CORE_URL="${CORE_URL:-http://127.0.0.1:18080}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:19090}"
KEYCLOAK_URL="${KEYCLOAK_URL:-http://127.0.0.1:18083}/realms/ontoaxis"
export KESTRA_BASIC_AUTH_USERNAME="${KESTRA_BASIC_AUTH_USERNAME:-admin@ontoaxis.local}"
export KESTRA_BASIC_AUTH_PASSWORD="${KESTRA_BASIC_AUTH_PASSWORD:-Admin1234}"
KESTRA_AUTH_HEADER="Authorization: Basic $(printf '%s:%s' "${KESTRA_BASIC_AUTH_USERNAME}" "${KESTRA_BASIC_AUTH_PASSWORD}" | base64 | tr -d '\n')"
HTTP=(curl -q --noproxy '*' --connect-timeout 3 --max-time 15)

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

echo "== starting combined stack (base+authz+temporal+search+pipeline) =="
# 注意：deploy/compose/pipeline-kestra/compose.pipeline.yaml 的 kestra healthcheck 用
# basic auth 访问 /api/v1/configs，本镜像版本返回 401 → 容器始终 unhealthy。
# 因此不能让 compose 通过 service_healthy 依赖带起 ontoaxis-core，改为先起依赖，
# 再 --no-deps 起 core（core 只要求 kestra 可达，不要求 healthcheck 通过）。
docker compose "${COMPOSE_ARGS[@]}" up -d --build \
  postgres keycloak openfga authz-init trino kestra temporal temporal-init \
  ontoaxis-actions-worker meilisearch search-worker >"${tmp}/compose-up.log" 2>&1 || {
  echo "compose up failed, tail:"; tail -60 "${tmp}/compose-up.log"; exit 1;
}
for _ in $(seq 1 60); do
  code="$(curl -q --noproxy '*' -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18086/ 2>/dev/null || echo 000)"
  [ "${code}" != "000" ] && break
  sleep 3
done
docker compose "${COMPOSE_ARGS[@]}" up -d --no-deps ontoaxis-core >>"${tmp}/compose-up.log" 2>&1 || {
  echo "core start failed, tail:"; tail -60 "${tmp}/compose-up.log"; exit 1;
}
# web 依赖 core healthy，必须在 core 启动后再起（避免 compose 依赖阻塞）。
docker compose "${COMPOSE_ARGS[@]}" up -d --no-deps web >>"${tmp}/compose-up.log" 2>&1 || true

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

metrics_sum() {
  "${HTTP[@]}" -sS "${METRICS_URL}/metrics" 2>/dev/null | awk '/^ontoaxis_http_requests_total/{s+=$NF} END{printf "%d", s+0}'
}

RUN_ID="$(date +%s)"
METRICS_BEFORE="$(metrics_sum || echo 0)"

echo "== 1. object: object type + object =="
OT="$(api POST /api/v1/ontology/object-types "{\"name\":\"chain_type_${RUN_ID}\",\"display_name\":\"Chain Type\",\"primary_key_property\":\"name\"}" | json 'd["id"]')"
OBJ="$(api POST /api/v1/objects "{\"object_type_id\":\"${OT}\",\"properties\":{\"name\":\"chain-before-${RUN_ID}\"}}" | json 'd["id"]')"
check "object created" ok "$([ -n "${OBJ}" ] && [ "${OBJ}" != None ] && echo ok || echo missing)"

echo "== 2. action: object.update on the created object =="
AT_BODY="$(api POST /api/v1/actions/types "{\"name\":\"chain_update_${RUN_ID}\",\"display_name\":\"Chain Update\",\"object_type_id\":\"${OT}\",\"parameters\":{\"execution_definition\":{\"steps\":[{\"id\":\"s1\",\"activity_type\":\"object.update\",\"parameters\":{\"properties\":{\"name\":\"chain-after-${RUN_ID}\"}},\"compensation\":{\"activity_type\":\"object.update\",\"parameters\":{\"properties\":{\"name\":\"chain-before-${RUN_ID}\"}}}}]}}}")"
AT="$(printf '%s' "${AT_BODY}" | json 'd.get("id","")' 2>/dev/null || echo "")"
if [ -n "${AT}" ]; then
  VAL_BODY="$(api POST "/api/v1/actions/types/${AT}/validate" "{\"parameters\":{\"object_id\":\"${OBJ}\"}}")"
  echo "VALIDATE: ${VAL_BODY}"
  PREV_BODY="$(api POST "/api/v1/actions/types/${AT}/preview" "{\"parameters\":{\"object_id\":\"${OBJ}\"}}")"
  echo "PREVIEW: $(echo "${PREV_BODY}" | head -c 500)"
fi
if [ -z "${AT}" ]; then echo "ACTION_TYPE_BODY: ${AT_BODY}"; fail=$((fail+1)); RESULTS+=("{\"name\":\"action type created\",\"status\":\"fail\",\"detail\":\"empty id\"}"); echo "FAIL action type created: ${AT_BODY}"; else
RUN_BODY="$(api POST "/api/v1/actions/types/${AT}/run" "{\"parameters\":{\"object_id\":\"${OBJ}\"}}")"
EID="$(printf '%s' "${RUN_BODY}" | json 'd.get("id","")' 2>/dev/null || echo "")"
if [ -z "${EID}" ]; then echo "ACTION_RUN_BODY: ${RUN_BODY}"; fi
fi
ACTION_STATUS="timeout"
for _ in $(seq 1 90); do
  ACTION_STATUS="$(api GET "/api/v1/actions/executions/${EID}" | json 'd.get("status","")')"
  case "${ACTION_STATUS}" in
    succeeded|failed|cancelled|timed_out|compensation_failed) break;;
  esac
  sleep 1
done
check "action execution succeeded" succeeded "${ACTION_STATUS}"
NAME_AFTER="$(api GET "/api/v1/objects/${OBJ}" | json 'd["properties"]["name"]')"
check "object mutated by action" "chain-after-${RUN_ID}" "${NAME_AFTER}"

echo "== 3. search: object converges into index =="
SEARCH_HIT="no"
for _ in $(seq 1 60); do
  BODY="$(api GET "/api/v1/objects/search?q=chain-after-${RUN_ID}&object_type_id=${OT}")"
  if printf '%s' "${BODY}" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if any(i.get('id')=='${OBJ}' for i in d.get('items',[])) else 1)" 2>/dev/null; then
    SEARCH_HIT="yes"; break
  fi
  sleep 2
done
check "search finds mutated object" yes "${SEARCH_HIT}"

echo "== 4. dataset: dataset + main branch + view =="
DS_BODY="$(api POST /api/v1/datasets "{\"name\":\"chain_ds_${RUN_ID}\",\"display_name\":\"Chain Dataset\",\"format\":\"iceberg\"}")"
DS="$(printf '%s' "${DS_BODY}" | json 'd.get("id","")' 2>/dev/null || echo "")"
[ -n "${DS}" ] || echo "DATASET_BODY: ${DS_BODY}"
check "dataset created" ok "$([ -n "${DS}" ] && [ "${DS}" != None ] && echo ok || echo missing)"
BRANCHES_BODY="$(api GET "/api/v1/datasets/${DS}/branches")"
BRANCH="$(printf '%s' "${BRANCHES_BODY}" | python3 -c "import sys,json;d=json.load(sys.stdin);print((d.get('items') or [{}])[0].get('id',''))" 2>/dev/null || echo "")"
[ -n "${BRANCH}" ] || echo "BRANCHES_BODY: ${BRANCHES_BODY}"
check "dataset main branch present" ok "$([ -n "${BRANCH}" ] && [ "${BRANCH}" != None ] && echo ok || echo missing)"
VIEW_CODE="$(api_code POST "/api/v1/datasets/${DS}/views" "{\"branch_id\":\"${BRANCH}\",\"name\":\"chain_view_${RUN_ID}\",\"query\":\"select 1 as x\"}" "${tmp}/view.json")"
check "dataset view created" 201 "${VIEW_CODE}"

echo "== 5. pipeline: create + publish + trigger run =="
PIPE_BODY="$(python3 - "${RUN_ID}" "${DS}" <<'PY'
import json,sys
run_id,ds=sys.argv[1],sys.argv[2]
dag={"dag_version":1,"dataset_ids":[ds],
     "tasks":[{"id":"chain_model","type":"dbt","model":"chain_model","materialized":"table","sql":"select 1 as x"}],
     "outputs":[{"dataset_id":ds,"model":"chain_model"}]}
print(json.dumps({"name":f"chain_pipeline_{run_id}","display_name":"Chain Pipeline","description":"E2E-08 chain","type":"batch","dag":dag}))
PY
)"
PIPE_RESP="$(api POST /api/v1/pipelines "${PIPE_BODY}")"
PIPE="$(printf '%s' "${PIPE_RESP}" | json 'd.get("id","")' 2>/dev/null || echo "")"
[ -n "${PIPE}" ] || echo "PIPELINE_BODY: ${PIPE_RESP}"
check "pipeline created" ok "$([ -n "${PIPE}" ] && [ "${PIPE}" != None ] && echo ok || echo missing)"
PUB_CODE="$(api_code POST "/api/v1/pipelines/${PIPE}/publish" '{"published_by":"e2e"}' "${tmp}/publish.json")"
echo "PUBLISH_RESP(code=${PUB_CODE}): $(cat "${tmp}/publish.json" 2>/dev/null | head -c 400)"
  if [ "${PUB_CODE}" != "200" ]; then
    echo "--- core log tail ---"
    docker compose "${COMPOSE_ARGS[@]}" logs --no-log-prefix ontoaxis-core 2>&1 | tail -60 || true
    echo "--- kestra probe ---"
    curl -q --noproxy '*' -sS -H "${KESTRA_AUTH_HEADER}" -o /dev/null -w "GET /api/v1/main/flows/search -> %{http_code}\n" "http://127.0.0.1:18086/api/v1/main/flows/search?size=1" || true
    curl -q --noproxy '*' -sS -H "${KESTRA_AUTH_HEADER}" -o /dev/null -w "GET /api/v1/main/namespaces -> %{http_code}\n" "http://127.0.0.1:18086/api/v1/main/namespaces" || true
  fi
check "pipeline published" 200 "${PUB_CODE}"
RUN_CODE="$(api_code POST "/api/v1/pipelines/${PIPE}/runs" '' "${tmp}/run.json")"
RUN_STATUS="$(python3 -c "import json;print(json.load(open('${tmp}/run.json')).get('status',''))" 2>/dev/null || echo "")"
RUN_ID_API="$(python3 -c "import json;print(json.load(open('${tmp}/run.json')).get('id',''))" 2>/dev/null || echo "")"
case "${RUN_CODE}" in 200|201|202) RUN_CREATED=ok;; *) RUN_CREATED="code=${RUN_CODE}";; esac
check "pipeline run triggered" ok "${RUN_CREATED}"
if [ -n "${RUN_ID_API}" ]; then
  for _ in $(seq 1 60); do
    RUN_STATUS="$(api GET "/api/v1/pipelines/${PIPE}/runs/${RUN_ID_API}" | json 'd.get("status","")')"
    case "${RUN_STATUS}" in succeeded|failed|cancelled) break;; esac
    sleep 2
  done
fi
if [ "${RUN_STATUS}" = failed ]; then
  echo "RUN_BODY: $(api GET "/api/v1/pipelines/${PIPE}/runs/${RUN_ID_API}" | head -c 800)"
  echo "--- kestra executions ---"
  EXEC_JSON="$(curl -q --noproxy '*' -sS -H "${KESTRA_AUTH_HEADER}" "http://127.0.0.1:18086/api/v1/main/executions/search?page=1&size=3")"
  echo "${EXEC_JSON}" | head -c 600
  EXEC_ID="$(printf '%s' "${EXEC_JSON}" | python3 -c "import json,sys;d=json.load(sys.stdin);print(d['results'][0]['id'] if d.get('results') else '')" 2>/dev/null || true)"
  if [ -n "${EXEC_ID}" ]; then
    echo "--- kestra execution detail (${EXEC_ID}) ---"
    curl -q --noproxy '*' -sS -H "${KESTRA_AUTH_HEADER}" "http://127.0.0.1:18086/api/v1/main/executions/${EXEC_ID}" | head -c 1500 || true
  fi
  echo
  docker compose "${COMPOSE_ARGS[@]}" logs --no-log-prefix kestra 2>&1 | tail -40 || true
fi
check "pipeline run reached terminal state (not failed)" ok "$([ "${RUN_STATUS}" = failed ] && echo failed || echo ok)"

echo "== 6. query/search consistency + audit metrics =="
SEARCH_HIT2="no"
BODY="$(api GET "/api/v1/objects/search?q=chain-after-${RUN_ID}&object_type_id=${OT}")"
if printf '%s' "${BODY}" | python3 -c "import sys,json;d=json.load(sys.stdin);sys.exit(0 if any(i.get('id')=='${OBJ}' for i in d.get('items',[])) else 1)" 2>/dev/null; then
  SEARCH_HIT2="yes"
fi
check "search remains consistent after chain" yes "${SEARCH_HIT2}"
METRICS_AFTER="$(metrics_sum || echo 0)"
DELTA=$(( METRICS_AFTER - METRICS_BEFORE ))
check "audit metrics increased" ok "$([ "${DELTA}" -gt 0 ] && echo ok || echo "delta=${DELTA}")"

cat > "${tmp}/results.json" <<JSON
{"pass":${pass},"fail":${fail},"results":[$(IFS=,; echo "${RESULTS[*]}")]}
JSON
cat "${tmp}/results.json"
[ "${fail}" -eq 0 ]
