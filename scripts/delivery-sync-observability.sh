#!/usr/bin/env bash
# Copy deploy/observability configs into the observability chart files/ tree,
# each with a content-digest header. Idempotent.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/deploy/helm/ontoaxis-observability/files"
declare -a MAP=(
  "prometheus/prometheus.yaml:deploy/observability/prometheus/prometheus.yaml"
  "prometheus/rules.yaml:deploy/observability/prometheus/rules.yaml"
  "alertmanager/alertmanager.yaml:deploy/observability/alertmanager/alertmanager.yaml"
  "otel/otel-collector.yaml:deploy/observability/otel-collector.yaml"
)
for pair in "${MAP[@]}"; do
  rel="${pair%%:*}"; src="${pair#*:}"
  mkdir -p "$DEST/$(dirname "$rel")"
  digest="$(shasum -a 256 "$ROOT/$src" | awk '{print $1}')"
  {
    printf '# digest: sha256:%s\n' "$digest"
    printf '# source: %s\n' "$src"
    cat "$ROOT/$src"
  } > "$DEST/$rel"
done
echo "synced observability configs to $DEST"
