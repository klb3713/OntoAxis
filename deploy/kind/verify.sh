#!/usr/bin/env bash
# Verify a bootstrapped OntoAxis cluster: wait for workloads, then run the smoke
# suite. On failure, collect diagnostics with scripts/delivery-diag.sh.
#
# Usage: deploy/kind/verify.sh           # env defaults to kind
#        ENV=staging-like deploy/kind/verify.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

ENV_NAME="${ENV:-kind}"
WAIT_TIMEOUT="${VERIFY_TIMEOUT:-300}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v kubectl >/dev/null 2>&1 || die "missing required tool: kubectl"
kubectl cluster-info >/dev/null 2>&1 || die "no Kubernetes cluster reachable"

log "waiting for namespaces (argocd, oa-app) to exist"
kubectl wait --for=jsonpath='{.status.phase}'=Active --timeout="${WAIT_TIMEOUT}s" namespace/oa-app 2>/dev/null \
  || warn "namespace oa-app not Active within timeout"

log "waiting for core workload in oa-app (best effort)"
kubectl -n oa-app wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" deployment --all 2>/dev/null \
  || warn "oa-app deployments not all Available; smoke will report the real state"

log "running smoke suite for env='${ENV_NAME}'"
SMOKE="${ROOT}/scripts/delivery-smoke.sh"
DIAG="${ROOT}/scripts/delivery-diag.sh"
[ -x "$SMOKE" ] || die "missing smoke script: $SMOKE"

set +e
"$SMOKE" --env "$ENV_NAME"
rc=$?
set -e

if [ "$rc" -ne 0 ]; then
  warn "smoke failed (rc=$rc); collecting diagnostics"
  if [ -x "$DIAG" ]; then
    "$DIAG" || warn "diagnostics collection also failed"
  else
    warn "diagnostics script not found: $DIAG"
  fi
  die "verification failed (smoke rc=$rc)"
fi

log "verification passed"
