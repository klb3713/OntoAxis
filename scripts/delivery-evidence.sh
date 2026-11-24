#!/usr/bin/env bash
# Summarize smoke JSON to a new Markdown fragment. Never overwrite a ledger.
# Usage: delivery-evidence.sh --env kind [--smoke FILE] [--diag DIR] [--out FILE]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_NAME=kind; SMOKE=""; DIAG=""; OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --env|--smoke|--diag|--out)
      if [ "$#" -lt 2 ] || [ -z "$2" ] || [[ "$2" == --* ]]; then echo "missing value for $1" >&2; exit 2; fi
      case "$1" in --env) ENV_NAME="$2" ;; --smoke) SMOKE="$2" ;; --diag) DIAG="$2" ;; --out) OUT="$2" ;; esac
      shift 2 ;;
    -h|--help) echo "usage: $0 --env kind|staging-like [--smoke FILE] [--diag DIR] [--out FILE]"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
case "$ENV_NAME" in kind|staging-like|prod-like) ;; *) echo 'invalid environment' >&2; exit 2 ;; esac
SMOKE="${SMOKE:-$ROOT/dist/delivery/smoke-${ENV_NAME}.json}"
OUT="${OUT:-$ROOT/dist/delivery/evidence-${ENV_NAME}-$(date -u +%Y%m%dT%H%M%SZ).md}"
python3 - "$ENV_NAME" "$SMOKE" "$DIAG" "$OUT" <<'PY'
from datetime import datetime, timezone
import json
from pathlib import Path
import sys

env, smoke, diag, out = sys.argv[1:]
path = Path(smoke)
rows = []
profile = "unknown"
if path.is_file():
    doc = json.loads(path.read_text(encoding="utf-8"))
    if doc.get("environment", doc.get("env")) != env:
        sys.exit("smoke environment does not match evidence environment")
    profile = doc.get("profile", "historic / unspecified")
    for check in doc.get("checks", []):
        result = "passed" if check.get("ok") is True else ("failed" if check.get("ok") is False else "not passed")
        if check.get("result") == "not passed" and check.get("ok") is not True:
            result = "not passed"
        rows.append((check.get("name", "check"), result, check.get("detail", ""), smoke))
    overall = "passed" if doc.get("passed") is True and rows and all(x[1] == "passed" for x in rows) else "not passed"
    if overall == "passed" and profile != "full":
        overall = "passed for this profile only; full acceptance not established"
else:
    rows.append(("smoke-json", "not executed", "report missing", smoke))
    overall = "not executed"
lines = ["# Stage 2 production-delivery evidence fragment", "", f"- environment: `{env}`",
         f"- profile: `{profile}`", f"- overall: {overall}", f"- generated_at: {datetime.now(timezone.utc).isoformat()}",
         "", "| check | result | detail | evidence |", "| --- | --- | --- | --- |"]
for row in rows:
    cells = [str(cell).replace("|", "\\|").replace("\n", " ").replace("\r", " ") for cell in row]
    lines.append("| " + " | ".join(cells) + " |")
if diag:
    lines.append(f"\n- diagnostics: `{diag}`")
p = Path(out)
p.parent.mkdir(parents=True, exist_ok=True)
with p.open("x", encoding="utf-8") as stream:
    stream.write("\n".join(lines) + "\n")
print(out)
PY
