#!/usr/bin/env bash
# delivery-version-check.sh — report the delivery toolchain and fail when a
# required command is missing.
#
# Required: helm, kubectl, kind, kubeconform
# Optional: k3d, argocd, gitleaks
#
# Prints "MISSING" for any absent command. Exit code is 1 when at least one
# REQUIRED command is absent, 0 otherwise. Optional tools only warn.
#
# Environment notes (this repo workspace): helm/k3d/argocd/kubeconform/gitleaks
# are frequently NOT installed; that is expected for chart authoring and is
# reported explicitly rather than treated as a chart error.

set -uo pipefail

required=(helm kubectl kind kubeconform)
optional=(k3d argocd gitleaks)

missing_required=0
missing_optional=0

# Best-effort version probe; never let a failing version call abort the script.
probe_version() {
  local cmd="$1"
  case "$cmd" in
    helm)        helm version --short 2>/dev/null || true ;;
    kubectl)     kubectl version --client 2>/dev/null | head -n1 || true ;;
    kind)        kind version 2>/dev/null || true ;;
    kubeconform) kubeconform -v 2>/dev/null || true ;;
    k3d)         k3d version 2>/dev/null | head -n1 || true ;;
    argocd)      argocd version --client --short 2>/dev/null | head -n1 || true ;;
    gitleaks)    gitleaks version 2>/dev/null || true ;;
    *)           true ;;
  esac
}

report() {
  local cmd="$1"
  local kind="$2"   # required|optional
  if command -v "$cmd" >/dev/null 2>&1; then
    local ver
    ver="$(probe_version "$cmd")"
    printf '%-14s OK      %s\n' "$cmd" "${ver:-$(command -v "$cmd")}"
  else
    printf '%-14s MISSING\n' "$cmd"
    if [ "$kind" = required ]; then
      missing_required=$((missing_required + 1))
    else
      missing_optional=$((missing_optional + 1))
    fi
  fi
}

echo "OntoAxis delivery toolchain check"
echo "---------------------------------"
for c in "${required[@]}"; do report "$c" required; done
for c in "${optional[@]}"; do report "$c" optional; done
echo "---------------------------------"

if [ "$missing_required" -ne 0 ]; then
  echo "RESULT: FAIL — ${missing_required} required command(s) missing."
  exit 1
fi

if [ "$missing_optional" -ne 0 ]; then
  echo "RESULT: PASS (required present) — ${missing_optional} optional command(s) missing."
else
  echo "RESULT: PASS — all commands present."
fi
exit 0
