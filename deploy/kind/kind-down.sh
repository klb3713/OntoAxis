#!/usr/bin/env bash
# Delete the OntoAxis kind cluster and its local registry container.
# Idempotent: missing cluster/container is not an error.
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ontoaxis-kind}"
REGISTRY_NAME="${KIND_REGISTRY_NAME:-ontoaxis-registry}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v kind >/dev/null 2>&1 || die "missing required tool: kind"
command -v docker >/dev/null 2>&1 || die "missing required tool: docker"

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  log "deleting kind cluster '$CLUSTER_NAME'"
  kind delete cluster --name "$CLUSTER_NAME"
else
  log "kind cluster '$CLUSTER_NAME' not found; nothing to delete"
fi

if docker inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
  log "removing registry container '$REGISTRY_NAME'"
  docker rm -f "$REGISTRY_NAME" >/dev/null
else
  log "registry container '$REGISTRY_NAME' not found"
fi

log "kind teardown complete"
