# Delivery Runbook — OntoAxis

Operational runbook for the `production-delivery` change: local **kind**
delivery, GitOps promotion, backup/restore, and the drills around them.

> **Scope / honesty note.** These procedures are authored and script-validated
> (`bash -n`) but have **not been executed against a live cluster in Stage 2**.
> Steps that require a cluster are marked *requires cluster*. Backup **RPO ≤ 24h
> / RTO ≤ 60min** numbers below are **technical design targets only — not a
> production SLA promise**.

Environment names: `kind` (local), `staging-like`, `prod-like`. Prod-like is
**manual sync**; kind/staging-like are automated prune+selfHeal.

---

## 1. Install (kind)

Prereqs: Docker, `kind` (or `k3d`), `kubectl`, `helm`, `argocd` CLI (optional),
`curl`, `python3`.

```sh
# 1. Cluster + local registry (containerd mirror configured) + kubeconfig at dist/kind/kubeconfig
deploy/kind/kind-up.sh
#    or: deploy/kind/k3d-up.sh

# 2. Fill secrets (git-ignored), then bootstrap everything
cp .env.kind.example .env.kind      # fill real values, never commit
deploy/kind/bootstrap.sh            # namespaces, ArgoCD, ESO, Envoy Gateway,
                                    # bootstrap Secret, AppProject, root app

# 3. Wait for workloads and run smoke (calls delivery-smoke; on failure runs delivery-diag)
deploy/kind/verify.sh --env kind
```

`bootstrap.sh` flags: `--skip-argocd`, `--skip-eso`, `--skip-envoy`, `--skip-apps`.
It reuses the existing Envoy path (`deploy/edge/kind/install-envoy-gateway.sh`)
and applies `deploy/argocd/projects/ontoaxis.yaml` + the root app.

### Uninstall

```sh
deploy/kind/kind-down.sh     # deletes the cluster and the local registry container
#    or: deploy/kind/k3d-down.sh
```

For a prod-like target, remove ArgoCD Applications (or the root app with
cascade) and then the namespaces — do **not** run the kind teardown scripts
there.

---

## 2. Secrets

Secrets flow: `.env.kind` → `ontoaxis-bootstrap` Secret (ns `external-secrets`)
→ `ClusterSecretStore` → `ExternalSecret` → target Secrets consumed by pods.
Key inventory and rotation live in `deploy/argocd/secrets/README.md`.

```sh
# Create/refresh the bootstrap Secret from .env.kind (never prints values;
# exits 1 listing any missing key names)
scripts/delivery-secrets-bootstrap.sh --env kind

# Force ESO to re-read (after rotating a value in .env.kind + re-running bootstrap)
kubectl -n oa-app annotate externalsecret ontoaxis-core force-sync="$(date +%s)" --overwrite
# then restart affected workloads so they pick up the new Secret
```

Rules: never commit `.env.kind` or any value; the manifests carry **names only**
(`secretKeyRef`/`remoteRef`/`valueFrom`). Run `scripts/delivery-secret-scan.sh`
before committing delivery changes (uses `gitleaks` when available, else a
conservative grep fallback).

---

## 3. Build, promote, rollback

### Build images + record digests

```sh
scripts/delivery-images.sh            # builds core+web, pushes to localhost:5000,
                                      # writes dist/delivery/digests.env and updates
                                      # deploy/releases/kind.yaml digests
```

### Promote a verified release

```sh
# Requires a passing smoke file for the source env (dist/delivery/smoke-<from>.json with "passed": true)
scripts/delivery-promote.sh --from kind --to staging-like
# prod-like additionally requires --confirm:
scripts/delivery-promote.sh --from staging-like --to prod-like --confirm
```

Promote validates source digests, rewrites **only** digest/commit fields in the
target manifest (and values file if given), runs the render check when present,
and stages a git commit (no push). It refuses unless the source smoke passed.

### Rollback

```sh
scripts/delivery-rollback.sh --env kind --previous deploy/releases/kind.yaml.previous
```

Rollback reads the previous manifest, rewrites the current manifest digests,
syncs (argocd CLI, or `kubectl annotate application ontoaxis-root refresh=hard`),
waits Healthy (best effort), runs smoke and writes a markdown/JSON rollback
marker under `dist/delivery/`.

### Drift check

```sh
scripts/delivery-drift-check.sh --env kind
```

Summarises `kubectl get Application/Deployment/ConfigMap` drift and **exits 1**
on drift. Run it in CI and before promoting.

---

## 4. Backup and restore

Backups: `pg_dump -Fc` daily 02:00 UTC to a PVC, with a `manifest.json` recording
`backup_id, created_at, sha256, size_bytes, schema_version, image_digest,
pg_version`; keep 7. **RPO ≤ 24h (daily cadence), RTO ≤ 60min (restore+verify) —
technical targets only, not a production promise.**

```sh
# Restore into an isolated oa-restore namespace (never the live env):
scripts/delivery-restore.sh --backup-dir dist/backups/<backup_id>
#   optional: --keep (keep oa-restore) --core-image <ref> (start core read-only)
```

The restore validates the manifest and **sha256** and refuses a corrupted dump;
it creates `oa-restore`, an independent Postgres, `pg_restore`s, compares the
goose schema version and representative row counts, optionally starts core
read-only against the restored DB, then cleans up.

---

## 5. Drills

- **Drift drill**: introduce an out-of-band change (e.g. `kubectl edit deploy`),
  run `delivery-drift-check.sh` → expect exit 1 + a diff summary; then sync and
  confirm it returns clean.
- **Rollback drill**: promote a new build, then `delivery-rollback.sh` to the
  previous manifest; confirm smoke passes and the marker file is written.

---

## 6. Cross-schema rollback (no down migrations)

The platform uses **forward-only** goose migrations; there are **no down
migrations**. Rolling back a release therefore restores the **previous image
digests**, not the database schema.

- If the new release added columns/tables, the previous image must remain
  compatible (additive changes only) — otherwise roll forward with a fix.
- To recover from a bad schema change, restore from a pre-change backup via
  `delivery-restore.sh` in `oa-restore`, validate, then repoint/redeploy.
- Never hand-edit migration history or force a goose version outside a restore.

---

## 7. Staging IdP fallback (partial)

Staging-like may run with an **in-cluster Keycloak** instead of an external IdP.
This is a **partial** fallback: it validates core's OIDC issuer/audience and
token flow, but does not exercise the production IdP's network path, signing-key
rotation, or claim maps. Document that any staging IdP pass is not a production
identity acceptance.

```sh
scripts/delivery-idp-check.sh --issuer <oidc-issuer> --core-url <core>
# optional real token: OIDC_TEST_TOKEN=... (never printed)
# negative cases: IDP_NEGATIVE=audience|issuer|expired|bad_kid|missing_claims
```

---

## 8. Access: port-forward 18088 vs ingress

For local kind delivery, smoke/diag use a **port-forward of the edge service to
`127.0.0.1:18088`** (`delivery-smoke.sh`). This is a developer convenience, not a
production ingress. In staging/prod, traffic reaches the edge through the Envoy
Gateway `HTTPRoute`/`Gateway` (ArgoCD Directory app over
`deploy/edge/generated/`). Never expose a port-forward as a production entry
point, and never bypass the edge for user traffic.

---

## 9. Residual risks (accepted / rebuildable)

The following are treated as **rebuildable** state, not backed up as source of
truth — losing them is recoverable by re-sync/re-index (documented risk, not
silent):

- Object storage (SeaweedFS/S3) contents — rebuild from source systems.
- Meilisearch indexes — rebuild by re-indexing from Postgres.
- Kafka topics / Debezium offsets — rebuild from source connectors.
- Kestra / Temporal workflow state — in-flight runs may be lost; re-run.
- Trino / Nessie catalog metadata — rebuild catalogs; Nessie history may be lost.

Only **Postgres** (system of record) is backed up under the policy in §4. The
in-cluster observability stack (Prometheus history, Jaeger traces) is also not
backed up.

## 资源画像与 PDB 推导（2026-11-23）

| 环境 | 默认副本 | requests (cpu/mem) | limits (cpu/mem) | PDB minAvailable |
| --- | --- | --- | --- | --- |
| kind | 1 | 25m / 64Mi | 250m / 256Mi | 1 |
| staging-like | 2 | 100m / 128Mi | 1 / 1Gi | 1 |

- 数值来源：`deploy/values/kind.yaml`、`deploy/values/staging-like.yaml` 的 `global.defaultReplicas/defaultResources`；逐组件覆盖见各 chart `values.yaml`。
- PDB 推导：`minAvailable = max(1, replicas-1)`；kind 单副本因此为 1（允许 0 可用，符合本地单节点假设），staging-like 双副本为 1，滚动期间至少保留 1 个可用副本。
- `kind-small` 将 9 个重组件置为 0 副本，保留 core/web/数据库等；其请求/限制沿用 `kind` 默认值。
- 变更资源画像时必须同步本表与 chart values；`make delivery-render-check` 保证渲染不漂移。

## 跨 schema 回滚流程（桌面推演，2026-11-23）

前提：只向前迁移，不执行 down migration；回滚镜像不等于回滚 schema。

1. **冻结写入**：将入口置为维护/只读（edge 503 或 core 只读开关），等待在途事务结束；记录停止时间。
2. **比对 schema 版本**：`psql -c "select version from schema_migrations order by version"`（或各模块迁移表）与目标镜像内置迁移清单比对，确认存在高于旧镜像的迁移。
3. **恢复备份到独立实例**：用 `scripts/delivery-restore.sh` 将最近成功备份恢复到独立 Postgres 实例；校验 `sha256`、`pg_restore --list` 与行数抽样。
4. **部署旧镜像**：按上一 release manifest 用 `scripts/delivery-rollback.sh` 回写 digest，ArgoCD 同步到旧镜像；不删除新迁移。
5. **校验**：`/healthz` `/readyz`、核心烟测、关键表行数与租户隔离查询；确认旧代码能读取新 schema（向后兼容）或已切换独立实例。
6. **恢复写入**：解除只读，观察 30 分钟错误率；记录回滚耗时与最终 digest。
7. **禁止项**：不得 `DROP COLUMN`/`ALTER TYPE` 回退；不得直接修改生产 schema；若旧代码不兼容新 schema，只能保持维护并前滚修复。

桌面推演结论：步骤 1–7 的脚本与命令均存在（`delivery-restore.sh`、`delivery-rollback.sh`、`delivery-smoke.sh`），本次未在真实生产执行；`not executed` 原因为无生产集群与真实备份。
