#!/usr/bin/env bash
# connector-runtime-e2e.sh — Stage 2 CDC 负向矩阵与可选成功路径。
#
# 默认只跑快速负向用例（不依赖长时间快照）：未知资源 404、非法同步配置 400、
# 重复启动幂等、运行中取消。加 --with-success 跑完整同步到 succeeded（需要
# 5 分钟以上的提交窗口）；加 --with-outage 额外验证 Kafka Connect 不可达时
# StartSync 返回 503 且不伪造作业。
#
# 该脚本面向本地 connector-runtime profile（合成数据），不是生产探测。
set -euo pipefail

API="${ONTOAXIS_API:-http://127.0.0.1:18080}"
CONNECT="${ONTOAXIS_CONNECT:-http://127.0.0.1:18086}"
TOKEN_URL="${ONTOAXIS_TOKEN_URL:-http://127.0.0.1:18083/realms/ontoaxis/protocol/openid-connect/token}"
USER_NAME="${ONTOAXIS_USER:-admin}"
PASSWORD="${ONTOAXIS_PASSWORD:-admin123}"
CLIENT_ID="${ONTOAXIS_CLIENT_ID:-ontoaxis-web}"
COMPOSE_PROJECT="${ONTOAXIS_COMPOSE_PROJECT:-ontoaxis-local}"
SUCCESS_TIMEOUT="${ONTOAXIS_SUCCESS_TIMEOUT:-900}"
TRINO_URL="${ONTOAXIS_TRINO_URL:-http://127.0.0.1:18081}"

WITH_SUCCESS=0
WITH_OUTAGE=0
SKIP_MATRIX=0
for arg in "$@"; do
  case "$arg" in
    --with-success) WITH_SUCCESS=1 ;;
    --success-only) WITH_SUCCESS=1; SKIP_MATRIX=1 ;;
    --with-outage) WITH_OUTAGE=1 ;;
    -h|--help)
      sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

pass=0
fail=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
run_id="$(date +%s)"

check() { # name expected actual detail
  local name="$1" expected="$2" actual="$3" detail="${4:-}"
  if [ "$expected" = "$actual" ]; then
    pass=$((pass + 1)); printf 'PASS %-48s %s\n' "$name" "$actual"
  else
    fail=$((fail + 1)); printf 'FAIL %-48s want=%s got=%s %s\n' "$name" "$expected" "$actual" "$detail"
  fi
}

json_field() { # file key
  python3 - "$1" "$2" <<'PY'
import json,sys
data=json.load(open(sys.argv[1]))
for key in sys.argv[2].split('.'):
    data=data[key]
print(data)
PY
}

token() {
  curl -sS -X POST "$TOKEN_URL" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    -d "grant_type=password&client_id=${CLIENT_ID}&username=${USER_NAME}&password=${PASSWORD}" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])'
}

_do_call() { # method path [body] [extra-header] -> writes $tmp/body, echoes status
  local method="$1" path="$2" body="${3:-}" extra="${4:-}"
  local args=(-sS -o "$tmp/body" -w '%{http_code}' -X "$method" "${API}${path}"
    -H "Authorization: Bearer ${TOKEN}")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  [ -n "$extra" ] && args+=(-H "$extra")
  curl "${args[@]}"
}

call() { # method path [body] [extra-header]; refreshes the 5-minute Keycloak token once on 401
  local method="$1" path="$2" body="${3:-}" extra="${4:-}" code
  code=$(_do_call "$method" "$path" "$body" "$extra")
  if [ "$code" = "401" ]; then
    TOKEN="$(token)"
    code=$(_do_call "$method" "$path" "$body" "$extra")
  fi
  printf '%s' "$code"
}

create_dataset() { # name -> id
  local name="$1"
  call POST /api/v1/datasets "{\"name\":\"${name}\",\"display_name\":\"${name}\",\"format\":\"iceberg\",\"storage_location\":\"file:///warehouse/oa/${name}\"}" >/dev/null
  json_field "$tmp/body" id
}

create_connector() { # name dataset_id [config-extra-json] [host] -> id
  local name="$1" dataset="$2" extra="${3:-}" host="${4:-source-postgres}"
  local config="{\"host\":\"${host}\",\"port\":5432,\"database\":\"ontoaxis_source\",\"schema\":\"public\",\"tables\":[\"customers\"],\"publication_name\":\"oa_publication\",\"dataset_id\":\"${dataset}\"${extra}}"
  call POST /api/v1/connectors "{\"name\":\"${name}\",\"type\":\"postgres\",\"secret_ref\":\"SOURCEPG\",\"config\":${config}}" >/dev/null
  json_field "$tmp/body" id
}

job_status() { # job id -> status
  call GET "/api/v1/ingestion/jobs/$1" >/dev/null
  json_field "$tmp/body" status
}

trino_sql() { # sql -> first cell of the result (data can arrive before the last poll)
  local sql="$1" resp next cell last=""
  resp=$(curl -sS -X POST "${TRINO_URL}/v1/statement" -H 'X-Trino-User: connector-e2e' -H 'X-Trino-Catalog: iceberg' --data-binary "$sql")
  while :; do
    next=$(printf '%s' "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("nextUri") or "")')
    cell=$(printf '%s' "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); rows=d.get("data") or []; print(rows[0][0] if rows else "")')
    [ -n "$cell" ] && last="$cell"
    if [ -z "$next" ]; then printf '%s' "$last"; return 0; fi
    resp=$(curl -sS "$next")
  done
}

cleanup_inactive_slots() { # 清掉本 profile 留下的非活跃复制槽，避免 max_replication_slots 耗尽
  docker exec "${COMPOSE_PROJECT}-source-postgres-1" psql -U ontoaxis_source -d ontoaxis_source -tAc \
    "select pg_drop_replication_slot(slot_name) from pg_replication_slots where not active" >/dev/null 2>&1 || true
}

wait_job() { # job id expected timeout
  local job="$1" want="$2" timeout="$3" waited=0
  while [ "$waited" -lt "$timeout" ]; do
    if [ "$(job_status "$job")" = "$want" ]; then return 0; fi
    sleep 5; waited=$((waited + 5))
  done
  return 1
}

echo "== preflight =="
code=$(curl -sS -o /dev/null -w '%{http_code}' "$API/readyz")
check "core /readyz" 200 "$code"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$CONNECT/connector-plugins")
check "kafka connect REST" 200 "$code"
TOKEN="$(token)"
[ -n "$TOKEN" ] || { echo "FAIL cannot obtain token" >&2; exit 1; }

if [ "$SKIP_MATRIX" -eq 0 ]; then
echo "== negative matrix =="
code=$(call GET "/api/v1/connectors/11111111-1111-4111-8111-111111111111")
check "unknown connector -> 404" 404 "$code"

invalid_ds=$(create_dataset "e2e-invalid-${run_id}")
invalid_id=$(create_connector "e2e-invalid-${run_id}" "$invalid_ds" ',"bogus_key":"x"')
code=$(call POST "/api/v1/connectors/${invalid_id}/sync")
check "unknown sync config key -> 400" 400 "$code"

paused_ds=$(create_dataset "e2e-paused-${run_id}")
paused_id=$(create_connector "e2e-paused-${run_id}" "$paused_ds")
code=$(call PATCH "/api/v1/connectors/${paused_id}" '{"status":"paused"}')
check "pause connector -> 200" 200 "$code"
code=$(call POST "/api/v1/connectors/${paused_id}/sync")
# 状态由授权策略拒绝（resource_state_denied），HTTP 映射为 403 而不是 409。
check "sync paused connector -> 403" 403 "$code"
code=$(call PATCH "/api/v1/connectors/${paused_id}" '{"status":"active"}')
check "resume connector -> 200" 200 "$code"

error_ds=$(create_dataset "e2e-error-${run_id}")
error_id=$(create_connector "e2e-error-${run_id}" "$error_ds")
code=$(call PATCH "/api/v1/connectors/${error_id}" '{"status":"error"}')
check "mark connector error -> 200" 200 "$code"
code=$(call POST "/api/v1/connectors/${error_id}/sync")
check "sync error-state connector -> 403" 403 "$code"
code=$(call PATCH "/api/v1/connectors/${error_id}" '{"status":"active"}')
check "recover error connector -> 200" 200 "$code"

# 提交失败：源库主机不可达，作业必须 failed 且错误不含凭据
fail_ds=$(create_dataset "e2e-fail-${run_id}")
fail_id=$(create_connector "e2e-fail-${run_id}" "$fail_ds" "" "no-such-host.invalid")
code=$(call POST "/api/v1/connectors/${fail_id}/sync")
check "start sync with bad host -> 201" 201 "$code"
fail_job=$(json_field "$tmp/body" job_id)
if wait_job "$fail_job" failed 180; then
  pass=$((pass + 1)); printf 'PASS %-48s %s\n' "submit failure ends failed" "failed"
else
  fail=$((fail + 1)); printf 'FAIL %-48s status=%s\n' "submit failure ends failed" "$(job_status "$fail_job")"
fi
failure_text=$(python3 - "$tmp/body" <<'PYFAIL'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    data = {}
print(json.dumps(data))
PYFAIL
)
case "$failure_text" in
  *ontoaxis_source_local*|*password*|*Password*) fail=$((fail + 1)); printf 'FAIL %-48s leaked credential\n' "failure text is redacted" ;;
  *) pass=$((pass + 1)); printf 'PASS %-48s redacted\n' "failure text is redacted" ;;
esac

dup_ds=$(create_dataset "e2e-dup-${run_id}")
dup_id=$(create_connector "e2e-dup-${run_id}" "$dup_ds")
call POST "/api/v1/connectors/${dup_id}/sync" >/dev/null
first_job=$(json_field "$tmp/body" job_id)
sleep 1
code=$(call POST "/api/v1/connectors/${dup_id}/sync")
second_job=$(json_field "$tmp/body" job_id)
check "duplicate sync -> 201" 201 "$code"
if [ "$first_job" = "$second_job" ]; then
  pass=$((pass + 1)); printf 'PASS %-48s %s\n' "duplicate sync is idempotent" "same job"
else
  fail=$((fail + 1)); printf 'FAIL %-48s first=%s second=%s\n' "duplicate sync is idempotent" "$first_job" "$second_job"
fi

cancel_ds=$(create_dataset "e2e-cancel-${run_id}")
cancel_id=$(create_connector "e2e-cancel-${run_id}" "$cancel_ds")
code=$(call POST "/api/v1/connectors/${cancel_id}/sync")
cancel_job=$(json_field "$tmp/body" job_id)
sleep 2
code=$(call POST "/api/v1/ingestion/jobs/${cancel_job}/cancel")
check "cancel running job -> 200" 200 "$code"
check "cancelled job is terminal" cancelled "$(job_status "$cancel_job")"

# 单租户 realm 的 tenant_id 是硬编码 claim，无法用第二租户 token 走真实跨租户；
# 这里验证伪造租户头不被信任（fail-closed），真实跨租户隔离由 API/service 单测覆盖。
code=$(call GET "/api/v1/ingestion/jobs/${cancel_job}" "" 'x-ontoaxis-tenant-id: 99999999-9999-7999-8999-999999999999')
check "forged tenant header -> 403" 403 "$code"

echo "== cleanup (best effort) =="
for cid in "$invalid_id" "$paused_id" "$error_id" "$fail_id" "$dup_id" "$cancel_id"; do
  call DELETE "/api/v1/connectors/${cid}" >/dev/null || true
done

fi  # end negative matrix

if [ "$WITH_OUTAGE" -eq 1 ]; then
  echo "== outage matrix =="
  docker stop "${COMPOSE_PROJECT}-kafka-connect-1" >/dev/null
  # 等 REST 真正不可达
  for _ in $(seq 1 30); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$CONNECT/connector-plugins" || true)
    [ "$code" = "000" ] && break
    sleep 1
  done
  out_ds=$(create_dataset "e2e-outage-${run_id}")
  out_id=$(create_connector "e2e-outage-${run_id}" "$out_ds")
  code=$(call POST "/api/v1/connectors/${out_id}/sync")
  check "connect down -> 503" 503 "$code"
  docker start "${COMPOSE_PROJECT}-kafka-connect-1" >/dev/null
  for _ in $(seq 1 90); do
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$CONNECT/connector-plugins" || true)
    [ "$code" = "200" ] && break
    sleep 2
  done
  check "kafka connect recovered" 200 "$code"
fi

if [ "$WITH_SUCCESS" -eq 1 ]; then
  echo "== success path (long) =="
  cleanup_inactive_slots
  ok_ds=$(create_dataset "e2e-success-${run_id}")
  ok_id=$(create_connector "e2e-success-${run_id}" "$ok_ds")
  code=$(call POST "/api/v1/connectors/${ok_id}/sync")
  check "start sync -> 201" 201 "$code"
  ok_job=$(json_field "$tmp/body" job_id)
  if wait_job "$ok_job" succeeded "$SUCCESS_TIMEOUT"; then
    pass=$((pass + 1)); printf 'PASS %-48s %s\n' "sync job succeeded" "succeeded"
    call GET "/api/v1/ingestion/jobs/${ok_job}/runs" >/dev/null
    rows=$(python3 - "$tmp/body" <<'PYROWS'
import json, sys
runs = json.load(open(sys.argv[1])).get("runs") or []
stats = runs[0].get("stats") or {} if runs else {}
print(stats.get("verified_rows", ""))
PYROWS
)
    if [ -n "$rows" ] && [ "$rows" -gt 0 ]; then
      pass=$((pass + 1)); printf 'PASS %-48s %s rows\n' "verified_rows reported" "$rows"
    else
      fail=$((fail + 1)); printf 'FAIL %-48s verified_rows=%s\n' "verified_rows reported" "${rows:-empty}"
    fi
    # 三方行数：源表 = sink total-records = Trino 查询
    compact=$(printf '%s' "$ok_ds" | tr -d '-')
    namespace="ds_${compact:0:8}_public"
    trino_rows=$(trino_sql "select count(*) from iceberg.${namespace}.customers" 2>/dev/null || true)
    check "trino query returns sink rows" "$rows" "${trino_rows:-error}"
    source_rows=$(docker exec "${COMPOSE_PROJECT}-source-postgres-1" \
      psql -U ontoaxis_source -d ontoaxis_source -tAc 'select count(*) from public.customers' 2>/dev/null | tr -d '[:space:]')
    check "source rows equal sink rows" "$rows" "${source_rows:-error}"
    runtime_ref=$(python3 - "$tmp/body" <<'PYOREF'
import json, sys
runs = json.load(open(sys.argv[1])).get("runs") or []
stats = runs[0].get("stats") or {} if runs else {}
print(stats.get("runtime_ref", ""))
PYOREF
)
    printf 'INFO job=%s dataset=%s dataset_rid=ri.ontoaxis.main.dataset.%s runtime_ref=%s\n' \
      "$ok_job" "$ok_ds" "$ok_ds" "$runtime_ref"
  else
    fail=$((fail + 1)); printf 'FAIL %-48s status=%s\n' "sync job succeeded" "$(job_status "$ok_job")"
  fi
fi

echo
echo "RESULT pass=${pass} fail=${fail}"
[ "$fail" -eq 0 ]
