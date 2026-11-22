#!/usr/bin/env bash
# Install Envoy Gateway into the current kind/k3d cluster. Does not bundle the controller.
set -euo pipefail
VERSION="${ENVOY_GATEWAY_VERSION:-v1.2.6}"
kubectl apply --server-side --force-conflicts -f "https://github.com/envoyproxy/gateway/releases/download/${VERSION}/install.yaml"
kubectl wait --for=condition=Available deployment/envoy-gateway -n envoy-gateway-system --timeout=180s
kubectl apply -f "$(dirname "$0")/gatewayclass.yaml"
kubectl wait --for=condition=Accepted gatewayclass/eg --timeout=120s
kubectl get gatewayclass eg
