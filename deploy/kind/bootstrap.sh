#!/usr/bin/env bash
# Bootstrap an OntoAxis local cluster (kind or k3d) with the delivery control
# plane: namespaces, ArgoCD, External Secrets Operator (ESO), Envoy Gateway, the
# bootstrap Secret, the AppProject and the app-of-apps root Application.
#
# No values are read from Git. The bootstrap Secret is created from `.env.kind`
# (sourced only if it exists) or from the current environment by
# scripts/delivery-secrets-bootstrap.sh.
#
# Usage:
#   deploy/kind/bootstrap.sh [--skip-argocd] [--skip-eso] [--skip-envoy] [--skip-apps]
#
# Env knobs: ARGOCD_VERSION, ESO_VERSION, ENVOY_GATEWAY_VERSION (used by the
# edge installer), BOOTSTRAP_CONTEXT, WAIT_TIMEOUT.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

ARGOCD_VERSION="${ARGOCD_VERSION:-v2.13.3}"
ESO_VERSION="${ESO_VERSION:-v0.14.4}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"
ROOT_APP_ENV="${ROOT_APP_ENV:-kind}"       # kind|staging-like select root-app.yaml; prod-like selects the manual variant
BOOTSTRAP_SECRET_NS="${BOOTSTRAP_SECRET_NS:-external-secrets}"

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

SKIP_ARGOCD=0; SKIP_ESO=0; SKIP_ENVOY=0; SKIP_APPS=0
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-argocd) SKIP_ARGOCD=1 ;;
    --skip-eso) SKIP_ESO=1 ;;
    --skip-envoy) SKIP_ENVOY=1 ;;
    --skip-apps) SKIP_APPS=1 ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

command -v kubectl >/dev/null 2>&1 || die "missing required tool: kubectl"
command -v helm >/dev/null 2>&1 || warn "helm not found; ESO install will fall back to its static manifest"

if ! kubectl cluster-info >/dev/null 2>&1; then
  die "no Kubernetes cluster reachable (set KUBECONFIG; context='${BOOTSTRAP_CONTEXT:-current}')"
fi
log "cluster: $(kubectl config current-context 2>/dev/null || echo unknown)"

apply_ns() {
  kubectl create namespace "$1" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "namespace '$1' ensured"
}

# --- 1. namespaces ---------------------------------------------------------
for ns in argocd external-secrets oa-system oa-app oa-data oa-observability; do
  apply_ns "$ns"
done

# --- 2. ArgoCD -------------------------------------------------------------
if [ "$SKIP_ARGOCD" -eq 0 ]; then
  log "installing ArgoCD ${ARGOCD_VERSION}"
  kubectl apply -n argocd --server-side --force-conflicts \
    -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
  kubectl -n argocd wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" \
    deployment/argocd-server deployment/argocd-repo-server
else
  log "skip: ArgoCD"
fi

# --- 3. External Secrets Operator -----------------------------------------
if [ "$SKIP_ESO" -eq 0 ]; then
  if command -v helm >/dev/null 2>&1; then
    log "installing ESO ${ESO_VERSION} via helm"
    helm repo add external-secrets https://charts.external-secrets.io >/dev/null 2>&1 || true
    helm repo update external-secrets >/dev/null 2>&1 || true
    helm upgrade --install external-secrets external-secrets/external-secrets \
      --namespace external-secrets --create-namespace \
      --set installCRDs=true \
      --version "${ESO_VERSION#v}" \
      --wait --timeout "${WAIT_TIMEOUT}s"
  else
    log "installing ESO ${ESO_VERSION} via static bundle manifest"
    kubectl apply --server-side --force-conflicts \
      -f "https://github.com/external-secrets/external-secrets/releases/download/${ESO_VERSION}/bundle.yaml"
    kubectl -n external-secrets wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" deployment/external-secrets
  fi
else
  log "skip: ESO"
fi

# --- 4. Envoy Gateway (reuse the existing edge install path) --------------
if [ "$SKIP_ENVOY" -eq 0 ]; then
  log "installing Envoy Gateway via deploy/edge/kind/install-envoy-gateway.sh"
  bash "${ROOT}/deploy/edge/kind/install-envoy-gateway.sh"
else
  log "skip: Envoy Gateway"
fi

# --- 5. bootstrap Secret ---------------------------------------------------
# `.env.kind` is git-ignored; source it only when present.
if [ -f "${ROOT}/.env.kind" ]; then
  log "sourcing ${ROOT}/.env.kind"
  set -a
  # shellcheck disable=SC1091
  . "${ROOT}/.env.kind"
  set +a
else
  log "no .env.kind present; relying on current environment for bootstrap secret"
fi
log "creating bootstrap Secret via scripts/delivery-secrets-bootstrap.sh"
BOOTSTRAP_SECRET_NAMESPACE="$BOOTSTRAP_SECRET_NS" \
  "${ROOT}/scripts/delivery-secrets-bootstrap.sh" --env kind

# --- 6. AppProject + app-of-apps root Application -------------------------
if [ "$SKIP_APPS" -eq 0 ]; then
  log "applying AppProject"
  kubectl apply -f "${ROOT}/deploy/argocd/projects/ontoaxis.yaml"
  if [ "$ROOT_APP_ENV" = "prod-like" ]; then
    log "applying prod-like root Application (manual sync, no automated)"
    kubectl apply -f "${ROOT}/deploy/argocd/root-app.prod-like.yaml"
  else
    log "applying ${ROOT_APP_ENV} root Application (automated prune+selfHeal)"
    kubectl apply -f "${ROOT}/deploy/argocd/root-app.yaml"
  fi
else
  log "skip: AppProject + root Application"
fi

# --- 7. readiness ----------------------------------------------------------
log "waiting for control-plane readiness (timeout ${WAIT_TIMEOUT}s)"
kubectl -n external-secrets wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" deployment --all || warn "ESO deployments not all Available"
kubectl wait --for=condition=Accepted --timeout="${WAIT_TIMEOUT}s" gatewayclass/eg || warn "GatewayClass 'eg' not Accepted"
log "bootstrap complete"
log "next: deploy/kind/verify.sh   (waits, then runs scripts/delivery-smoke.sh --env kind)"
