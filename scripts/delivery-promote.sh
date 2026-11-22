#!/usr/bin/env bash
# delivery-promote.sh — promote verified digests from a source environment's
# release manifest into a target environment's release manifest.
#
# Gates:
#   * the source smoke JSON must exist and have "passed": true;
#   * the source manifest digests must be present and valid sha256;
#   * prod-like targets require --confirm.
# Also runs the render check when scripts/delivery-render-check.sh exists, then
# stages a git commit (never pushes).
#
# Usage:
#   scripts/delivery-promote.sh --from kind --to staging-like [--confirm]
#   scripts/delivery-promote.sh --source-smoke dist/delivery/smoke-kind.json --from kind --to staging-like
#
# Note: chart values under deploy/values are owned by a different scope; this
# script only rewrites release-manifest digest/commit fields. Pass
# --values-file <path> to also rewrite digest fields in a values file if one is
# deliberately in scope.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

FROM_ENV=""
TO_ENV=""
SOURCE_SMOKE=""
VALUES_FILE=""
CONFIRM=0
DO_COMMIT=1

while [ $# -gt 0 ]; do
  case "$1" in
    --from) FROM_ENV="${2:-}"; shift 2 ;;
    --from=*) FROM_ENV="${1#*=}"; shift ;;
    --to) TO_ENV="${2:-}"; shift 2 ;;
    --to=*) TO_ENV="${1#*=}"; shift ;;
    --source-smoke) SOURCE_SMOKE="${2:-}"; shift 2 ;;
    --values-file) VALUES_FILE="${2:-}"; shift 2 ;;
    --confirm) CONFIRM=1; shift ;;
    --no-commit) DO_COMMIT=0; shift ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -n "$TO_ENV" ] || die "--to <target-env> is required"
[ -n "$FROM_ENV" ] || die "--from <source-env> is required"
if [ "$TO_ENV" = "prod-like" ] && [ "$CONFIRM" -ne 1 ]; then
  die "promotion to prod-like requires --confirm"
fi

[ -z "$SOURCE_SMOKE" ] && SOURCE_SMOKE="$ROOT/dist/delivery/smoke-${FROM_ENV}.json"
[ -f "$SOURCE_SMOKE" ] || die "source smoke report not found: $SOURCE_SMOKE (run delivery-smoke.sh first)"

# 1. Source smoke must have passed:true.
pass="$(python3 - "$SOURCE_SMOKE" <<'PY'
import json, sys
try:
    doc = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception as e:
    print(f"invalid smoke JSON: {e}", file=sys.stderr); sys.exit(2)
print("true" if doc.get("passed") is True else "false")
PY
)" || die "could not parse $SOURCE_SMOKE"
[ "$pass" = "true" ] || die "source smoke did not pass (passed!=true in $SOURCE_SMOKE)"

SRC_MANIFEST="$ROOT/deploy/releases/${FROM_ENV}.yaml"
TGT_MANIFEST="$ROOT/deploy/releases/${TO_ENV}.yaml"
[ -f "$SRC_MANIFEST" ] || die "source release manifest not found: $SRC_MANIFEST"
[ -f "$TGT_MANIFEST" ] || die "target release manifest not found: $TGT_MANIFEST"

# 2. Validate source digests present + rewrite target digest/commit.
python3 - "$SRC_MANIFEST" "$TGT_MANIFEST" "$TO_ENV" <<'PY'
import sys, re
src, tgt, to_env = sys.argv[1:4]

def parse(path):
    comps, cur, in_comps = [], None, False
    for raw in open(path, encoding="utf-8"):
        line = raw.rstrip("\n"); s = line.strip()
        if not s or s.startswith("#"):
            continue
        if not line.startswith((" ", "\t")):
            m = re.match(r"^components:\s*$", s)
            if m:
                in_comps = True
            else:
                in_comps = False
            continue
        if in_comps:
            m = re.match(r"^-\s+component:\s*(.+)$", s)
            if m:
                cur = {"component": m.group(1).strip().strip('"').strip("'")}
                comps.append(cur); continue
            m = re.match(r"^(digest|commit|repository|chartVersion):\s*(.*)$", s)
            if m and cur is not None:
                cur[m.group(1)] = m.group(2).strip().strip('"').strip("'")
    return comps

src_comps = {c["component"]: c for c in parse(src)}
if not src_comps:
    print("source manifest has no components", file=sys.stderr); sys.exit(2)
zero = "sha256:" + "0" * 64
for name, c in src_comps.items():
    d = c.get("digest", "")
    if not re.match(r"^sha256:[0-9a-f]{64}$", d) or d == zero:
        print(f"source component {name} has no valid digest ('{d}')", file=sys.stderr); sys.exit(2)

lines = open(tgt, encoding="utf-8").read().splitlines()
cur = None; out = []; seen = set()
for line in lines:
    s = line.strip()
    m = re.match(r"^-\s+component:\s*(.+)$", s)
    if m:
        cur = m.group(1).strip().strip('"').strip("'")
        out.append(line); continue
    if cur in src_comps:
        if s.startswith("digest:"):
            out.append(f"    digest: {src_comps[cur]['digest']}"); continue
        if s.startswith("commit:"):
            out.append(f"    commit: {src_comps[cur].get('commit', '')}"); seen.add(cur); continue
    out.append(line)
missing = set(src_comps) - seen
if missing:
    print(f"target manifest missing promoted components: {sorted(missing)}", file=sys.stderr); sys.exit(2)
open(tgt, "w", encoding="utf-8").write("\n".join(out) + "\n")
print(f"promoted {len(seen)} components from {src} into {tgt}")
PY

# 3. Optional values-file digest rewrite (digest: lines only).
if [ -n "$VALUES_FILE" ]; then
  [ -f "$VALUES_FILE" ] || die "values file not found: $VALUES_FILE"
  log "rewriting digest fields in values file: $VALUES_FILE"
  python3 - "$TGT_MANIFEST" "$VALUES_FILE" <<'PY'
import sys, re
tgt, values = sys.argv[1:3]
digests = {}
cur = None
for raw in open(tgt, encoding="utf-8"):
    s = raw.strip()
    m = re.match(r"^-\s+component:\s*(.+)$", s)
    if m:
        cur = m.group(1).strip().strip('"').strip("'"); digests.setdefault(cur, {}); continue
    m = re.match(r"^digest:\s*(.+)$", s)
    if m and cur:
        digests[cur]["digest"] = m.group(1).strip().strip('"').strip("'")
# Values files may pin per-component digest; rewrite digest: values in order.
all_digests = [v["digest"] for v in digests.values() if v.get("digest")]
lines = open(values, encoding="utf-8").read().splitlines()
out = []; i = 0
for line in lines:
    s = line.strip()
    if s.startswith("digest:") and i < len(all_digests):
        indent = line[:len(line) - len(line.lstrip())]
        out.append(f"{indent}digest: {all_digests[i]}")
        i += 1
    else:
        out.append(line)
open(values, "w", encoding="utf-8").write("\n".join(out) + "\n")
print(f"rewrote {i} digest field(s) in {values}")
PY
fi

# 4. Render check if available (owned by another scope; call only if present).
RENDER="$ROOT/scripts/delivery-render-check.sh"
if [ -x "$RENDER" ]; then
  log "running render check: $RENDER"
  "$RENDER" --env "$TO_ENV"
else
  log "render check script not present; skipping"
fi

# 5. Stage a git commit (never push).
if [ "$DO_COMMIT" -eq 1 ] && command -v git >/dev/null 2>&1; then
  files=("deploy/releases/${TO_ENV}.yaml")
  [ -n "$VALUES_FILE" ] && files+=("$VALUES_FILE")
  if git -C "$ROOT" diff --quiet -- "${files[@]}" 2>/dev/null; then
    log "no manifest changes to commit"
  else
    git -C "$ROOT" add -- "${files[@]}"
    git -C "$ROOT" commit -m "promote(${TO_ENV}): digests from ${FROM_ENV} (smoke passed)" >/dev/null
    log "staged commit for ${TO_ENV} (not pushed). Review with: git -C $ROOT show --stat HEAD"
  fi
else
  log "skipping git commit (--no-commit or git unavailable)"
fi

log "promotion ${FROM_ENV} -> ${TO_ENV} complete; push is a manual, reviewed step"
