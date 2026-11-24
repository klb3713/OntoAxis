# Delivery Runbook — OntoAxis

Operational runbook for the `production-delivery` change: local **kind**
delivery, GitOps promotion, backup/restore, and the drills around them.

> **Scope / honesty note.** These procedures are authored and script-validated
> (`bash -n`) and have been executed against a local **kind** cluster
> (2026-11-23/24; evidence in
> `docs/stages/evidence/stage-2-production-delivery-kind.md`). They have **not**
> been executed against a real staging/production environment. Backup **RPO ≤
> 24h / RTO ≤ 60min** numbers below are **technical design targets only — not a
> production SLA promise**.

Environment names: `kind` (local), `staging-like`, `prod-like`. Prod-like is
**manual sync**; kind/staging-like are automated prune+selfHeal.

## 0. 入口表

| 目标 | 命令（本仓） | 说明 |
| --- | --- | --- |
| 建/删集群 | `deploy/kind/kind-up.sh` / `kind-down.sh`（或 `k3d-*`） | 1 control-plane + 2 workers + 本地 registry |
| 渲染/校验 | `make delivery-render-check`、`make delivery-lint`、`make delivery-config-check` | 确定性渲染、chart lint、配置漂移 |
| 部署（受控 GitOps） | `bash deploy/kind/bootstrap.sh --check-source` → `bash deploy/kind/bootstrap.sh` | 先审查固定提交与集群内 Git 源，见 §1 |
| 烟测/诊断 | `bash scripts/delivery-smoke.sh --env kind`；失败时 `bash scripts/delivery-diag.sh` | 完整验收默认 `full`；`edge` 只供诊断 |
| 镜像构建/晋升 | `make delivery-promote`、`scripts/delivery-release-check.sh --env <env>` | 见 §3 |
| 回滚 | `scripts/delivery-rollback.sh` | 按 GitOps 发布验证，见 §3 |
| 备份 | `kubectl -n oa-data create job ontoaxis-backup-manual --from=cronjob/ontoaxis-backup` | 见 §4 |
| 恢复 | `bash scripts/delivery-restore.sh --context <local-context> --backup-dir <dir>` | 仅 L1–L4；见 §4 |
| IdP 检查 | `bash scripts/delivery-idp-check.sh --issuer <url> --core-url <url>` | 见 §7 |

---

## 1. Install (kind)

前置：Docker、`kind`（或 `k3d`）、`kubectl`、Helm、Python 3 + PyYAML；本地只用合成身份和凭据。当前手工 Helm 渲染部署不等于 GitOps 验收。

1. 由操作者先按环境准备全部启用镜像的真实 digest、匹配的发布清单、必要的数据库/授权初始化与合成 OIDC 身份。`prod-like` 仅做渲染；这里的本地脚本不连接预发或生产。
2. 用 `python3 deploy/kind/render-gitops-root.py --checkout <reviewed-repo> --content-revision <sha> --env kind-small --out <new-external-dir>` 从已审核内容提交生成八个子 Application。工具不提交、不发布、不连接集群。评审后在受控 GitOps 仓库录入根提交；设置 `GITOPS_CHECKOUT`、`GITOPS_ROOT_REVISION`、`GITOPS_CONTENT_REVISION`、`ROOT_APP_ENV`（`kind` 或 `kind-small`），再运行 `bash deploy/kind/bootstrap.sh --check-source` 验证双 SHA。不从工作区或公网补取。
3. 启动经确认的本地 kind/k3d，显式设置该集群生成的 `KUBECONFIG`。在 `argocd` 命名空间内的只读 ClusterIP `ontoaxis-git` 发布这两个审核提交；不得开 NodePort、LoadBalancer 或主机端口。使用环境变量或 git-ignored `.env.kind` 提供合成 Secret 值，再运行 `bash deploy/kind/bootstrap.sh`。
4. 用相同 SHA 和 `KUBECONFIG` 运行 `bash deploy/kind/verify.sh --wait-only`；九个 Application 的健康、同步、实际 SHA 全部匹配后，才能运行 `bash deploy/kind/verify.sh`。默认 `full`；换成 `VERIFY_SMOKE_PROFILE=readiness` 仅证明预检通过，不替代正式验收。

`bootstrap.sh --skip-apps` 只启动控制面；跳过 ArgoCD、ESO 或 Envoy 的标志仅适用于已经安装并验证这些组件的本地集群。CI 的受控镜像、Git 源与合成身份准备仍未完成自动化，不能把 workflow 已接线当作已经部署。

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
# 默认只检查计划；来源必须是 full 报告，镜像清单须与源/目标构建一致。
bash scripts/delivery-promote.sh --from kind --to staging-like \
  --source-smoke "$SOURCE_FULL_REPORT" --target-release "$TARGET_RELEASE"
# 在干净工作区审查计划后，以 --commit 明确生成本地提交；不自动推送。
```

晋升检查完整源烟测、registry 摘要、源/目标镜像身份及渲染产物；Web 需使用对应目标环境构建。`prod-like` 仍为只渲染，准备候选提交须显式 `--confirm`，不代表允许部署。

### Rollback

```sh
# 用此前可用的发布清单和历史 readiness/full 证据先生成回滚计划。
bash scripts/delivery-rollback.sh --env kind --previous "$PREVIOUS_RELEASE" \
  --previous-smoke "$PREVIOUS_SMOKE"
# 审查后可在干净工作区加 --commit --context "$LOCAL_CONTEXT" 生成候选提交。
# 只对已人工发布进集群内 Git 源、根清单固定至此内容提交的版本同步。
bash scripts/delivery-rollback.sh --env kind --context "$LOCAL_CONTEXT" \
  --sync "$REVIEWED_ROLLBACK_SHA" --gitops-root-revision "$REVIEWED_ROOT_SHA"
```

回滚提交与同步分离。不支持静默改 Live YAML 或直接回滚 Helm，失败不会留下成功标记。同步前后检查平台迁移版本、GitOps 固定提交、readiness 及实际镜像；跨 schema 或需要换 PostgreSQL 工作负载时停止，另走审核后的恢复流程。切换镜像不等于验证了模块 schema 兼容。

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
# Local only; specify KUBECONFIG explicitly when using a separate kubeconfig.
scripts/delivery-restore.sh --context kind-ontoaxis-kind --backup-dir dist/backups/<backup_id>
# Optional: --keep retains this run's isolated namespace for troubleshooting.
```

恢复工具只接受受控 kind/k3d context 和本机回环 Kubernetes API。校验备份后，在随机命名的 `oa-restore-*` 命名空间用独立 PostgreSQL、Unix socket 和单事务恢复；不发布 Service，不连接源数据库，默认清理。校验错误立即失败。该工具只覆盖 L1–L4；当前 core 启动会执行迁移，`--core-image` 被拒绝，不能把健康探针或匿名 401 当作只读业务验证。

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
# Without a token, only discovery and JWKS are checked.
# Positive API: supply OIDC_TEST_TOKEN through the shell environment.
# Negative API: also supply a distinct OIDC_CONTROL_TOKEN, and select
# IDP_NEGATIVE=audience|issuer|expired|bad_signature|bad_kid|missing_claims.
# Record independently how the named fixture was prepared; HTTP 000 is not rejection.
```

---

## 8. Access: local port-forward vs ingress

本地烟测为 edge Service 在 `127.0.0.1` 上申请独立动态端口，并等待该次 `kubectl port-forward` 成功。`--profile edge` 只检查 HTTP 入口；`--profile readiness` 另查 GitOps、正在运行的 digest、探针/PDB/ESO 和 IdP；默认 `--profile full` 还要求本次发布对应的备份与回滚标记。端口转发不作为生产入口。 In staging/prod, traffic reaches the edge through the Envoy
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

## 10. 故障处置（按症状）与备份校验分级

### 10.1 故障分类与处置

| 故障 | 典型症状 | 诊断命令 | 处置 | 证据 |
| --- | --- | --- | --- | --- |
| 缺失镜像 / digest 错误 | Pod `ImagePullBackOff`、`ErrImagePull` | `kubectl -n <ns> describe pod <pod>`；`kubectl -n <ns> get events --sort-by=.lastTimestamp` | 修正 `deploy/releases/<env>.yaml` 与 values 的 digest；重新推送镜像 | `delivery-diag.sh` 的 describe/events |
| Secret 未同步 | Pod `CreateContainerConfigError`、ESO `SecretSynced False` | `kubectl -n <ns> get externalsecret`；`kubectl -n external-secrets describe secretstore` | 检查 `ClusterSecretStore`、bootstrap Secret 键；`kubectl annotate externalsecret <n> force-sync=$(date +%s)` | `events.txt`、ESO status |
| Pod 未就绪 | Ready 0/1、rollout 卡住 | `kubectl -n <ns> get pods`、`kubectl -n <ns> describe pod <pod>` | 看 readiness 探针路径/依赖（DB、JWKS）；修 env 或依赖 | `events.txt`、容器日志尾部 |
| 迁移失败 | core `startup failed`、goose 报错 | `kubectl -n oa-app logs deploy/ontoaxis-core` | 校验 DB 可达与 schema 版本；按 §6 跨 schema 流程处理 | core 日志 |
| DB 不可达 | core `ping database` 失败、`/readyz` 非 200 | `kubectl -n oa-data get pods`；`kubectl -n oa-data logs deploy/postgres` | 检查 postgres Pod/Secret/PVC；恢复后重启依赖组件 | pod 状态、日志 |
| 资源超限（OOM） | `OOMKilled`、`Exit Code 137` | `kubectl -n <ns> describe pod <pod>` | 提高 limits 或降低负载；`delivery-diag.sh` 采集 `Last State` | `describe.txt` 的 `Last State/Terminated` |
| ArgoCD Degraded | Application `Degraded`/`OutOfSync` | `argocd app get <app>`；`kubectl -n argocd get app` | 查 sync 错误、漂移；`argocd app sync` 或修 values（本 change 中 root app 仍为占位源，未接管） | Application 条件/history |
| 备份校验失败 | `delivery-restore.sh` 拒绝执行 | 复算 `sha256` 与 `manifest.json` 比对；`pg_restore --list` | 重新备份；损坏备份不得用于恢复 | restore 输出 |
| 回滚失败 | rollout 超时、目标 digest 不可用 | `kubectl -n <ns> rollout status deploy/<d>`；`kubectl -n <ns> rollout history` | `kubectl rollout undo` 或按上一 release manifest 回写 digest | rollout 事件、release-check |
| edge 502/404 | 烟测 `web-root` 或 API 502/404 | `kubectl -n envoy-gateway-system get pods`；`kubectl -n <ns> logs <edge>` | 检查 Gateway/HTTPRoute 与后端 Service/Endpoints（本 change 已知 X-01/X-02/X-03） | smoke JSON、edge 日志 |

### 10.2 备份校验分级（L1–L5）

| 级别 | 校验内容 | 命令 |
| --- | --- | --- |
| L1 完整性 | manifest 存在且 `sha256` 与 dump 复算一致 | `shasum -a 256 <dump>` 对比 `manifest.json.sha256` |
| L2 可读性 | dump 可被 `pg_restore` 解析 | `pg_restore --list <dump>` |
| L3 结构 | schema 迁移版本一致 | `psql -c "select max(version_id) from goose_db_version"`（或模块迁移表） |
| L4 数据 | 代表性表行数抽样 | `select count(*) from core.organizations/ontology.object_types/object_data.objects` |
| L5 服务 | 恢复实例 core 在强制只读权限下就绪；具合法身份的只读 API 返回预期数据 | 尚未实现可执行路径；不接受匿名 `401` 作为业务验证 |

演练记录：2026-11-24 本地 kind 通过 L1–L4（goose 版本 1，三张代表表行数均为 0）；截断备份与同长度损坏备份分别在大小和 SHA-256 检查被拒绝，未触及 Kubernetes。L5 未通过，详见 [kind 实测记录](../stages/evidence/stage-2-production-delivery-kind.md#15-2026-11-24-复核补充56-release-manifest72--76-恢复演练)。

桌面推演结论：步骤 1–7 的脚本与命令均存在（`delivery-restore.sh`、`delivery-rollback.sh`、`delivery-smoke.sh`），本次未在真实生产执行；`not executed` 原因为无生产集群与真实备份。

> 2026-11-24 范围说明：本手册覆盖的交付资产已在本地 kind 演练验证；生产级验证（wave 健康门控、真实 `--sync` 回滚、k3d、CI runner、真实 staging IdP、完整烟测链）不在 `production-delivery` 归档范围，移交 Stage 3 后续 change。
