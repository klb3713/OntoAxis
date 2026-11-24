#!/usr/bin/env bash
# Offline driver: substitute Docker, kind and the registry response.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work=$(mktemp -d)
cleanup() {
  if [[ "$work" = /* && "$work" != / && -f "$work/.kind-up-test" ]]; then rm -rf -- "$work"; fi
}
trap cleanup EXIT
: >"$work/.kind-up-test"
mkdir "$work/bin"
export PATH="$work/bin:$PATH"
export CALLS="$work/calls" KIND_KUBECONFIG_OUT="$work/kubeconfig" KIND_REGISTRY_START_TIMEOUT=1
export KIND_CLUSTER_NAME=ontoaxis-kind KIND_REGISTRY_NAME=ontoaxis-registry KIND_REGISTRY_PORT=5000
printf 'keep current kubeconfig\n' >"$work/current-context"
export KUBECONFIG="$work/current-context"
unset KIND_DOCKER_NETWORK KIND_LOCAL_IMAGES_ONLY KIND_EXPERIMENTAL_DOCKER_NETWORK
cat >"$work/bin/docker" <<'MOCK'
#!/usr/bin/env bash
case "$1:$2:${4:-}" in
  container:inspect:) [[ "$SCENARIO" = created || "$SCENARIO" = bad-binding || "$SCENARIO" = reuse ]] ;;
  container:inspect:--format)
    if [[ "$5" = *NetworkSettings* ]]; then
      printf '{"%s":{}}\n' "${KIND_DOCKER_NETWORK:-kind}"
    elif [[ "$5" = *PortBindings* ]]; then
      if [[ "$SCENARIO" = bad-binding ]]; then printf '0.0.0.0:5000 \n'; else printf '127.0.0.1:5000 \n'; fi
    else
      if [[ "$SCENARIO" = created ]]; then echo created; else echo running; fi
    fi ;;
  run:*)
    echo docker-run >>"$CALLS"
    if [[ "$SCENARIO" = start-timeout ]]; then exec sleep 20; fi
    if [[ "$SCENARIO" = local-only ]]; then
      [[ " $* " = *' --pull=never '* && " $* " = *' --network rehearsal '* ]] || exit 84
    fi ;;
  image:inspect:*) [[ "$SCENARIO" != uncached ]] ;;
  network:inspect:*) printf 'bridge\n' ;;
  network:connect:*) exit 0 ;;
  *) exit 87 ;;
esac
MOCK
cat >"$work/bin/kind" <<'MOCK'
#!/usr/bin/env bash
case "$1:$2" in
  get:clusters) if [[ "$SCENARIO" = reuse ]]; then echo ontoaxis-kind; fi ;;
  create:cluster)
    echo kind-create >>"$CALLS"
    dest=""
    while [[ $# -gt 0 ]]; do
      if [[ "$1" = --kubeconfig ]]; then dest="$2"; shift; fi
      if [[ "$1" = --config ]]; then grep -q 'apiServerAddress: 127.0.0.1' "$2" || exit 81; shift; fi
      shift
    done
    [[ "$dest" = "$KIND_KUBECONFIG_OUT" ]] || exit 82
    printf 'created isolated kubeconfig\n' >"$dest" ;;
  get:kubeconfig) printf 'isolated kubeconfig\n' ;;
  *) exit 88 ;;
esac
MOCK
cat >"$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
[[ "$SCENARIO" != bad-api ]]
MOCK
printf '#!/usr/bin/env bash\nexit 89\n' >"$work/bin/kubectl"
chmod +x "$work/bin/"*
assert_failure() {
  export SCENARIO=$1
  : >"$CALLS"
  if bash "$ROOT/deploy/kind/kind-up.sh" >"$work/result" 2>&1; then
    echo "unexpected success: $SCENARIO" >&2; return 1
  fi
  grep -Fq -- "$2" "$work/result"
  if grep -Fxq kind-create "$CALLS"; then
    echo "created kind after registry failure: $SCENARIO" >&2; return 1
  fi
  [[ ! -e "$KIND_KUBECONFIG_OUT" ]]
}
assert_failure created 'is created; inspect it before retrying'
assert_failure start-timeout 'did not start registry'
assert_failure bad-binding 'must bind port 5000 only on 127.0.0.1'
assert_failure bad-api 'did not answer the local v2 API'
export SCENARIO=ready
bash "$ROOT/deploy/kind/kind-up.sh" >"$work/result" 2>&1
grep -Fxq kind-create "$CALLS"
grep -Fxq 'isolated kubeconfig' "$KIND_KUBECONFIG_OUT"
grep -Fxq 'keep current kubeconfig' "$KUBECONFIG"
export SCENARIO=reuse
: >"$CALLS"
bash "$ROOT/deploy/kind/kind-up.sh" >"$work/result" 2>&1
[[ ! -s "$CALLS" ]]
export KIND_DOCKER_NETWORK=rehearsal KIND_LOCAL_IMAGES_ONLY=1 KIND_NODE_IMAGE=kindest/node:v1.32.0
export KIND_KUBECONFIG_OUT="$work/offline.kubeconfig"
assert_failure uncached 'required local images are not cached'
[[ ! -s "$CALLS" ]]
export SCENARIO=local-only
bash "$ROOT/deploy/kind/kind-up.sh" >"$work/result" 2>&1
grep -Fxq kind-create "$CALLS"
printf 'kind-up offline: 8/8 passed (stuck, timeout, unsafe bind, absent API, isolated output, idempotent reuse, cache rejection, isolated network)\n'
