#!/usr/bin/env bash
# Reproducible local HTTP load baseline for Stage 2 observability (E2E-09 / 7.4).
#
# This is a synthetic, local baseline only: it measures the local Compose stack
# and must NOT be reported as production capacity or a production SLO.
#
# Usage:
#   TARGET_URL=http://127.0.0.1:18080/healthz REQUESTS=300 CONCURRENCY=20 \
#     scripts/observability-load-baseline.sh
#   TARGET_URL=http://127.0.0.1:18080/api/v1/projects BEARER_TOKEN=... OUT=... \
#     scripts/observability-load-baseline.sh
set -euo pipefail

URL="${TARGET_URL:-http://127.0.0.1:18080/healthz}"
REQUESTS="${REQUESTS:-300}"
CONCURRENCY="${CONCURRENCY:-20}"
OUT="${OUT:-}"
TOKEN="${BEARER_TOKEN:-}"

python3 - "${URL}" "${REQUESTS}" "${CONCURRENCY}" "${OUT}" "${TOKEN}" <<'PY'
import concurrent.futures
import json
import os
import statistics
import sys
import time
import urllib.error
import urllib.request

url, requests_s, concurrency_s, out_path, token = sys.argv[1:6]
requests_n = int(requests_s)
concurrency = int(concurrency_s)
if requests_n <= 0 or concurrency <= 0:
    raise SystemExit("REQUESTS and CONCURRENCY must be positive")

headers = {}
if token:
    headers["Authorization"] = f"Bearer {token}"

def one(_):
    started = time.perf_counter()
    status = 0
    error = ""
    try:
        req = urllib.request.Request(url, headers=headers, method="GET")
        with urllib.request.urlopen(req, timeout=15) as resp:
            status = resp.status
            resp.read(1024)
    except urllib.error.HTTPError as exc:
        status = exc.code
    except Exception as exc:  # noqa: BLE001 - record infrastructure failures
        error = f"{type(exc).__name__}: {exc}"
    return status, (time.perf_counter() - started) * 1000.0, error

started = time.perf_counter()
with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
    results = list(pool.map(one, range(requests_n)))
elapsed = time.perf_counter() - started

latencies = sorted(r[1] for r in results)
statuses = {}
errors = []
for status, _, error in results:
    statuses[str(status)] = statuses.get(str(status), 0) + 1
    if error:
        errors.append(error)

def pct(p):
    if not latencies:
        return 0.0
    idx = min(len(latencies) - 1, int(round((p / 100.0) * (len(latencies) - 1))))
    return round(latencies[idx], 3)

ok = sum(1 for s, _, _ in results if 200 <= s < 400)
report = {
    "kind": "local_synthetic_load_baseline",
    "date": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
    "target": url,
    "requests": requests_n,
    "concurrency": concurrency,
    "elapsed_seconds": round(elapsed, 3),
    "requests_per_second": round(requests_n / elapsed, 2) if elapsed > 0 else 0,
    "success": ok,
    "failure": requests_n - ok,
    "error_rate": round((requests_n - ok) / requests_n, 4),
    "status_counts": statuses,
    "latency_ms": {
        "min": round(latencies[0], 3) if latencies else 0,
        "p50": pct(50),
        "p95": pct(95),
        "p99": pct(99),
        "max": round(latencies[-1], 3) if latencies else 0,
        "mean": round(statistics.fmean(latencies), 3) if latencies else 0,
    },
    "sample_errors": errors[:5],
    "caveat": "Local synthetic baseline; not production capacity or production SLO.",
}
text = json.dumps(report, ensure_ascii=False, indent=2)
print(text)
if out_path:
    os.makedirs(os.path.dirname(os.path.abspath(out_path)), exist_ok=True)
    with open(out_path, "w", encoding="utf-8") as fh:
        fh.write(text + "\n")
PY
