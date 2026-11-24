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
REGISTRY_START_TIMEOUT="${KIND_REGISTRY_START_TIMEOUT:-60}"
DOCKER_NETWORK="${KIND_DOCKER_NETWORK:-kind}"
LOCAL_IMAGES_ONLY="${KIND_LOCAL_IMAGES_ONLY:-0}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
require() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1 ($2)"; }

require docker "https://docs.docker.com/get-docker/"
require kind "https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
require kubectl "https://kubernetes.io/docs/tasks/tools/"
require curl "https://curl.se/"
[[ "$REGISTRY_START_TIMEOUT" =~ ^[1-9][0-9]{0,2}$ ]] || die 'KIND_REGISTRY_START_TIMEOUT must be 1–999 seconds'
[[ "$REGISTRY_PORT" =~ ^[1-9][0-9]{0,4}$ && "$REGISTRY_PORT" -le 65535 ]] || die 'invalid KIND_REGISTRY_PORT'
[[ "$REGISTRY_NAME" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die 'invalid KIND_REGISTRY_NAME'
[[ "$CLUSTER_NAME" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die 'invalid KIND_CLUSTER_NAME'
[[ "$DOCKER_NETWORK" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || die 'invalid KIND_DOCKER_NETWORK'
[[ "$LOCAL_IMAGES_ONLY" = 0 || "$LOCAL_IMAGES_ONLY" = 1 ]] || die 'invalid KIND_LOCAL_IMAGES_ONLY'
if [ "$DOCKER_NETWORK" != kind ]; then
  driver=$(docker network inspect "$DOCKER_NETWORK" --format '{{.Driver}}') || die 'the selected isolated Docker network must already exist'
  [ "$driver" = bridge ] || die 'the selected Docker network must be a bridge'
  export KIND_EXPERIMENTAL_DOCKER_NETWORK="$DOCKER_NETWORK"
else
  [ -z "${KIND_EXPERIMENTAL_DOCKER_NETWORK:-}" ] || [ "$KIND_EXPERIMENTAL_DOCKER_NETWORK" = kind ] || \
    die 'set KIND_DOCKER_NETWORK explicitly to use a non-default network'
fi
if [ "$LOCAL_IMAGES_ONLY" = 1 ]; then
  [ -n "${KIND_NODE_IMAGE:-}" ] || die 'local-only mode requires an explicit cached KIND_NODE_IMAGE'
  docker image inspect registry:2 "$KIND_NODE_IMAGE" >/dev/null || die 'required local images are not cached; no pull was attempted'
fi
attached() {
  docker container inspect "$REGISTRY_NAME" --format '{{json .NetworkSettings.Networks}}' | \
    python3 -c 'import json,sys; sys.exit(sys.argv[1] not in (json.load(sys.stdin) or {}))' "$DOCKER_NETWORK"
}
require python3 'https://www.python.org/'

# --- 1. local registry container -------------------------------------------
registry_client_pid=""
stop_registry_client() {
  if [ -n "$registry_client_pid" ]; then
    kill "$registry_client_pid" 2>/dev/null || true
    wait "$registry_client_pid" 2>/dev/null || true
    registry_client_pid=""
  fi
}
trap stop_registry_client EXIT
if docker container inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
  status=$(docker container inspect "$REGISTRY_NAME" --format '{{.State.Status}}')
  [ "$status" = running ] || die "registry container '$REGISTRY_NAME' is $status; inspect it before retrying"
  log "registry container '$REGISTRY_NAME' already running"
else
  log "creating registry container '$REGISTRY_NAME' on 127.0.0.1:${REGISTRY_PORT}"
  registry_args=(-d --restart=always -p "127.0.0.1:${REGISTRY_PORT}:5000" --name "$REGISTRY_NAME")
  if [ "$DOCKER_NETWORK" != kind ]; then registry_args+=(--network "$DOCKER_NETWORK"); fi
  if [ "$LOCAL_IMAGES_ONLY" = 1 ]; then registry_args+=(--pull=never); fi
  docker run "${registry_args[@]}" registry:2 >/dev/null &
  registry_client_pid=$!
  deadline=$((SECONDS + REGISTRY_START_TIMEOUT))
  while kill -0 "$registry_client_pid" 2>/dev/null; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      stop_registry_client
      die "Docker did not start registry '$REGISTRY_NAME' within ${REGISTRY_START_TIMEOUT}s; no kind cluster was created; inspect the container"
    fi
    sleep 1
  done
  rc=0
  wait "$registry_client_pid" || rc=$?
  registry_client_pid=""
  [ "$rc" -eq 0 ] || die "Docker registry start failed (exit $rc); no kind cluster was created"
fi
binding=$(docker container inspect "$REGISTRY_NAME" --format '{{with index .HostConfig.PortBindings "5000/tcp"}}{{range .}}{{.HostIp}}:{{.HostPort}} {{end}}{{end}}')
[ "$binding" = "127.0.0.1:${REGISTRY_PORT} " ] || die "registry '$REGISTRY_NAME' must bind port 5000 only on 127.0.0.1:${REGISTRY_PORT}"
if ! curl --noproxy '*' --fail --silent --show-error --connect-timeout 2 --max-time 4 \
    --retry 2 --retry-connrefused "http://127.0.0.1:${REGISTRY_PORT}/v2/" >/dev/null; then
  die "registry '$REGISTRY_NAME' did not answer the local v2 API; no kind cluster was created"
fi

# --- 2. kind cluster config with containerd mirror -------------------------
KIND_CFG="$(mktemp)"
cleanup() { rm -f "$KIND_CFG"; }
trap cleanup EXIT

cat >"$KIND_CFG" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${CLUSTER_NAME}
networking:
  apiServerAddress: 127.0.0.1
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

clusters=$(kind get clusters) || die 'could not list existing kind clusters'
if grep -Fxq -- "$CLUSTER_NAME" <<<"$clusters"; then
  log "kind cluster '$CLUSTER_NAME' already exists; reusing"
else
  mkdir -p "$(dirname "$KUBECONFIG_OUT")"
  [ ! -e "$KUBECONFIG_OUT" ] || die "kubeconfig output already exists: $KUBECONFIG_OUT"
  cfg_args=(--config "$KIND_CFG" --kubeconfig "$KUBECONFIG_OUT" --wait 180s)
  if [ -n "${KIND_NODE_IMAGE:-}" ]; then
    cfg_args+=(--image "$KIND_NODE_IMAGE")
  fi
  log "creating kind cluster '$CLUSTER_NAME' (1 control-plane + 2 workers)"
  (umask 077; kind create cluster "${cfg_args[@]}")
fi

# --- 3. attach registry to the kind docker network -------------------------
if ! attached; then
  docker network connect "$DOCKER_NETWORK" "$REGISTRY_NAME" || die "could not attach registry to '$DOCKER_NETWORK'"
  attached || die "registry is not attached to '$DOCKER_NETWORK'"
fi
log "registry '$REGISTRY_NAME' attached to docker network '$DOCKER_NETWORK'"

# --- 4. write kubeconfig ---------------------------------------------------
mkdir -p "$(dirname "$KUBECONFIG_OUT")"
kube_tmp=$(mktemp "${KUBECONFIG_OUT}.XXXXXXXX")
if kind get kubeconfig --name "$CLUSTER_NAME" >"$kube_tmp" && [ -s "$kube_tmp" ]; then
  mv -f "$kube_tmp" "$KUBECONFIG_OUT"
else
  if [ -f "$kube_tmp" ] && [ ! -L "$kube_tmp" ]; then : >"$kube_tmp"; fi
  die "could not export kubeconfig for '$CLUSTER_NAME'"
fi
CONTEXT="kind-${CLUSTER_NAME}"
log "kubeconfig written to $KUBECONFIG_OUT"
log "context: $CONTEXT"
log "next: export KUBECONFIG=$KUBECONFIG_OUT && kubectl config use-context $CONTEXT"
log "registry push target: localhost:${REGISTRY_PORT}"
