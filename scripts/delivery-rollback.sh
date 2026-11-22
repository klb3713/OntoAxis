#!/usr/bin/env bash
# delivery-rollback.sh — roll an environment back to a previous release manifest.
#
# Reads the previous release manifest, rewrites the current manifest's digests and
# commits, triggers an ArgoCD sync, waits for healthy rollouts (best effort),
# runs the smoke suite, and writes a rollback marker.
#
# Usage:
#   scripts/delivery-rollback.sh --env kind --previous deploy/releases/kind.previous.yaml
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ENV_NAME="kind"
PREVIOUS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV_NAME="${2:-}"; shift 2 ;;
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    --previous) PREVIOUS="${2:-}"; shift 2 ;;
    --previous=*) PREVIOUS="${1#*=}"; shift ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -n "$PREVIOUS" ] || die "--previous <manifest> is required"
[ -f "$PREVIOUS" ] || die "previous manifest not found: $PREVIOUS"

CURRENT="$ROOT/deploy/releases/${ENV_NAME}.yaml"
[ -f "$CURRENT" ] || die "current manifest not found: $CURRENT"

ARGOCD_ROOT_APP="${ARGOCD_ROOT_APP:-ontoaxis-root}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-argocd}"

# 1. Validate previous digests, then copy digest/commit into current manifest.
python3 - "$PREVIOUS" "$CURRENT" <<'PY'
import sys, re
prev, cur_path = sys.argv[1:3]

def parse(path):
    comps, cur, in_comps = [], None, False
    for raw in open(path, encoding="utf-8"):
        line = raw.rstrip("\n"); s = line.strip()
        if not s or s.startswith("#"):
            continue
        if not line.startswith((" ", "\t")):
            in_comps = bool(re.match(r"^components:\s*$", s)); continue
        if in_comps:
            m = re.match(r"^-\s+component:\s*(.+)$", s)
            if m:
                cur = {"component": m.group(1).strip().strip('"').strip("'")}; comps.append(cur); continue
            m = re.match(r"^(digest|commit):\s*(.*)$", s)
            if m and cur is not None:
                cur[m.group(1)] = m.group(2).strip().strip('"').strip("'")
    return comps

prev_comps = {c["component"]: c for c in parse(prev)}
zero = "sha256:" + "0" * 64
for name, c in prev_comps.items():
    d = c.get("digest", "")
    if not re.match(r"^sha256:[0-9a-f]{64}$", d) or d == zero:
        print(f"previous component {name} has no valid digest ('{d}')", file=sys.stderr); sys.exit(2)

lines = open(cur_path, encoding="utf-8").read().splitlines()
cur = None; out = []; seen = set()
for line in lines:
    s = line.strip()
    m = re.match(r"^-\s+component:\s*(.+)$", s)
    if m:
        cur = m.group(1).strip().strip('"').strip("'"); out.append(line); continue
    if cur in prev_comps:
        if s.startswith("digest:"):
            out.append(f"    digest: {prev_comps[cur]['digest']}"); continue
        if s.startswith("commit:"):
            out.append(f"    commit: {prev_comps[cur].get('commit', '')}"); seen.add(cur); continue
    out.append(line)
missing = set(prev_comps) - seen
if missing:
    print(f"current manifest missing rollback components: {sorted(missing)}", file=sys.stderr); sys.exit(2)
open(cur_path, "w", encoding="utf-8").write("\n".join(out) + "\n")
print(f"rewrote {len(seen)} component digests in {cur_path} from previous manifest")
PY

# 2. Trigger a sync (prefer the argocd CLI, else refresh annotation).
if command -v argocd >/dev/null 2>&1; then
  log "argocd CLI found; syncing ${ARGOCD_ROOT_APP}"
  argocd app sync "$ARGOCD_ROOT_APP" --grpc-web || warn "argocd app sync returned non-zero"
elif command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
  log "annotating Application ${ARGOCD_ROOT_APP} to refresh"
  kubectl -n "$ARGOCD_NAMESPACE" annotate application "$ARGOCD_ROOT_APP" \
    "argocd.argoproj.io/refresh=hard" --overwrite || warn "could not annotate Application"
else
  warn "neither argocd nor a reachable cluster available; cannot trigger sync"
fi

# 3. Wait for health (best effort).
if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
  log "waiting for rollouts to become healthy (best effort)"
  for ns in oa-app oa-data oa-system; do
    kubectl -n "$ns" rollout status deployment --timeout=180s 2>/dev/null || warn "deployments in $ns not all healthy"
  done
fi

# 4. Smoke.
SMOKE="$ROOT/scripts/delivery-smoke.sh"
smoke_passed=false
if [ -x "$SMOKE" ]; then
  set +e
  "$SMOKE" --env "$ENV_NAME"
  smoke_rc=$?
  set -e
  [ "$smoke_rc" -eq 0 ] && smoke_passed=true || warn "post-rollback smoke failed (rc=$smoke_rc)"
else
  warn "smoke script not found: $SMOKE"
fi

# 5. Mark the rollback.
TS="$(date -u +%Y%m%dT%H%M%SZ)"
MARKER_DIR="$ROOT/dist/delivery"
mkdir -p "$MARKER_DIR"
MARKER="$MARKER_DIR/rollback-${ENV_NAME}-${TS}.json"
python3 - "$MARKER" "$ENV_NAME" "$PREVIOUS" "$smoke_passed" "${OPERATOR:-unknown}" <<'PY'
import json, sys, datetime
marker, env, previous, smoke_passed, operator = sys.argv[1:6]
doc = {
    "env": env,
    "action": "rollback",
    "previous_manifest": previous,
    "smoke_passed": smoke_passed == "true",
    "operator": operator,
    "rolled_back_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
open(marker, "w", encoding="utf-8").write(json.dumps(doc, indent=2) + "\n")
print(f"rollback marker written: {marker}")
PY

if [ "$smoke_passed" = "true" ]; then
  log "rollback of '${ENV_NAME}' complete and smoke passed"
else
  die "rollback rewrote digests but post-rollback smoke did not pass; investigate before declaring success"
fi
