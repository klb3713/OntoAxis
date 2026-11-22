#!/usr/bin/env bash
# delivery-restore.sh — restore a backup into an isolated oa-restore namespace
# and sanity-check it, without touching the live environment.
#
# Steps: validate manifest + sha256 (refuse a corrupted dump), create oa-restore
# and an independent Postgres, pg_restore, check goose version and representative
# row counts, optionally start core read-only, then clean up.
#
# Usage:
#   scripts/delivery-restore.sh --backup-dir dist/backups/<id>
#   scripts/delivery-restore.sh --dump path/to/db.dump --manifest path/to/manifest.json
# Options: --keep (do not delete oa-restore), --core-image <ref>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

BACKUP_DIR=""
DUMP=""
MANIFEST=""
KEEP=0
CORE_IMAGE="${RESTORE_CORE_IMAGE:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --backup-dir) BACKUP_DIR="${2:-}"; shift 2 ;;
    --dump) DUMP="${2:-}"; shift 2 ;;
    --manifest) MANIFEST="${2:-}"; shift 2 ;;
    --core-image) CORE_IMAGE="${2:-}"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

if [ -n "$BACKUP_DIR" ]; then
  [ -d "$BACKUP_DIR" ] || die "backup dir not found: $BACKUP_DIR"
  [ -z "$MANIFEST" ] && MANIFEST="$BACKUP_DIR/manifest.json"
  if [ -z "$DUMP" ]; then
    DUMP="$(ls -1 "$BACKUP_DIR"/*.dump 2>/dev/null | head -n1 || true)"
    [ -z "$DUMP" ] && [ -f "$BACKUP_DIR/dump" ] && DUMP="$BACKUP_DIR/dump"
  fi
fi
[ -n "$MANIFEST" ] && [ -f "$MANIFEST" ] || die "manifest not found (use --backup-dir or --manifest)"
[ -n "$DUMP" ] && [ -f "$DUMP" ] || die "dump not found (use --backup-dir or --dump)"

# 1. Validate manifest fields.
eval "$(python3 - "$MANIFEST" <<'PY'
import json, sys, shlex
doc = json.load(open(sys.argv[1], encoding="utf-8"))
for k in ("backup_id", "sha256", "size_bytes", "schema_version", "pg_version", "image_digest"):
    v = doc.get(k, "")
    print(f"{k}={shlex.quote(str(v))}")
PY
)"
[ -n "$sha256" ] || die "manifest missing sha256"
log "backup_id=${backup_id:-unknown} schema_version=${schema_version:-?} pg_version=${pg_version:-?}"

# 2. Verify sha256 (refuse corrupted dump).
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$DUMP" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$DUMP" | awk '{print $1}')"
else
  die "no shasum/sha256sum available to verify the dump"
fi
[ "$actual" = "$sha256" ] || die "refusing corrupted dump: sha256 mismatch (want ${sha256}, got ${actual})"
log "dump sha256 verified"

if [ -n "$size_bytes" ]; then
  actual_size="$(wc -c <"$DUMP" | tr -d ' ')"
  [ "$actual_size" = "$size_bytes" ] || warn "size mismatch (manifest=${size_bytes} actual=${actual_size})"
fi

# 3. Cluster + isolated namespace.
command -v kubectl >/dev/null 2>&1 || die "kubectl not found"
kubectl cluster-info >/dev/null 2>&1 || die "no Kubernetes cluster reachable"

NS="${RESTORE_NAMESPACE:-oa-restore}"
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
log "namespace '$NS' ready"

# 4. Independent Postgres (trust auth; restore-only, never exposed).
PG_IMAGE="postgres:${pg_version:-16}"
cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Service
metadata:
  name: ontoaxis-restore-postgres
  namespace: ${NS}
spec:
  selector:
    app: ontoaxis-restore-postgres
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ontoaxis-restore-postgres
  namespace: ${NS}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ontoaxis-restore-postgres
  template:
    metadata:
      labels:
        app: ontoaxis-restore-postgres
    spec:
      containers:
        - name: postgres
          image: ${PG_IMAGE}
          env:
            - name: POSTGRES_HOST_AUTH_METHOD
              value: trust
            - name: POSTGRES_DB
              value: postgres
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          ports:
            - containerPort: 5432
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
      volumes:
        - name: data
          emptyDir: {}
EOF
kubectl -n "$NS" rollout status deployment/ontoaxis-restore-postgres --timeout=240s
POD="$(kubectl -n "$NS" get pod -l app=ontoaxis-restore-postgres -o jsonpath='{.items[0].metadata.name}')"
[ -n "$POD" ] || die "restore Postgres pod not found"
for _ in $(seq 1 60); do
  if kubectl -n "$NS" exec "$POD" -- pg_isready -U postgres >/dev/null 2>&1; then break; fi
  sleep 1
done

# 5. pg_restore.
log "copying dump into restore pod"
kubectl -n "$NS" cp "$DUMP" "$POD:/tmp/restore.dump" >/dev/null
log "running pg_restore"
set +e
kubectl -n "$NS" exec "$POD" -- pg_restore -U postgres -d postgres --clean --if-exists --no-owner --no-privileges /tmp/restore.dump
restore_rc=$?
set -e
if [ "$restore_rc" -gt 1 ]; then
  die "pg_restore failed (rc=$restore_rc)"
elif [ "$restore_rc" -eq 1 ]; then
  warn "pg_restore completed with warnings (rc=1)"
else
  log "pg_restore completed"
fi

# 6. Goose schema version.
gv="$(kubectl -n "$NS" exec "$POD" -- psql -U postgres -d postgres -tAc \
  "select version_id from goose_db_version order by id desc limit 1" 2>/dev/null | tr -d '[:space:]' || true)"
if [ -n "$gv" ]; then
  log "goose latest version_id=${gv} (manifest schema_version=${schema_version:-?})"
  if [ -n "$schema_version" ] && [ "$gv" != "$schema_version" ]; then
    warn "goose version (${gv}) != manifest schema_version (${schema_version})"
  fi
else
  warn "could not read goose_db_version (table may be in a different database)"
fi

# 7. Representative row counts.
QUERIES="${RESTORE_ROWCOUNT_QUERIES:-select 'organizations', count(*) from organizations;select 'object_types', count(*) from object_types;select 'objects', count(*) from objects;}"
log "representative row counts:"
IFS=';' read -r -a qs <<<"$QUERIES"
for q in "${qs[@]}"; do
  [ -z "${q// /}" ] && continue
  if out="$(kubectl -n "$NS" exec "$POD" -- psql -U postgres -d postgres -tAc "$q" 2>&1)"; then
    echo "  ${q} -> ${out}"
  else
    warn "row-count query failed: ${q} (${out})"
  fi
done

# 8. Optional: start core in read-only mode against the restored DB.
if [ -n "$CORE_IMAGE" ]; then
  log "starting core (read-only) against restored DB: ${CORE_IMAGE}"
  cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ontoaxis-restore-core
  namespace: ${NS}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ontoaxis-restore-core
  template:
    metadata:
      labels:
        app: ontoaxis-restore-core
    spec:
      containers:
        - name: core
          image: ${CORE_IMAGE}
          env:
            - name: ONTOAXIS_DATABASE_URL
              value: postgres://postgres@ontoaxis-restore-postgres:5432/postgres?sslmode=disable
            - name: ONTOAXIS_READ_ONLY
              value: "true"
          ports:
            - containerPort: 8080
EOF
  if kubectl -n "$NS" rollout status deployment/ontoaxis-restore-core --timeout=180s; then
    kubectl -n "$NS" port-forward deployment/ontoaxis-restore-core 18089:8080 >/dev/null 2>&1 &
    pf=$!
    for _ in $(seq 1 30); do
      if curl -fsS http://127.0.0.1:18089/healthz >/dev/null 2>&1; then log "restore core /healthz OK"; break; fi
      sleep 0.5
    done
    kill "$pf" 2>/dev/null || true
  else
    warn "restore core did not become ready"
  fi
else
  log "no --core-image/RESTORE_CORE_IMAGE; skipping core read-only start"
fi

# 9. Cleanup.
if [ "$KEEP" -eq 1 ]; then
  log "keeping namespace '$NS' (--keep)"
else
  log "deleting namespace '$NS'"
  kubectl delete namespace "$NS" --wait=false || warn "namespace delete request failed"
fi

log "restore verification complete"
