#!/usr/bin/env bash
# Stage 2 E2E-01（Compose 基础链路）：Keycloak 登录 → web 静态入口 → 对象读写 → health/readyz。
# 只使用隔离的本地 Compose 环境与合成数据；结束前清理本脚本启动的栈。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE_ARGS=(-f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml)
CORE_URL="${CORE_URL:-http://127.0.0.1:18080}"
WEB_URL="${WEB_URL:-http://127.0.0.1:18082}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:19090}"
KEYCLOAK_URL="${KEYCLOAK_URL:-http://127.0.0.1:18083}/realms/ontoaxis"
HTTP=(curl -q --noproxy '*' --connect-timeout 3 --max-time 10)

pass=0
fail=0
tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}"
  docker compose "${COMPOSE_ARGS[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "${expected}" == "${actual}" ]]; then
    pass=$((pass + 1)); echo "PASS ${name} (${actual})"
  else
    fail=$((fail + 1)); echo "FAIL ${name}: expected ${expected}, got ${actual}"
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
  local url="$1" want="$2"
  local timeout="${3:-90}"
  local deadline=$((SECONDS + timeout))
  local value=000
  while (( SECONDS < deadline )); do
    value="$(status "${tmp}/probe.body" "${url}")"
    [[ "${value}" == "${want}" ]] && break
    sleep 2
  done
  printf '%s\n' "${value}"
}

echo "starting base compose stack"
docker compose "${COMPOSE_ARGS[@]}" up -d --build

check "core /healthz" 200 "$(wait_code "${CORE_URL}/healthz" 200 120)"
check "core /readyz" 200 "$(wait_code "${CORE_URL}/readyz" 200 120)"
check "web entry" 200 "$(wait_code "${WEB_URL}/" 200 120)"
check "keycloak realm discovery" 200 "$(wait_code "${KEYCLOAK_URL}/.well-known/openid-configuration" 200 120)"

code="$(status "${tmp}/metrics" "${METRICS_URL}/metrics")"
check "core /metrics" 200 "${code}"
if grep -q 'ontoaxis_http_requests_total' "${tmp}/metrics"; then
  check "metrics exposes ontoaxis_http_requests_total" ok ok
else
  check "metrics exposes ontoaxis_http_requests_total" ok missing
fi

status "${tmp}/web.html" "${WEB_URL}/" >/dev/null
if grep -qiE 'ontoaxis|<div id="root"|<div id=root' "${tmp}/web.html"; then
  check "web serves SPA shell" ok ok
else
  check "web serves SPA shell" ok missing
fi

code="$(status "${tmp}/unknown" "${CORE_URL}/api/v1/not-registered")"
check "unknown route 404" 404 "${code}"

echo "authenticating via Keycloak (admin)"
code="$(status "${tmp}/token.json" -X POST "${KEYCLOAK_URL}/protocol/openid-connect/token" \
  --data-urlencode 'grant_type=password' --data-urlencode 'client_id=ontoaxis-web' \
  --data-urlencode 'username=admin' --data-urlencode 'password=admin123')"
check "Keycloak password grant" 200 "${code}"
if [[ "${code}" == "200" ]]; then
  token="$(python3 - "${tmp}/token.json" <<'PY'
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("access_token", ""))
except (OSError, ValueError):
    print("")
PY
)"
else
  token=""
fi
if [[ -n "${token}" && "$(awk -F. '{print NF}' <<<"${token}")" == "3" ]]; then
  check "issued JWT well-formed" ok ok
else
  check "issued JWT well-formed" ok missing
fi
printf 'Authorization: Bearer %s\n' "${token}" >"${tmp}/auth.header"

api() {
  local method="$1" path="$2" out="$3" body="${4:-}"
  local args=(-X "${method}" "${CORE_URL}${path}" -H "@${tmp}/auth.header")
  if [[ -n "${body}" ]]; then args+=(-H 'Content-Type: application/json' --data-binary "${body}"); fi
  status "${out}" "${args[@]}"
}

code="$(api POST /api/v1/ontology/object-types "${tmp}/type.json" '{"name":"e2e_workbench","display_name":"E2E Workbench","primary_key_property":"code"}')"
check "create object type" 201 "${code}"
type_id="$(python3 - "${tmp}/type.json" <<'PY'
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("id", ""))
except (OSError, ValueError):
    print("")
PY
)"
if [[ -n "${type_id}" ]]; then check "object type has id" ok ok; else check "object type has id" ok missing; fi

code="$(api POST /api/v1/objects "${tmp}/obj.json" "{\"object_type_id\":\"${type_id}\",\"properties\":{\"name\":\"E2E 工作台对象\",\"code\":\"WB-1\"}}")"
check "create object" 201 "${code}"
obj_id="$(python3 - "${tmp}/obj.json" <<'PY'
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("id", ""))
except (OSError, ValueError):
    print("")
PY
)"
check "get object" 200 "$(api GET "/api/v1/objects/${obj_id}" "${tmp}/obj-get.json")"
check "patch object" 200 "$(api PATCH "/api/v1/objects/${obj_id}" "${tmp}/obj-patch.json" '{"properties":{"code":"WB-1U"},"replace":false}')"
check "delete object" 204 "$(api DELETE "/api/v1/objects/${obj_id}" "${tmp}/obj-del.json")"

echo "workbench e2e: ${pass} passed, ${fail} failed"
[[ "${fail}" -eq 0 ]]
