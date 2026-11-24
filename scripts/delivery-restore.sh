#!/usr/bin/env bash
# Restore a synthetic backup in a dedicated local kind/k3d namespace (L1–L4).
# Usage: delivery-restore.sh --context CONTEXT --backup-dir DIR [--keep]
#        delivery-restore.sh --context CONTEXT --dump FILE --manifest FILE
# --core-image is rejected until a genuinely read-only core path is available.
set -euo pipefail

backup_dir=""; dump=""; manifest=""; context=""; keep=0; core_image="${RESTORE_CORE_IMAGE:-}"
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
while [ $# -gt 0 ]; do
  case "$1" in
    --backup-dir|--dump|--manifest|--context|--core-image)
      if [ "$#" -lt 2 ] || [ -z "$2" ] || [[ "$2" == --* ]]; then die "missing argument for $1"; fi
      case "$1" in
        --backup-dir) backup_dir="$2" ;; --dump) dump="$2" ;; --manifest) manifest="$2" ;;
        --context) context="$2" ;; --core-image) core_image="$2" ;;
      esac
      shift 2 ;;
    --keep) keep=1; shift ;;
    -h|--help) printf '%s\n' 'usage: delivery-restore.sh --context CONTEXT --backup-dir DIR [--keep]' '       delivery-restore.sh --context CONTEXT --dump FILE --manifest FILE'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -z "$core_image" ] || die 'L5 not executed: core has startup migrations and no enforced read-only mode; refusing --core-image'
if [ -n "$backup_dir" ]; then
  dump="${dump:-$backup_dir/ontoaxis.dump}"
  manifest="${manifest:-$backup_dir/manifest.json}"
fi
if [ ! -f "$dump" ] || [ ! -f "$manifest" ]; then die 'provide an existing dump and manifest'; fi

preflight="$(python3 -E - "$manifest" "$dump" <<'PY'
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import sys

try:
    meta = json.loads(Path(sys.argv[1]).read_text())
    dump = Path(sys.argv[2])
    assert re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", meta["backup_id"]), "invalid backup_id"
    date = meta["created_at"]
    if re.fullmatch(r"\d{8}T\d{6}Z", date):
        datetime.strptime(date, "%Y%m%dT%H%M%SZ")
    else:
        assert datetime.fromisoformat(date.replace("Z", "+00:00")).tzinfo is not None, "timestamp needs timezone"
    assert type(meta["size_bytes"]) is int and meta["size_bytes"] > 0, "invalid size_bytes"
    assert re.fullmatch(r"[1-9]\d*", str(meta["schema_version"])), "invalid schema_version"
    assert re.fullmatch(r"\d{1,2}\.\d{1,2}(?:\.\d{1,2})?", meta["pg_version"]), "invalid pg_version"
    ref = meta["image_digest"]
    assert re.fullmatch(r"(?:[a-z0-9.-]+(?::\d+)?/)?(?:[a-z0-9._-]+/)*(?:postgres)@sha256:[0-9a-f]{64}", ref), "backup requires a pinned Postgres image"
    assert not ref.endswith("0" * 64), "placeholder Postgres digest"
    assert re.fullmatch(r"[0-9a-f]{64}", meta["sha256"]), "invalid sha256"
    assert dump.stat().st_size == meta["size_bytes"], "backup size mismatch"
    h = hashlib.sha256()
    with dump.open("rb") as f:
        assert f.read(5) == b"PGDMP", "not a Postgres custom dump"
        f.seek(0)
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    assert h.hexdigest() == meta["sha256"], "backup SHA-256 mismatch"
    print(meta["schema_version"], ref)
except (AssertionError, ValueError, KeyError, TypeError, OSError) as exc:
    sys.exit(f"backup preflight failed: {exc if isinstance(exc, AssertionError) else 'invalid manifest or dump'}")
PY
)" || die 'restore stopped before touching Kubernetes'
read -r schema pg_image <<< "$preflight"
printf '%s\n' 'L1: backup metadata, size, format and SHA-256 verified'
case "$context" in kind-ontoaxis-kind|k3d-ontoaxis-k3d) ;; *) die 'specify the approved local kind-ontoaxis-kind or k3d-ontoaxis-k3d context' ;; esac
# Do not interpret arbitrary images from a backup as executable workloads.
case "$pg_image" in localhost:5000/library/postgres@*|localhost:5000/postgres@*) ;; *) die 'backup Postgres image is not in the approved local registry' ;; esac
command -v kubectl >/dev/null || die 'kubectl not found'
k() { kubectl --context "$context" "$@"; }
server="$(k config view --minify -o 'jsonpath={.clusters[0].cluster.server}')"
[[ "$server" =~ ^https://(127\.0\.0\.1|localhost):[0-9]+$ ]] || die 'restore requires a loopback Kubernetes API'
NS="oa-restore-$(python3 -c 'import secrets; print(secrets.token_hex(6))')"
export PG_IMAGE="$pg_image"
created=0
cleanup() {
  rc=$?
  trap - EXIT
  if [ "$created" -eq 1 ]; then
    if [ "$keep" -eq 1 ]; then
      printf 'restore namespace retained: %s\n' "$NS" >&2
    elif ! k delete namespace "$NS" --wait=true --timeout=60s >/dev/null; then
      printf 'error: could not clean restore namespace %s\n' "$NS" >&2
      rc=1
    fi
  fi
  exit "$rc"
}
trap cleanup EXIT
k create namespace "$NS" >/dev/null
created=1

# No Service or published port. Password is generated into a Secret (stdin);
# SQL is run only over the socket of the temporary Pod, never against the source.
python3 - <<'PY' | k -n "$NS" create -f - >/dev/null
import json
import secrets
print(json.dumps(dict(apiVersion="v1", kind="Secret", metadata=dict(name="restore-postgres"),
                      stringData=dict(password=secrets.token_hex(32)))))
PY
python3 - <<'PY' | k -n "$NS" create -f - >/dev/null
import json
import os
pod = dict(apiVersion="v1", kind="Pod", metadata=dict(name="restore-postgres"), spec=dict(
    automountServiceAccountToken=False, restartPolicy="Never", containers=[dict(
        name="postgres", image=os.environ["PG_IMAGE"], args=["-c", "listen_addresses="],
        env=[dict(name="POSTGRES_PASSWORD", valueFrom=dict(secretKeyRef=dict(name="restore-postgres", key="password")))],
        readinessProbe=dict(exec={"command": ["pg_isready", "-U", "postgres"]}, periodSeconds=2))]))
print(json.dumps(pod))
PY
k -n "$NS" wait --for=condition=Ready pod/restore-postgres --timeout=180s >/dev/null
if ! k -n "$NS" exec -i restore-postgres -- sh -c 'umask 077; cat > /tmp/ontoaxis.dump' < "$dump"; then
  die 'could not transfer dump to restore pod'
fi
remote_hash="$(k -n "$NS" exec restore-postgres -- sha256sum /tmp/ontoaxis.dump)"
local_hash="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["sha256"])' "$manifest")"
[ "${remote_hash%% *}" = "$local_hash" ] || die 'dump changed during transfer'
k -n "$NS" exec restore-postgres -- pg_restore --list /tmp/ontoaxis.dump >/dev/null || die 'pg_restore could not read dump index'
printf '%s\n' 'L2: pg_restore archive index verified'
k -n "$NS" exec restore-postgres -- pg_restore --exit-on-error --single-transaction --no-owner --no-privileges -U postgres -d postgres /tmp/ontoaxis.dump >/dev/null || die 'pg_restore failed; restored database not verified'
sql() { k -n "$NS" exec restore-postgres -- psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres -tAqc "$1"; }
actual="$(sql 'SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1')"
[ "$actual" = "$schema" ] || die 'restored goose schema version does not match manifest'
printf 'L3: restored goose schema version %s\n' "$actual"
for table in core.organizations ontology.object_types object_data.objects; do
  count="$(sql "SELECT count(*) FROM $table")"
  [[ "$count" =~ ^[0-9]+$ ]] || die "could not get row count: $table"
  printf 'L4: %s count=%s\n' "$table" "$count"
done
printf '%s\n' 'L1–L4 completed; L5 not executed (no core deployed). This is not full recovery acceptance.'
