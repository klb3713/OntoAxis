#!/usr/bin/env bash
# connector-runtime 只读健康检查；可选提交一个临时 source connector 做冒烟（默认不做）。
#
# 设计目标：默认只读，不创建/修改任何 connector；需要写操作时必须显式 --submit-smoke。
# 依赖：curl（必需）；python3（用于 JSON 解析，缺失时退化为 grep）；docker compose（可选，
#       用于检查 source-postgres publication；缺失时该检查 SKIP）。
#
# 用法：
#   ./deploy/connector-runtime/scripts/verify-connector-runtime.sh            # 只读检查
#   ./deploy/connector-runtime/scripts/verify-connector-runtime.sh --help
#   ./deploy/connector-runtime/scripts/verify-connector-runtime.sh --submit-smoke
#
# 环境变量：
#   ONTOAXIS_CONNECT_URL                 默认 http://127.0.0.1:18086
#   ONTOAXIS_SOURCE_POSTGRES_HOST_PORT   默认 15434
#   ONTOAXIS_SOURCE_POSTGRES_DB/USER/PASSWORD 默认 ontoaxis_source/ontoaxis_source/ontoaxis_source_local
#
# 退出码：0 = 必需检查全部通过；1 = 有必需检查失败；2 = 参数错误。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CONNECT_URL="${ONTOAXIS_CONNECT_URL:-http://127.0.0.1:18086}"
CONNECT_URL="${CONNECT_URL%/}"
SOURCE_DB="${ONTOAXIS_SOURCE_POSTGRES_DB:-ontoaxis_source}"
SOURCE_USER="${ONTOAXIS_SOURCE_POSTGRES_USER:-ontoaxis_source}"
SOURCE_PASSWORD="${ONTOAXIS_SOURCE_POSTGRES_PASSWORD:-ontoaxis_source_local}"
SOURCE_HOST_PORT="${ONTOAXIS_SOURCE_POSTGRES_HOST_PORT:-15434}"
SMOKE_PREFIX="oa-smoke-$$-$(date +%s)"

PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); printf 'PASS  %s\n' "$*"; }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL  %s\n' "$*" >&2; }
skip() { SKIP=$((SKIP + 1)); printf 'SKIP  %s\n' "$*"; }

usage() {
  sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

SUBMIT_SMOKE=0
while (($# > 0)); do
  case "$1" in
    --submit-smoke) SUBMIT_SMOKE=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

command -v curl >/dev/null 2>&1 || { echo "FAIL: 需要 curl" >&2; exit 1; }
HAVE_PYTHON=0
command -v python3 >/dev/null 2>&1 && HAVE_PYTHON=1

http_get() { # $1 = path; stdout = body; exit code 0 only for 2xx
  local path="$1" code body
  body="$(mktemp)"
  code="$(curl -sS -o "$body" -w '%{http_code}' "${CONNECT_URL}${path}")" || { rm -f "$body"; return 1; }
  if [[ "$code" -ge 200 && "$code" -lt 300 ]]; then
    cat "$body"
    rm -f "$body"
    return 0
  fi
  rm -f "$body"
  return 1
}

# ---------------------------------------------------------------------------
# 1. Connect REST 可达
# ---------------------------------------------------------------------------
if ! plugins_json="$(http_get /connector-plugins)"; then
  fail "Kafka Connect REST 不可达: ${CONNECT_URL}/connector-plugins"
  echo
  echo "提示：确认已启动 profile 且 kafka-connect 处于 healthy："
  echo "  docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime ps"
  echo "SUMMARY pass=${PASS} fail=${FAIL} skip=${SKIP}"
  exit 1
fi
pass "Kafka Connect REST 可达: ${CONNECT_URL}"

# ---------------------------------------------------------------------------
# 2. 必需插件
# ---------------------------------------------------------------------------
check_plugin() { # $1 = plugin class
  if grep -qF "$1" <<<"${plugins_json}"; then
    pass "插件存在: $1"
  else
    fail "插件缺失: $1（见 connect/README.md；Iceberg 插件需先构建镜像）"
  fi
}
check_plugin "io.debezium.connector.postgresql.PostgresConnector"
check_plugin "org.apache.iceberg.connect.IcebergSinkConnector"

# fallback sink 只做提示，不算必需（Iceberg 不可用时的降级选择）
if grep -qF "io.debezium.connector.jdbc.JdbcSinkConnector" <<<"${plugins_json}"; then
  pass "fallback JDBC sink 可用: io.debezium.connector.jdbc.JdbcSinkConnector"
else
  skip "fallback JDBC sink 未出现在 /connector-plugins"
fi

# ---------------------------------------------------------------------------
# 3. 已提交 connectors（只读信息）
# ---------------------------------------------------------------------------
if connectors_json="$(http_get /connectors)"; then
  if [[ "${HAVE_PYTHON}" -eq 1 ]]; then
    count="$(python3 -c 'import json,sys; print(len(json.load(sys.stdin)))' <<<"${connectors_json}")" || count="?"
  else
    count="$(grep -o '"[^"]*"' <<<"${connectors_json}" | wc -l | tr -d ' ')"
  fi
  pass "GET /connectors 成功（当前 connector 数量: ${count}）"
else
  fail "GET /connectors 失败"
fi

# ---------------------------------------------------------------------------
# 4. source-postgres：wal_level 与 publication（需要 docker compose，可选）
# ---------------------------------------------------------------------------
if command -v docker >/dev/null 2>&1; then
  compose=(docker compose -f "${ROOT}/deploy/compose/compose.yaml" -f "${ROOT}/deploy/connector-runtime/compose.connector-runtime.yaml" --profile connector-runtime)
  if "${compose[@]}" ps source-postgres 2>/dev/null | grep -q "running"; then
    if wal_level="$("${compose[@]}" exec -T source-postgres psql -U "${SOURCE_USER}" -d "${SOURCE_DB}" -Atc 'SHOW wal_level' 2>/dev/null)" && [[ "${wal_level}" == "logical" ]]; then
      pass "source-postgres wal_level=logical"
    else
      fail "source-postgres wal_level 不是 logical（或查询失败）"
    fi
    if pub_tables="$("${compose[@]}" exec -T source-postgres psql -U "${SOURCE_USER}" -d "${SOURCE_DB}" -Atc "SELECT tablename FROM pg_publication_tables WHERE pubname = 'oa_publication' ORDER BY tablename" 2>/dev/null)"; then
      if [[ "$(tr -d '[:space:]' <<<"${pub_tables}")" == "customersorders" ]]; then
        pass "publication oa_publication 覆盖 public.customers/public.orders"
      else
        fail "publication oa_publication 表清单不符合预期: ${pub_tables//$'\n'/,}"
      fi
    else
      fail "查询 pg_publication_tables 失败"
    fi
  else
    skip "source-postgres 未运行，跳过 wal_level/publication 检查"
  fi
else
  skip "未安装 docker，跳过 source-postgres 检查"
fi

# ---------------------------------------------------------------------------
# 5. 可选：提交临时 source connector 冒烟（显式 --submit-smoke 才执行）
# ---------------------------------------------------------------------------
cleanup_smoke() {
  if command -v curl >/dev/null 2>&1; then
    curl -sS -o /dev/null -X DELETE "${CONNECT_URL}/connectors/${SMOKE_PREFIX}" || true
    curl -sS -o /dev/null -X DELETE "${CONNECT_URL}/connectors/${SMOKE_PREFIX}-sink" || true
  fi
  echo "已尝试删除临时 connector: ${SMOKE_PREFIX}"
  echo "注意：Debezium 复制槽可能残留在 source-postgres，需手动清理（见 README 清理章节）"
}
if [[ "${SUBMIT_SMOKE}" -eq 1 ]]; then
  trap 'cleanup_smoke' EXIT
  echo "---- 提交临时 source connector（会写入 Kafka Connect 配置、创建复制槽）----"
  smoke_json="$(cat <<EOF
{
  "connector.class": "io.debezium.connector.postgresql.PostgresConnector",
  "tasks.max": "1",
  "plugin.name": "pgoutput",
  "database.hostname": "source-postgres",
  "database.port": "5432",
  "database.user": "${SOURCE_USER}",
  "database.password": "${SOURCE_PASSWORD}",
  "database.dbname": "${SOURCE_DB}",
  "topic.prefix": "${SMOKE_PREFIX}",
  "table.include.list": "public.customers,public.orders",
  "publication.name": "oa_publication",
  "slot.name": "${SMOKE_PREFIX//-/_}_slot",
  "snapshot.mode": "initial"
}
EOF
)"
  code="$(curl -sS -o /tmp/connector-smoke-response.json -w '%{http_code}' \
    -H 'Content-Type: application/json' -X POST \
    --data "${smoke_json}" "${CONNECT_URL}/connectors" || echo 000)"
  if [[ "${code}" -ge 200 && "${code}" -lt 300 ]]; then
    pass "临时 source connector 已创建: ${SMOKE_PREFIX}（HTTP ${code}）"
    status=""
    for _ in $(seq 1 30); do
      status="$(curl -sS "${CONNECT_URL}/connectors/${SMOKE_PREFIX}/status" 2>/dev/null || true)"
      if grep -q '"state":"RUNNING"' <<<"${status}"; then break; fi
      if grep -q '"state":"FAILED"' <<<"${status}"; then break; fi
      sleep 2
    done
    if grep -q '"state":"RUNNING"' <<<"${status}"; then
      pass "临时 source connector 状态 RUNNING"
    else
      fail "临时 source connector 未达到 RUNNING（响应: ${status:-空}）"
    fi
  else
    fail "创建临时 source connector 失败（HTTP ${code}，响应见 /tmp/connector-smoke-response.json）"
  fi
else
  skip "--submit-smoke 未指定：跳过写操作冒烟（默认只读）"
fi

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
echo
echo "SUMMARY pass=${PASS} fail=${FAIL} skip=${SKIP}"
if [[ "${FAIL}" -gt 0 ]]; then
  echo "结果: FAIL（至少一项必需检查未通过）"
  exit 1
fi
echo "结果: PASS（只读检查全部通过；未验证 E2E 数据一致性）"