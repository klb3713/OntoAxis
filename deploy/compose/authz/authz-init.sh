#!/bin/sh
set -eu
until curl -fsS http://openfga:8080/healthz >/dev/null; do sleep 1; done
store_json=$(curl -fsS -X POST http://openfga:8080/stores -H 'Content-Type: application/json' -d '{"name":"ontoaxis-local"}')
store_id=$(printf '%s' "$store_json" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$store_id" ] || { echo "openfga: store id missing" >&2; exit 1; }
model_json=$(curl -fsS -X POST "http://openfga:8080/stores/$store_id/authorization-models" -H 'Content-Type: application/json' --data-binary @/model.json)
model_id=$(printf '%s' "$model_json" | grep -o '"authorization_model_id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$model_id" ] || { echo "openfga: model id missing" >&2; exit 1; }
cat > /shared/authz.env <<ENV
ONTOAXIS_AUTHZ_OPENFGA_API_URL=http://openfga:8080
ONTOAXIS_AUTHZ_OPENFGA_STORE_ID=$store_id
ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID=$model_id
ONTOAXIS_AUTHZ_AUDIT_SINK=stdout
ONTOAXIS_AUTHZ_SOURCE_COMMIT=local-compose
ENV
echo "authz bootstrap: store=$store_id model=$model_id"
