#!/usr/bin/env bash
# delivery-secrets-bootstrap.sh — create/update the git-ignored bootstrap Secret
# `ontoaxis-bootstrap` in the `external-secrets` namespace from `.env.kind` or
# the current environment.
#
# Never prints secret values. Lists missing key NAMES and exits 1 if any required
# key is unset. Idempotent (kubectl apply).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ENV_NAME="kind"
SECRET_NAME="${BOOTSTRAP_SECRET_NAME:-ontoaxis-bootstrap}"
SECRET_NS="${BOOTSTRAP_SECRET_NAMESPACE:-external-secrets}"

while [ $# -gt 0 ]; do
  case "$1" in
    --env) ENV_NAME="${2:-kind}"; shift 2 ;;
    --env=*) ENV_NAME="${1#*=}"; shift ;;
    -h|--help) echo "usage: $0 [--env kind|staging-like]"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }

# Source the git-ignored env file if present (does not print it).
if [ -f "$ROOT/.env.kind" ]; then
  log "loading ${ROOT}/.env.kind"
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env.kind"
  set +a
fi

command -v kubectl >/dev/null 2>&1 || { echo "error: kubectl not found" >&2; exit 1; }

# secret-key : candidate env vars (first non-empty wins).
PAIRS=(
  "database-url:ONTOAXIS_DATABASE_URL,ONTOAXIS_DB_PASSWORD"
  "oidc-client-secret:ONTOAXIS_OIDC_CLIENT_SECRET"
  "meilisearch-master-key:ONTOAXIS_MEILISEARCH_MASTER_KEY"
  "keycloak-bootstrap-admin-username:ONTOAXIS_KEYCLOAK_ADMIN_USER,KEYCLOAK_BOOTSTRAP_ADMIN_USERNAME"
  "keycloak-bootstrap-admin-password:ONTOAXIS_KEYCLOAK_ADMIN_PASSWORD,KEYCLOAK_BOOTSTRAP_ADMIN_PASSWORD"
  "keycloak-db-password:ONTOAXIS_KEYCLOAK_DB_PASSWORD"
  "postgres-password:ONTOAXIS_POSTGRES_PASSWORD,POSTGRES_PASSWORD"
  "trino-user:ONTOAXIS_TRINO_USER"
  "trino-password:ONTOAXIS_TRINO_PASSWORD"
  "trino-credentials:ONTOAXIS_TRINO_CREDENTIALS"
  "openfga-datastore-uri:ONTOAXIS_OPENFGA_DATASTORE_URI"
  "postgres-database:ONTOAXIS_POSTGRES_DB"
  "postgres-user:ONTOAXIS_POSTGRES_USER"
  "seaweedfs-s3-access-key:ONTOAXIS_SEAWEEDFS_S3_ACCESS_KEY"
  "seaweedfs-s3-secret-key:ONTOAXIS_SEAWEEDFS_S3_SECRET_KEY"
  "openfga-token:ONTOAXIS_OPENFGA_TOKEN"
  "backup-passphrase:ONTOAXIS_BACKUP_PASSPHRASE"
)

missing=()
args=(kubectl -n "$SECRET_NS" create secret generic "$SECRET_NAME" --dry-run=client -o yaml)
set_keys=()

for pair in "${PAIRS[@]}"; do
  key="${pair%%:*}"
  vars="${pair#*:}"
  val=""
  from=""
  IFS=',' read -r -a names <<<"$vars"
  for v in "${names[@]}"; do
    if [ -n "${!v:-}" ]; then val="${!v}"; from="$v"; break; fi
  done
  if [ -z "$val" ]; then
    missing+=("$key")
  else
    args+=(--from-literal="${key}=${val}")
    set_keys+=("$key <- $from")
  fi
done

if [ "${#missing[@]}" -gt 0 ]; then
  echo "error: missing required secret values (env vars not set):" >&2
  for m in "${missing[@]}"; do echo "  - $m" >&2; done
  echo "hint: cp .env.kind.example .env.kind and fill values, or export them before running." >&2
  exit 1
fi

"${args[@]}" | kubectl apply -f - >/dev/null
log "bootstrap Secret '${SECRET_NAME}' applied in namespace '${SECRET_NS}'"
log "runtime environment: ${ENV_NAME}"
log "keys set (names only):"
for k in "${set_keys[@]}"; do echo "  - ${k}"; done
