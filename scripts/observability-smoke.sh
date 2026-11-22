#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT}/deploy/compose/compose.yaml"
CORE_URL="${CORE_URL:-http://127.0.0.1:18080}"
PROM_URL="${PROM_URL:-http://127.0.0.1:19091}"
JAEGER_URL="${JAEGER_URL:-http://127.0.0.1:16686}"
PERSES_URL="${PERSES_URL:-http://127.0.0.1:18084}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 2
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 2
fi

export ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT="${ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT:-http://otel-collector:4318}"

echo "starting observability profile"
docker compose -f "${COMPOSE_FILE}" --profile observability up -d --build postgres otel-collector jaeger prometheus alertmanager perses ontoaxis-core

wait_http() {
  local url="$1"
  local name="$2"
  for _ in $(seq 1 60); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for ${name}: ${url}" >&2
  docker compose -f "${COMPOSE_FILE}" --profile observability logs --tail=80 ontoaxis-core otel-collector jaeger prometheus >&2 || true
  return 1
}

wait_http "${CORE_URL}/healthz" "core health"
wait_http "${PROM_URL}/-/ready" "prometheus"
wait_http "${JAEGER_URL}/api/services" "jaeger"
wait_http "${PERSES_URL}/api/v1/projects" "perses"

echo "importing Perses dashboard"
python3 - "${PERSES_URL}" "${ROOT}/deploy/observability/perses/globaldatasources/prometheus.json" "${ROOT}/deploy/observability/perses/dashboards/ontoaxis-service-overview.json" <<'PYINC'
import json, sys, urllib.error, urllib.request
base, datasource_path, dashboard_path = sys.argv[1], sys.argv[2], sys.argv[3]

def request(path, payload, method):
    req = urllib.request.Request(
        f"{base}{path}",
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json"},
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            assert resp.status in (200, 201)
    except urllib.error.HTTPError as exc:
        if method == "POST" and exc.code == 409:
            return
        raise

request("/api/v1/projects", {"kind": "Project", "metadata": {"name": "ontoaxis"}, "spec": {"display": {"name": "OntoAxis"}}}, "POST")
with open(datasource_path, encoding="utf-8") as fh:
    datasource = json.load(fh)
request("/api/v1/globaldatasources/prometheus", datasource, "PUT")
with open(dashboard_path, encoding="utf-8") as fh:
    dashboard = json.load(fh)
request("/api/v1/projects/ontoaxis/dashboards", dashboard, "POST")
PYINC

echo "generating traffic"
for _ in $(seq 1 5); do
  curl -fsS "${CORE_URL}/healthz" >/dev/null
done

echo "waiting for Prometheus samples"
for _ in $(seq 1 30); do
  python3 - "${PROM_URL}" <<'PY' && break || true
import json, sys, urllib.parse, urllib.request
base = sys.argv[1]
q = urllib.parse.quote('ontoaxis_http_requests_total')
with urllib.request.urlopen(f"{base}/api/v1/query?query={q}", timeout=5) as resp:
    data = json.load(resp)
assert data.get("status") == "success"
assert data.get("data", {}).get("result"), "no HTTP metric series yet"
PY
  sleep 2
done

echo "waiting for Jaeger trace"
for _ in $(seq 1 30); do
  python3 - "${JAEGER_URL}" <<'PY' && break || true
import json, sys, urllib.request
base = sys.argv[1]
with urllib.request.urlopen(f"{base}/api/traces?service=ontoaxis-core&limit=1", timeout=5) as resp:
    data = json.load(resp)
assert data.get("data"), "no trace found for ontoaxis-core"
PY
  sleep 2
done

python3 - "${PROM_URL}" "${JAEGER_URL}" <<'PY'
import json, sys, urllib.parse, urllib.request
prom, jaeger = sys.argv[1], sys.argv[2]

def get(url):
    with urllib.request.urlopen(url, timeout=5) as resp:
        return json.load(resp)

metrics = get(f"{prom}/api/v1/query?query={urllib.parse.quote('ontoaxis_http_requests_total')}")
assert metrics["status"] == "success" and metrics["data"]["result"], "HTTP metric missing"
traces = get(f"{jaeger}/api/traces?service=ontoaxis-core&limit=1")
assert traces.get("data"), "trace missing"
print(f"PASS: {len(metrics['data']['result'])} HTTP metric series, {len(traces['data'])} trace(s), Perses dashboard imported")
PY
