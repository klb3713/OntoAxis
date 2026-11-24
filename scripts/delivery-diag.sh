#!/usr/bin/env bash
# delivery-diag.sh — collect Kubernetes diagnostics for a failed delivery check.
#
# Writes dist/delivery/diagnostics-<timestamp>/ with get/describe/events/logs
# output for the OntoAxis namespaces. Secrets are NEVER collected: this script
# does not run `kubectl get secret -o yaml` and redacts token/password-like
# substrings from captured text.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${DIAG_OUT:-$ROOT/dist/delivery/diagnostics-$TS}"
mkdir -p "$OUT"

NAMESPACES="${DIAG_NAMESPACES:-argocd external-secrets oa-system oa-app oa-data oa-observability}"

redact() {
  # Two passes:
  #  1. key=value / key: value forms (tokens, passwords, DSN keys, ...);
  #  2. credentials embedded in URIs, e.g. postgres://user:pw@host - a bare
  #     "postgres://ontoaxis:pg-local-pass@..." line has no `password=` marker,
  #     so the first rule alone would leak the dump's connection string.
  sed -E     -e 's/((token|password|passwd|passphrase|secret|authorization|bearer|access[_-]?key|secret[_-]?key)[A-Za-z0-9_.-]*[[:space:]]*[:=][[:space:]]*)[^[:space:]",}]+/\1<redacted>/Ig' \
    -e 's#([a-zA-Z][a-zA-Z0-9+.-]*://)[^:/@[:space:]]+:[^@[:space:]]+@#\1<redacted>@#g'
}

run_get() {
  local name="$1"; shift
  { "$@" 2>&1 || true; } | redact >"$OUT/$name"
}

{
  echo "# OntoAxis delivery diagnostics"
  echo "# generated_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# context: $(kubectl config current-context 2>/dev/null || echo unknown)"
  echo "# NOTE: Kubernetes Secrets are intentionally NOT collected."
  echo "# files:"
} >"$OUT/SUMMARY.txt"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl not found on PATH" >>"$OUT/SUMMARY.txt"
  echo "$OUT"
  exit 0
fi
if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "no cluster reachable" >>"$OUT/SUMMARY.txt"
  echo "$OUT"
  exit 0
fi

run_get "nodes.txt" kubectl get nodes -o wide
run_get "pods.txt" kubectl get pods -A -o wide
run_get "svc.txt" kubectl get svc -A -o wide
run_get "deploy.txt" kubectl get deploy -A
run_get "sts.txt" kubectl get sts -A
run_get "events.txt" kubectl get events -A --sort-by=.lastTimestamp
run_get "externalsecrets.txt" kubectl get externalsecret -A
run_get "applications.txt" kubectl get applications -n argocd -o wide
run_get "gatewayclass.txt" kubectl get gatewayclass,gateway -A 2>/dev/null || true

: >"$OUT/describe.txt"
: >"$OUT/logs.txt"
for ns in $NAMESPACES; do
  {
    echo "==================== namespace $ns ===================="
    kubectl -n "$ns" get pods -o wide 2>&1 || true
    kubectl -n "$ns" describe pods 2>&1 || true
    kubectl -n "$ns" describe deployments 2>&1 || true
    kubectl -n "$ns" describe statefulsets 2>&1 || true
  } | redact >>"$OUT/describe.txt"

  pods="$(kubectl -n "$ns" get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)"
  for pod in $pods; do
    {
      echo "==================== $ns/$pod ===================="
      kubectl -n "$ns" logs "$pod" --all-containers=true --tail=200 2>&1 || true
    } | redact >>"$OUT/logs.txt"
  done
done

ls -1 "$OUT" >>"$OUT/SUMMARY.txt"
echo "$OUT"
