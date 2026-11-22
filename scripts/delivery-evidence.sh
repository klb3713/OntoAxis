#!/usr/bin/env bash
# delivery-evidence.sh — render a Markdown evidence table from smoke JSON + diag.
# Usage: scripts/delivery-evidence.sh --env kind [--smoke dist/delivery/smoke-kind.json] [--diag DIR] [--out FILE]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_NAME="kind"; SMOKE=""; DIAG=""; OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV_NAME="${2:-}"; shift 2 ;;
    --smoke) SMOKE="${2:-}"; shift 2 ;;
    --diag) DIAG="${2:-}"; shift 2 ;;
    --out) OUT="${2:-}"; shift 2 ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -z "$SMOKE" ] && SMOKE="$ROOT/dist/delivery/smoke-${ENV_NAME}.json"
[ -z "$OUT" ] && OUT="$ROOT/docs/stages/evidence/stage-2-production-delivery.md"
python3 - "$ENV_NAME" "$SMOKE" "$DIAG" "$OUT" <<'PY'
import json, os, sys, datetime
env, smoke, diag, out = sys.argv[1:5]
rows = []
if os.path.isfile(smoke):
    doc = json.load(open(smoke, encoding="utf-8"))
    for check in doc.get("checks", []):
        rows.append((check.get("name","check"), check.get("result","not passed"), check.get("detail",""), smoke))
    overall = doc.get("overall","not passed")
else:
    rows.append(("smoke-json", "not executed", f"missing {smoke}", ""))
    overall = "not executed"
os.makedirs(os.path.dirname(out), exist_ok=True)
lines = ["# Stage 2 production-delivery evidence", "", f"- environment: `{env}`", f"- overall: `{overall}`", f"- generated_at: {datetime.datetime.now(datetime.timezone.utc).isoformat()}", "", "| check | result | detail | evidence |", "| --- | --- | --- | --- |"]
for name, result, detail, ev in rows:
    lines.append(f"| {name} | {result} | {detail} | `{ev}` |")
if diag:
    lines.append(f"\n- diagnostics: `{diag}`")
open(out, "w", encoding="utf-8").write("\n".join(lines) + "\n")
print(out)
PY
