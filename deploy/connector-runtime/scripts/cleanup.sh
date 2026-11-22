#!/usr/bin/env bash
# connector-runtime 清理脚本：按前缀停止并删除 connector，然后只停掉 profile 自带服务。
#
# 安全约束（重要）：
#   - 任何写操作/删除操作都必须显式传入 --yes；否则只打印计划并退出 1。
#   - 默认不执行 `docker compose down`，因为那会连带停掉 base compose 的 postgres/trino/core
#     等非本 profile 服务；默认只 stop/rm 本 profile 的 4 个服务。
#   - `--down-stack` 才会对整个 project 执行 `down`（需显式确认，影响 base 服务）。
#   - `--purge-volumes` 只在 --yes 下删除本 profile 的 4 个命名卷，不动 base 卷。
#
# 用法：
#   ./deploy/connector-runtime/scripts/cleanup.sh               # 打印计划，拒绝执行
#   ./deploy/connector-runtime/scripts/cleanup.sh --yes         # 删除 connectors + 停本 profile 服务
#   ./deploy/connector-runtime/scripts/cleanup.sh --yes --connectors-only
#   ./deploy/connector-runtime/scripts/cleanup.sh --yes --purge-volumes
#   ./deploy/connector-runtime/scripts/cleanup.sh --yes --down-stack
#
# 环境变量：
#   ONTOAXIS_CONNECT_URL        默认 http://127.0.0.1:18086
#   ONTOAXIS_CONNECTOR_PREFIX   默认 oa-（只删除该前缀的 connector）
#   ONTOAXIS_KEEP_CONNECTORS=1  即使 --yes 也跳过 connector 删除
#
# 退出码：0 = 成功；1 = 未确认/有操作失败；2 = 参数错误。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BASE_COMPOSE="${ROOT}/deploy/compose/compose.yaml"
OVERLAY_COMPOSE="${ROOT}/deploy/connector-runtime/compose.connector-runtime.yaml"
CONNECT_URL="${ONTOAXIS_CONNECT_URL:-http://127.0.0.1:18086}"
CONNECT_URL="${CONNECT_URL%/}"
PREFIX="${ONTOAXIS_CONNECTOR_PREFIX:-oa-}"
PROFILE_SERVICES=(redpanda kafka-connect source-postgres iceberg-warehouse)
PROFILE_VOLUMES=(ontoaxis_redpanda_data ontoaxis_source_postgres_data ontoaxis_iceberg_warehouse ontoaxis_connect_extra_plugins)
PROJECT_NAME="ontoaxis-local" # 与 base compose 顶层 name 一致；卷名会加此前缀

YES=0
CONNECTORS_ONLY=0
PURGE_VOLUMES=0
DOWN_STACK=0

usage() {
  sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while (($# > 0)); do
  case "$1" in
    --yes) YES=1 ;;
    --connectors-only) CONNECTORS_ONLY=1 ;;
    --purge-volumes) PURGE_VOLUMES=1 ;;
    --down-stack) DOWN_STACK=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

compose() { docker compose -f "${BASE_COMPOSE}" -f "${OVERLAY_COMPOSE}" --profile connector-runtime "$@"; }

# ---------------------------------------------------------------------------
# 计划输出（无论是否 --yes 都打印，便于复核）
# ---------------------------------------------------------------------------
echo "== connector-runtime 清理计划 =="
echo "Connect URL       : ${CONNECT_URL}"
echo "connector 前缀    : ${PREFIX} (ONTOAXIS_CONNECTOR_PREFIX 可覆盖)"
if [[ "${ONTOAXIS_KEEP_CONNECTORS:-0}" == "1" ]]; then
  echo "connectors        : 跳过（ONTOAXIS_KEEP_CONNECTORS=1）"
else
  echo "connectors        : 停止并删除以 '${PREFIX}' 开头的 connector"
fi
if [[ "${CONNECTORS_ONLY}" -eq 1 ]]; then
  echo "compose 服务      : 跳过（--connectors-only）"
else
  echo "compose 服务      : stop + rm ${PROFILE_SERVICES[*]}（不 down 整个 project）"
fi
[[ "${DOWN_STACK}" -eq 1 ]] && echo "down-stack        : 将对整个 ontoaxis-local project 执行 down（会影响 base 服务）"
[[ "${PURGE_VOLUMES}" -eq 1 ]] && echo "volumes           : 删除 ${PROFILE_VOLUMES[*]}（带 project 前缀）"
echo

if [[ "${YES}" -eq 0 ]]; then
  echo "拒绝执行：未指定 --yes。以上仅为计划，未做任何修改。" >&2
  exit 1
fi

command -v docker >/dev/null 2>&1 || { echo "FAIL: 需要 docker" >&2; exit 1; }
[[ -f "${BASE_COMPOSE}" && -f "${OVERLAY_COMPOSE}" ]] || { echo "FAIL: 缺少 compose 文件" >&2; exit 1; }

FAILED=0

# ---------------------------------------------------------------------------
# 1. 按前缀停止并删除 connectors（Connect 不可达时只告警，不阻塞 infra 清理）
# ---------------------------------------------------------------------------
if [[ "${ONTOAXIS_KEEP_CONNECTORS:-0}" != "1" ]]; then
  if command -v curl >/dev/null 2>&1; then
    if connectors="$(curl -sS --max-time 5 "${CONNECT_URL}/connectors" 2>/dev/null)"; then
      if command -v python3 >/dev/null 2>&1; then
        names="$(python3 -c 'import json,sys
p = sys.argv[1]
try:
    data = json.load(sys.stdin)
except Exception:
    data = []
for n in data:
    if isinstance(n, str) and n.startswith(p):
        print(n)' "${PREFIX}" <<<"${connectors}")"
      else
        names="$(grep -o '"'"${PREFIX}"'[^"]*"' <<<"${connectors}" | tr -d '"')"
      fi
      if [[ -z "${names}" ]]; then
        echo "connectors: 没有以 '${PREFIX}' 开头的 connector"
      else
        while IFS= read -r name; do
          [[ -z "${name}" ]] && continue
          echo "connector: 停止 ${name}"
          curl -sS -o /dev/null --max-time 10 -X PUT "${CONNECT_URL}/connectors/${name}/stop" || true
          echo "connector: 删除 ${name}"
          if ! curl -sS -o /dev/null --max-time 10 -X DELETE "${CONNECT_URL}/connectors/${name}"; then
            echo "FAIL: 删除 connector ${name} 失败" >&2
            FAILED=1
          fi
        done <<<"${names}"
      fi
      echo "提示：Debezium 的复制槽/replication slot 可能仍残留在 source-postgres。"
      echo "      检查: docker compose -f deploy/compose/compose.yaml exec source-postgres psql -U ontoaxis_source -d ontoaxis_source -Atc \"SELECT slot_name FROM pg_replication_slots\""
      echo "      删除: SELECT pg_drop_replication_slot('<slot_name>');  # 仅确认无活跃消费者后执行"
    else
      echo "WARN: Kafka Connect 不可达（${CONNECT_URL}），跳过 connector 删除"
      echo "      如果容器已停止，其配置与偏移仍在 named volume 中；按需用 --yes --purge-volumes 清理。"
    fi
  else
    echo "WARN: 未安装 curl，跳过 connector 删除" >&2
    FAILED=1
  fi
fi

# ---------------------------------------------------------------------------
# 2. 停止并移除 profile 服务
# ---------------------------------------------------------------------------
if [[ "${CONNECTORS_ONLY}" -eq 0 ]]; then
  if [[ "${DOWN_STACK}" -eq 1 ]]; then
    echo "compose: down 整个 project（含 base 服务）"
    compose down || FAILED=1
  else
    echo "compose: stop ${PROFILE_SERVICES[*]}"
    compose stop "${PROFILE_SERVICES[@]}" || echo "WARN: stop 返回非 0（服务可能本就未运行）"
    echo "compose: rm -f ${PROFILE_SERVICES[*]}"
    compose rm -f "${PROFILE_SERVICES[@]}" || echo "WARN: rm 返回非 0（容器可能本就已移除）"
  fi
fi

# ---------------------------------------------------------------------------
# 3. 可选删除 profile 命名卷（显式 --purge-volumes）
# ---------------------------------------------------------------------------
if [[ "${PURGE_VOLUMES}" -eq 1 ]]; then
  for vol in "${PROFILE_VOLUMES[@]}"; do
    full="${PROJECT_NAME}_${vol}"
    if docker volume inspect "${full}" >/dev/null 2>&1; then
      echo "volume: 删除 ${full}"
      docker volume rm "${full}" || FAILED=1
    else
      echo "volume: 不存在，跳过 ${full}"
    fi
  done
  echo "注意：base postgres 的 iceberg_catalog schema 与业务库卷不在本脚本清理范围（由 ops 决定）。"
fi

echo
if [[ "${FAILED}" -ne 0 ]]; then
  echo "结果: 部分操作失败（见上方 FAIL/WARN）"
  exit 1
fi
echo "结果: 清理完成（connector + profile 服务；未触碰 base 服务，除非使用了 --down-stack）"