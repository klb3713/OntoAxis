#!/usr/bin/env bash
# Delete the OntoAxis k3d cluster and its registry. Idempotent.
set -euo pipefail

CLUSTER_NAME="${K3D_CLUSTER_NAME:-ontoaxis-kind}"
REGISTRY_HOST="${K3D_REGISTRY_HOST:-ontoaxis-registry.localhost}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v k3d >/dev/null 2>&1 || die "missing required tool: k3d"

if k3d cluster list 2>/dev/null | awk '{print $1}' | grep -qx "$CLUSTER_NAME"; then
  log "deleting k3d cluster '$CLUSTER_NAME'"
  k3d cluster delete "$CLUSTER_NAME"
else
  log "k3d cluster '$CLUSTER_NAME' not found; nothing to delete"
fi

if k3d registry list 2>/dev/null | grep -q "${REGISTRY_HOST%%.*}"; then
  log "deleting k3d registry '$REGISTRY_HOST'"
  k3d registry delete "$REGISTRY_HOST" || true
else
  log "k3d registry '$REGISTRY_HOST' not found"
fi

log "k3d teardown complete"
