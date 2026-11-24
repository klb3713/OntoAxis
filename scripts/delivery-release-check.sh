#!/usr/bin/env bash
# delivery-release-check.sh — validate a release manifest, and (when a cluster is
# reachable) confirm the running Pod imageIDs match the pinned digests.
#
# Fails (exit 1) on: missing/invalid fields, placeholder all-zero digests, unset
# commit, or a running-Pod digest mismatch. This intentionally fails until the
# template placeholders are replaced.
#
# Usage: scripts/delivery-release-check.sh --env kind|staging-like|prod-like
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

MANIFEST="${RELEASE_MANIFEST:-$ROOT/deploy/releases/${ENV_NAME}.yaml}"
[ -f "$MANIFEST" ] || { echo "error: release manifest not found: $MANIFEST" >&2; exit 1; }

rc=0
python3 - "$MANIFEST" <<'PY' || rc=$?
import sys, re
path = sys.argv[1]

def unq(v):
    v = v.strip()
    if len(v) >= 2 and v[0] == v[-1] and v[0] in "\"'":
        return v[1:-1]
    return v

top, comps, cur, in_comps = {}, [], None, False
with open(path, encoding="utf-8") as fh:
    for raw in fh:
        line = raw.rstrip("\n")
        s = line.strip()
        if not s or s.startswith("#"):
            continue
        if not line.startswith((" ", "\t")):
            m = re.match(r"^([A-Za-z0-9_]+):\s*(.*)$", s)
            if not m:
                continue
            k, v = m.group(1), m.group(2)
            if k == "components" and v == "":
                in_comps = True
                continue
            in_comps = False
            top[k] = unq(v)
        elif in_comps:
            m = re.match(r"^-\s+([A-Za-z0-9_]+):\s*(.*)$", s)
            if m:
                cur = {m.group(1): unq(m.group(2))}
                comps.append(cur)
                continue
            m = re.match(r"^([A-Za-z0-9_]+):\s*(.*)$", s)
            if m and cur is not None:
                cur[m.group(1)] = unq(m.group(2))
top["components"] = comps

errors = []
if not comps:
    errors.append("no components found")
for k in ("environment", "created_at", "operator"):
    if not top.get(k):
        errors.append(f"missing top-level '{k}'")

zero = "sha256:" + "0" * 64
digest_re = re.compile(r"^sha256:[0-9a-f]{64}$")
for c in comps:
    name = c.get("component", "<unnamed>")
    for f in ("component", "repository", "digest", "commit", "chartVersion"):
        if f not in c:
            errors.append(f"{name}: missing field '{f}'")
    d = c.get("digest", "")
    if not digest_re.match(d):
        errors.append(f"{name}: digest invalid ('{d}')")
    elif d == zero:
        errors.append(f"{name}: digest is placeholder (all zeros) — must be replaced")
    if c.get("commit", "") in ("", "REPLACE_ME"):
        errors.append(f"{name}: commit not set")
    print(f"PASS field-check {name}")

if errors:
    print()
    for e in errors:
        print(f"FAIL {e}", file=sys.stderr)
    sys.exit(1)
print(f"\nrelease manifest OK: {path} ({len(comps)} components)")
PY

if [ "$rc" -ne 0 ]; then
  echo "release check FAILED for env='${ENV_NAME}'" >&2
  exit "$rc"
fi

# Cluster comparison (only if reachable and not disabled).
if [ "${CHECK_CLUSTER:-auto}" != "never" ] && command -v kubectl >/dev/null 2>&1 && kubectl cluster-info >/dev/null 2>&1; then
  echo "cluster reachable: comparing running Pod imageIDs against pinned digests"
  pairs=()
  while IFS= read -r _pair; do pairs+=("$_pair"); done < <(python3 - "$MANIFEST" <<'PY'
import sys, re
path = sys.argv[1]
cur = None
for raw in open(path, encoding="utf-8"):
    line = raw.rstrip("\n"); s = line.strip()
    m = re.match(r"^-\s+component:\s*(.+)$", s)
    if m:
        cur = {"component": m.group(1).strip()}; continue
    if cur is not None:
        m = re.match(r"^(repository|digest):\s*(.+)$", s)
        if m:
            cur[m.group(1)] = m.group(2).strip().strip('"').strip("'")
            if "repository" in cur and "digest" in cur:
                if cur["digest"].startswith("sha256:") and cur["digest"] != "sha256:" + "0" * 64:
                    print(f"{cur['repository']}\t{cur['digest']}")
                cur = None
PY
)
  ids="$(kubectl get pods -A -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.imageID}{"\n"}{end}{end}' 2>/dev/null || true)"
  mismatch=0
  for pair in "${pairs[@]}"; do
    [ -n "$pair" ] || continue
    repo="${pair%%$'\t'*}"
    digest="${pair##*$'\t'}"
    matched="$(printf '%s\n' "$ids" | grep -F "$repo" || true)"
    if [ -z "$matched" ]; then
      echo "WARN ${repo} not currently running in cluster (skipped)"
      continue
    fi
    if printf '%s\n' "$matched" | grep -qF "$digest"; then
      echo "PASS running digest match ${repo}"
    else
      echo "FAIL running digest mismatch ${repo} (want ${digest})" >&2
      mismatch=1
    fi
  done
  if [ "$mismatch" -ne 0 ]; then
    echo "release check FAILED: running Pods do not match the release manifest" >&2
    exit 1
  fi
else
  echo "no cluster reachable (or CHECK_CLUSTER=never); skipped running-Pod comparison"
fi

echo "release check passed for env='${ENV_NAME}'"
