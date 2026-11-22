#!/usr/bin/env bash
# delivery-secret-scan.sh — fail on plaintext secrets in the working tree.
#
# Prefers `gitleaks` with .gitleaks.toml (which allows secretKeyRef/remoteRef
# reference paths). Falls back to a conservative grep over values/scripts when
# gitleaks is unavailable. Exit 1 on any hit.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCAN_PATH="${1:-$ROOT}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }

if command -v gitleaks >/dev/null 2>&1; then
  log "running gitleaks against ${SCAN_PATH}"
  set +e
  gitleaks detect --no-banner --redact --source "$SCAN_PATH" --config "$ROOT/.gitleaks.toml"
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    log "gitleaks: no leaks found"
    exit 0
  fi
  if [ "$rc" -eq 1 ]; then
    echo "gitleaks: leaks detected (exit 1)" >&2
    exit 1
  fi
  echo "gitleaks failed with exit ${rc} (not a clean scan signal; refusing to pass)" >&2
  exit "$rc"
fi

log "gitleaks not found; using conservative grep fallback"

# Fallback scope: the delivery-owned config + scripts (values, releases, argocd
# manifests, kind scripts, delivery scripts). This deliberately does NOT scan
# pre-existing dev fixtures (deploy/compose, internal/ test data, .github) which
# contain intentional local-only credentials.
FALLBACK_ROOTS=""
for d in deploy/argocd deploy/values deploy/releases deploy/kind scripts; do
  [ -d "$ROOT/$d" ] && FALLBACK_ROOTS="$FALLBACK_ROOTS $ROOT/$d"
done
[ -n "$FALLBACK_ROOTS" ] || { log "no scan roots found"; exit 0; }

KEYVAL='(password|passwd|passphrase|secret|token|api[_-]?key)[[:space:]]*[:=][[:space:]]*["'\'']?[^[:space:]"'\'']{8,}'
DSN='://[^/[:space:]"'\'']+:[^/@[:space:]"'\'']+@'

hits="$(grep -RInE \
  --exclude-dir=.git --exclude-dir=dist --exclude-dir=node_modules \
  --exclude='*.example' --exclude='*.md' --exclude='go.sum' \
  --exclude='delivery-secret-scan.sh' --exclude='delivery-secrets-bootstrap.sh' \
  --exclude='edge-smoke*.sh' \
  -e "$KEYVAL" -e "$DSN" $FALLBACK_ROOTS 2>/dev/null \
  | grep -vE 'secretKeyRef|remoteRef|valueFrom|REDACTED|<redacted>|REPLACE_ME|\$\{|sha256:' \
  | grep -vE '[[:space:]:=][A-Z][A-Z0-9_]{3,}[[:space:]"]*$' \
  | grep -vE ':[[:space:]]*[a-z][a-z0-9.-]+[[:space:]]*$' || true)"

if [ -n "$hits" ]; then
  echo "potential plaintext secrets found:" >&2
  printf '%s\n' "$hits" >&2
  exit 1
fi
log "no plaintext secret patterns found"
