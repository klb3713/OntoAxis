#!/usr/bin/env bash
# Fail when chart files/ drift from deploy/observability sources, or when the
# edge generated config is copied instead of referenced as an ArgoCD directory.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/deploy/helm/ontoaxis-observability/files"
declare -a MAP=(
  "prometheus/prometheus.yaml:deploy/observability/prometheus/prometheus.yaml"
  "prometheus/rules.yaml:deploy/observability/prometheus/rules.yaml"
  "alertmanager/alertmanager.yaml:deploy/observability/alertmanager/alertmanager.yaml"
  "otel/otel-collector.yaml:deploy/observability/otel-collector.yaml"
)
fail=0
for pair in "${MAP[@]}"; do
  rel="${pair%%:*}"; src="${pair#*:}"
  [ -f "$DEST/$rel" ] || { echo "MISSING $DEST/$rel (run delivery-sync-observability.sh)"; fail=1; continue; }
  src_sum="sha256:$(shasum -a 256 "$ROOT/$src" | awk '{print $1}')"
  hdr="$(sed -n '1s/^# digest: //p' "$DEST/$rel")"
  if [ "$src_sum" != "$hdr" ]; then echo "DRIFT $rel: source=$src_sum chart=$hdr"; fail=1; fi
  body_sum="sha256:$(tail -n +3 "$DEST/$rel" | shasum -a 256 | awk '{print $1}')"
  if [ "$src_sum" != "$body_sum" ]; then echo "DRIFT body $rel: source=$src_sum body=$body_sum"; fail=1; fi
done
if ! grep -R -q 'deploy/edge/generated' "$ROOT/deploy/argocd" 2>/dev/null; then
  echo "MISSING edge generated ArgoCD Directory reference"; fail=1
fi
[ "$fail" -eq 0 ] && echo "delivery-config-check: OK"
exit "$fail"
