#!/usr/bin/env bash
# Create a local kind cluster for OntoAxis with a local container registry.
#
# Topology: 1 control-plane + 2 workers. A `registry:2` container doubles as the
# in-cluster image source so `delivery-images.sh` can push to localhost:5000 and
# kind nodes can pull it via a containerd registry mirror.
#
# Idempotent: re-running against an existing cluster/registry is a no-op.
# Prints the kubeconfig context on success.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ontoaxis-kind}"
REGISTRY_NAME="${KIND_REGISTRY_NAME:-ontoaxis-registry}"
REGISTRY_PORT="${KIND_REGISTRY_PORT:-5000}"
KUBECONFIG_OUT="${KIND_KUBECONFIG_OUT:-${ROOT}/dist/kind/kubeconfig}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1 ($2)"; }

require docker "https://docs.docker.com/get-docker/"
require kind "https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
require kubectl "https://kubernetes.io/docs/tasks/tools/"

# --- 1. local registry container -------------------------------------------
if docker inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
  log "registry container '$REGISTRY_NAME' already exists"
else
  log "creating registry container '$REGISTRY_NAME' on 127.0.0.1:${REGISTRY_PORT}"
  docker run -d --restart=always \
    -p "127.0.0.1:${REGISTRY_PORT}:5000" \
    --name "$REGISTRY_NAME" registry:2 >/dev/null
fi

# --- 2. kind cluster config with containerd mirror -------------------------
KIND_CFG="$(mktemp)"
cleanup() { rm -f "$KIND_CFG"; }
trap cleanup EXIT

cat >"$KIND_CFG" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${CLUSTER_NAME}
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REGISTRY_PORT}"]
      endpoint = ["http://${REGISTRY_NAME}:5000"]
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |-
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
  - role: worker
  - role: worker
EOF

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  log "kind cluster '$CLUSTER_NAME' already exists; reusing"
else
  log "creating kind cluster '$CLUSTER_NAME' (1 control-plane + 2 workers)"
  kind create cluster --config "$KIND_CFG" --wait 180s
fi

# --- 3. attach registry to the kind docker network -------------------------
if docker network inspect kind >/dev/null 2>&1; then
  # `network connect` errors if already attached; treat that as success.
  docker network connect kind "$REGISTRY_NAME" >/dev/null 2>&1 || true
  log "registry '$REGISTRY_NAME' attached to docker network 'kind'"
else
  log "warning: docker network 'kind' not found; registry mirror may not resolve"
fi

# --- 4. write kubeconfig ---------------------------------------------------
mkdir -p "$(dirname "$KUBECONFIG_OUT")"
kind get kubeconfig --name "$CLUSTER_NAME" >"$KUBECONFIG_OUT"
CONTEXT="kind-${CLUSTER_NAME}"
log "kubeconfig written to $KUBECONFIG_OUT"
log "context: $CONTEXT"
log "next: export KUBECONFIG=$KUBECONFIG_OUT && kubectl config use-context $CONTEXT"
log "registry push target: localhost:${REGISTRY_PORT}"
