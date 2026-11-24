#!/usr/bin/env bash
# Offline: synthetic Git objects only; never calls a live cluster or registry.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
cleanup() {
  if [[ "$tmp" == /* && -d "$tmp/fixture" && -f "$tmp/.ontoaxis-test" ]]; then
    rm -rf -- "$tmp"
  fi
}
trap cleanup EXIT
mkdir "$tmp/fixture"
touch "$tmp/.ontoaxis-test"
fixture="$tmp/fixture"
# Copy only versioned, reviewed template paths, never the working tree's secrets.
git -C "$ROOT" archive HEAD -- deploy/argocd/apps deploy/argocd/manifests/namespaces \
  deploy/argocd/secrets/kind deploy/helm deploy/values deploy/edge/generated | tar -x -C "$fixture"
git -C "$fixture" init -q
git -C "$fixture" add deploy
git -C "$fixture" -c user.name=fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false \
  -c core.hooksPath=/dev/null commit -qm 'isolated content fixture'
content="$(git -C "$fixture" rev-parse HEAD)"
# A working-tree change must not affect the render.
printf 'invalid working tree\n' > "$fixture/deploy/argocd/apps/40-app.yaml"
render() { python3 "$ROOT/deploy/kind/render-gitops-root.py" --checkout "$fixture" --env kind-small "$@"; }
render --content-revision "$content" --out "$tmp/rendered"
if render --content-revision "$content" --out "$tmp/rendered"; then
  echo 'existing output unexpectedly accepted' >&2; exit 1
fi
if render --content-revision HEAD --out "$tmp/invalid"; then
  echo 'floating revision unexpectedly accepted' >&2; exit 1
fi
mkdir -p "$tmp/no-kube"
printf '#!/bin/sh\nexit 98\n' > "$tmp/no-kube/kubectl"
chmod 700 "$tmp/no-kube/kubectl"
cp "$tmp/rendered/deploy/argocd/apps/"*.yaml "$fixture/deploy/argocd/apps/"
git -C "$fixture" add deploy/argocd/apps
git -C "$fixture" -c user.name=fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false \
  -c core.hooksPath=/dev/null commit -qm 'isolated root fixture'
root_rev="$(git -C "$fixture" rev-parse HEAD)"
PATH="$tmp/no-kube:$PATH" ROOT_APP_ENV=kind-small GITOPS_CHECKOUT="$fixture" \
  GITOPS_ROOT_REVISION="$root_rev" GITOPS_CONTENT_REVISION="$content" \
  bash "$ROOT/deploy/kind/bootstrap.sh" --check-source
printf '%s\n' 'GitOps root rendering and bootstrap source agreement passed (offline)'
