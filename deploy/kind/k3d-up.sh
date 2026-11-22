#!/usr/bin/env bash
# Create a local k3d cluster for OntoAxis: --agents 2, traefik disabled, a
# local registry attached. Idempotent. Prints the kubeconfig context on success.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

CLUSTER_NAME="${K3D_CLUSTER_NAME:-ontoaxis-kind}"
REGISTRY_HOST="${K3D_REGISTRY_HOST:-ontoaxis-registry.localhost}"
REGISTRY_PORT="${K3D_REGISTRY_PORT:-5000}"
KUBECONFIG_OUT="${K3D_KUBECONFIG_OUT:-${ROOT}/dist/kind/kubeconfig}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1 ($2)"; }

require k3d "https://k3d.io/"
require kubectl "https://kubernetes.io/docs/tasks/tools/"

# --- 1. local registry -----------------------------------------------------
if k3d registry list 2>/dev/null | grep -q "${REGISTRY_HOST%%.*}"; then
  log "k3d registry '$REGISTRY_HOST' already exists"
else
  log "creating k3d registry '$REGISTRY_HOST' on :${REGISTRY_PORT}"
  k3d registry create "$REGISTRY_HOST" --port "$REGISTRY_PORT"
fi

# --- 2. cluster ------------------------------------------------------------
if k3d cluster list 2>/dev/null | awk '{print $1}' | grep -qx "$CLUSTER_NAME"; then
  log "k3d cluster '$CLUSTER_NAME' already exists; reusing"
else
  log "creating k3d cluster '$CLUSTER_NAME' (--agents 2, traefik disabled)"
  k3d cluster create "$CLUSTER_NAME" \
    --agents 2 \
    --registry-use "${REGISTRY_HOST}:5000" \
    --k3s-arg "--disable=traefik@server:0" \
    -p "127.0.0.1:8080:80@loadbalancer" \
    --wait
fi

# --- 3. kubeconfig ---------------------------------------------------------
mkdir -p "$(dirname "$KUBECONFIG_OUT")"
k3d kubeconfig get "$CLUSTER_NAME" >"$KUBECONFIG_OUT"
CONTEXT="k3d-${CLUSTER_NAME}"
log "kubeconfig written to $KUBECONFIG_OUT"
log "context: $CONTEXT"
log "next: export KUBECONFIG=$KUBECONFIG_OUT && kubectl config use-context $CONTEXT"
log "registry push target: localhost:${REGISTRY_PORT}"
