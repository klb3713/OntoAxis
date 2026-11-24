#!/usr/bin/env bash
# Read-only delivery checks. `edge` is diagnostic only; `readiness` excludes
# backup/rollback markers, which are required for `full` acceptance.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$ROOT/scripts/delivery-smoke.py" "$@"
