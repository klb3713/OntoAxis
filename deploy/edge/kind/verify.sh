#!/usr/bin/env bash
# Validate the generated Envoy resources locally; apply them server-side when a cluster is reachable.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
MANIFEST="${ROOT}/deploy/edge/generated/envoy-httproute.yaml"

python3 - "$MANIFEST" <<'PY'
import re, sys
path = sys.argv[1]
text = open(path, encoding="utf-8").read()
docs = [d for d in re.split(r"(?m)^---\s*$", text) if d.strip()]
kinds = []
for d in docs:
    m = re.search(r"(?m)^kind:\s*(\S+)\s*$", d)
    if not m:
        raise SystemExit("document missing kind: " + d[:80])
    kinds.append(m.group(1))
expected = ["Gateway", "HTTPRoute", "HTTPRoute", "SecurityPolicy", "BackendTrafficPolicy", "HTTPRouteFilter", "ReferenceGrant"]
if kinds != expected:
    raise SystemExit(f"unexpected resource kinds: {kinds}")
print("structural validation: " + ", ".join(kinds))
PY

if kubectl cluster-info >/dev/null 2>&1; then
  kubectl apply --dry-run=server -f "$MANIFEST"
  kubectl apply -f "$MANIFEST"
  # The generated resources carry an explicit namespace, so the wait must name
  # the same one instead of inheriting the caller's context namespace.
  kubectl -n "${EDGE_NS:-oa-system}" wait --for=condition=Programmed gateway/oa-edge --timeout=120s
else
  echo "no Kubernetes cluster reachable; skipped server-side dry-run/apply"
fi
