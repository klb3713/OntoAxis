#!/usr/bin/env bash
# delivery-drift-check.sh — summarize live vs desired drift for ArgoCD
# Applications, Deployments and ConfigMaps. Prints a summary and warns; exits 1
# when drift is detected (out-of-sync apps or a Deployment/ConfigMap that ArgoCD
# reports as modified).
#
# Read-only. Usage: scripts/delivery-drift-check.sh [--env kind]
set -euo pipefail

ENV_NAME="kind"
while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV_NAME="${2:-}"; shift 2 ;;
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    -h|--help) echo "usage: $0 [--env kind|staging-like|prod-like]"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v kubectl >/dev/null 2>&1 || die "kubectl not found"
kubectl cluster-info >/dev/null 2>&1 || die "no Kubernetes cluster reachable"

log "drift check for env='${ENV_NAME}'"

drift=0

# 1. ArgoCD Application sync/health drift.
if kubectl get crd applications.argoproj.io >/dev/null 2>&1; then
  apps="$(kubectl -n argocd get applications -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.sync.status}{"\t"}{.status.health.status}{"\n"}{end}' 2>/dev/null || true)"
  if [ -z "$apps" ]; then
    warn "no ArgoCD Applications found in namespace argocd"
  else
    echo "Applications (name / sync / health):"
    while IFS=$'\t' read -r name sync health; do
      [ -n "$name" ] || continue
      printf '  %-24s %-10s %s\n' "$name" "$sync" "$health"
      if [ "$sync" != "Synced" ]; then
        warn "Application $name is $sync (drift)"
        drift=1
      fi
      if [ "$health" = "Degraded" ] || [ "$health" = "Missing" ]; then
        warn "Application $name health is $health"
        drift=1
      fi
    done <<<"$apps"
  fi
else
  warn "ArgoCD Application CRD not found; skipping Application drift"
fi

# 2. Deployment drift summary (unavailable / not up-to-date).
deps="$(kubectl get deploy -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\t"}{.status.replicas}{"\t"}{.status.readyReplicas}{"\t"}{.status.updatedReplicas}{"\n"}{end}' 2>/dev/null || true)"
if [ -n "$deps" ]; then
  echo "Deployments (ns/name replicas ready updated):"
  while IFS=$'\t' read -r ns name replicas ready updated; do
    [ -n "$name" ] || continue
    replicas="${replicas:-0}"; ready="${ready:-0}"; updated="${updated:-0}"
    printf '  %-14s %-32s %s/%s ready, %s updated\n' "$ns" "$name" "$ready" "$replicas" "$updated"
    if [ "$ready" != "$replicas" ] || [ "$updated" != "$replicas" ]; then
      warn "Deployment $ns/$name not converged (ready=$ready replicas=$replicas updated=$updated)"
      drift=1
    fi
  done <<<"$deps"
else
  warn "no Deployments found"
fi

# 3. ConfigMap drift: report ConfigMaps whose resourceVersion changed since apply
#    is not directly available; instead flag ConfigMaps managed by ArgoCD that are
#    OutOfSync at the Application level (already covered). Here we list counts so
#    an operator can eyeball unexpected changes.
cm_count="$(kubectl get configmap -A --no-headers 2>/dev/null | wc -l | tr -d ' ' || echo 0)"
echo "ConfigMaps visible: ${cm_count} (ArgoCD-managed drift is reported via Application sync status above)"

if [ "$drift" -ne 0 ]; then
  warn "DRIFT DETECTED for env='${ENV_NAME}' (exit 1)"
  exit 1
fi
log "no drift detected for env='${ENV_NAME}'"
