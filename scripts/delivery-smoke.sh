#!/usr/bin/env bash
# delivery-smoke.sh — read-only smoke for an OntoAxis environment.
#
# Port-forwards the edge Service to localhost:18088 and checks, through the edge
# boundary: core /healthz, core /readyz, a read-only API endpoint, the web root,
# and the search path. Emits a JSON report to stdout and to
# dist/delivery/smoke-<env>.json. Exit 0 only when every check passes.
#
# Usage: scripts/delivery-smoke.sh --env kind|staging-like|prod-like
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ENV_NAME="kind"
while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV_NAME="${2:-}"; shift 2 ;;
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    -h|--help) echo "usage: $0 --env <kind|staging-like|prod-like>"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -n "$ENV_NAME" ] || { echo "--env is required" >&2; exit 2; }

EDGE_PORT="${SMOKE_EDGE_PORT:-18088}"
EDGE_NS="${SMOKE_EDGE_NS:-envoy-gateway-system}"
EDGE_SERVICE="${SMOKE_EDGE_SERVICE:-}"
READONLY_API_PATH="${SMOKE_READONLY_API_PATH:-/api/v1/tenancy/organizations}"
SEARCH_PATH="${SMOKE_SEARCH_PATH:-/search}"
SEARCH_EXPECT="${SMOKE_SEARCH_EXPECT:-404,503}"
SMOKE_TOKEN="${ONTOAXIS_SMOKE_TOKEN:-}"
WAIT_TRIES="${SMOKE_WAIT_TRIES:-50}"

STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
OUT_DIR="$ROOT/dist/delivery"
mkdir -p "$OUT_DIR"
OUT_FILE="$OUT_DIR/smoke-${ENV_NAME}.json"

CHECKS_FILE="$(mktemp)"
PF_LOG="$(mktemp)"
PF_PID=""
cleanup() {
  if [ -n "$PF_PID" ]; then kill "$PF_PID" 2>/dev/null || true; fi
  rm -f "$CHECKS_FILE" "$PF_LOG"
}
trap cleanup EXIT

record() { printf '%s\t%s\t%s\n' "$1" "$2" "$3" >>"$CHECKS_FILE"; }

get_code() {
  local url="$1"; shift
  local c
  c="$(curl -s -o /dev/null -w '%{http_code}' "$@" "$url" 2>/dev/null)" || c=000
  [ -n "$c" ] || c=000
  printf '%s' "$c"
}

BASE="http://127.0.0.1:${EDGE_PORT}"

if ! command -v kubectl >/dev/null 2>&1; then
  record "kubectl" false "kubectl not found on PATH"
elif ! kubectl cluster-info >/dev/null 2>&1; then
  record "cluster" false "no Kubernetes cluster reachable"
fi

discover_service() {
  if [ -n "$EDGE_SERVICE" ]; then printf '%s\n' "$EDGE_SERVICE"; return 0; fi
  local ns svc
  for ns in "$EDGE_NS" oa-system oa-app; do
    svc="$(kubectl -n "$ns" get svc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
      | grep -Ei 'envoy|oa-edge|edge' | head -n1 || true)"
    if [ -n "$svc" ]; then printf '%s/%s\n' "$ns" "$svc"; return 0; fi
  done
  return 1
}

if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
  target="$(discover_service || true)"
  if [ -z "$target" ]; then
    record "edge-service" false "no edge Service found (set SMOKE_EDGE_SERVICE=<ns>/<name>)"
  else
    pf_ns="${target%%/*}"; pf_svc="${target##*/}"
    kubectl -n "$pf_ns" port-forward "svc/$pf_svc" "${EDGE_PORT}:80" >"$PF_LOG" 2>&1 &
    PF_PID=$!
    ready=0
    for _ in $(seq 1 "$WAIT_TRIES"); do
      if curl -s -o /dev/null "http://127.0.0.1:${EDGE_PORT}/healthz" 2>/dev/null; then ready=1; break; fi
      if ! kill -0 "$PF_PID" 2>/dev/null; then break; fi
      sleep 0.3
    done
    if [ "$ready" -eq 1 ]; then
      record "edge-port-forward" true "svc/$pf_svc (${pf_ns}) -> 127.0.0.1:${EDGE_PORT}"
    else
      record "edge-port-forward" false "port-forward to $target did not become ready; see log"
    fi
  fi
fi

code="$(get_code "$BASE/healthz")"
if [ "$code" = "200" ]; then record "core-healthz" true "HTTP 200"; else record "core-healthz" false "HTTP ${code} (want 200)"; fi

code="$(get_code "$BASE/readyz")"
if [ "$code" = "200" ]; then record "core-readyz" true "HTTP 200"; else record "core-readyz" false "HTTP ${code} (want 200)"; fi

if [ -n "$SMOKE_TOKEN" ]; then
  code="$(get_code "$BASE$READONLY_API_PATH" -H "Authorization: Bearer ${SMOKE_TOKEN}")"
  if [ "$code" = "200" ]; then record "read-only-api" true "HTTP 200 (with token)"; else record "read-only-api" false "HTTP ${code} (want 200 with token)"; fi
else
  code="$(get_code "$BASE$READONLY_API_PATH")"
  if [ "$code" = "401" ]; then
    record "read-only-api" true "HTTP 401 (auth enforced; set ONTOAXIS_SMOKE_TOKEN to assert 200)"
  else
    record "read-only-api" false "HTTP ${code} (want 401 without token, or 200 with token)"
  fi
fi

code="$(get_code "$BASE/")"
if [ "$code" = "200" ]; then record "web-root" true "HTTP 200"; else record "web-root" false "HTTP ${code} (want 200)"; fi

code="$(get_code "$BASE$SEARCH_PATH")"
case ",${SEARCH_EXPECT}," in
  *",${code},"*) record "search-path" true "HTTP ${code} (expected one of ${SEARCH_EXPECT})" ;;
  *) record "search-path" false "HTTP ${code} (expected one of ${SEARCH_EXPECT})" ;;
esac

FINISHED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 - "$CHECKS_FILE" "$ENV_NAME" "$STARTED_AT" "$FINISHED_AT" "$OUT_FILE" <<'PY'
import json, sys
checks_path, env, started, finished, out = sys.argv[1:6]
checks = []
with open(checks_path, encoding="utf-8") as fh:
    for line in fh:
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        name = parts[0]
        ok = len(parts) > 1 and parts[1] == "true"
        detail = parts[2] if len(parts) > 2 else ""
        checks.append({"name": name, "ok": ok, "detail": detail})
passed = bool(checks) and all(c["ok"] for c in checks)
doc = {"env": env, "passed": passed, "checks": checks, "started_at": started, "finished_at": finished}
data = json.dumps(doc, indent=2)
with open(out, "w", encoding="utf-8") as fh:
    fh.write(data + "\n")
print(data)
sys.exit(0 if passed else 1)
PY
