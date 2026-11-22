# production-delivery 静态交付证据（2026-11-23）

> 状态：静态资产已生成并做语法/结构校验；**未在 kind/k3d/ArgoCD/真实集群执行**。本文件不把小规模静态检查冒充 AC-08/AC-10 的集群证据。

## 环境

- macOS x86_64；Docker 27.0.3；kubectl、kind 已安装；Helm、kubeconform、argocd CLI、gitleaks 未安装。
- 未连接任何云账号、集群或真实 IdP；无生产密钥/客户数据。

## 已生成资产

- `deploy/helm/ontoaxis-{platform,data,app,observability,jobs}/`（5 charts，Chart.yaml/values/templates/PDB/ExternalSecret/backup CronJob）。
- `deploy/values/{common,kind,kind-small,staging-like,prod-like}.yaml`。
- `deploy/argocd/`（AppProject、root-app、8 个 sync-wave Application、prod-like manual apps、secrets 三环境模板、README）。
- `deploy/kind/`（kind/k3d up/down、bootstrap、verify）。
- `scripts/delivery-{smoke,diag,images,drift-check,release-check,promote,rollback,secrets-bootstrap,secret-scan,idp-check,restore,render-check,version-check}.sh`。
- `deploy/releases/{kind,staging-like}.yaml`（REPLACE_ME 占位，诚实失败）。
- `docs/operations/delivery-runbook.md`、`deploy/README.md`、`.env.kind.example`、`.gitleaks.toml`。

## 已执行校验

| 命令 | 结果 |
| --- | --- |
| `for f in scripts/delivery-*.sh deploy/kind/*.sh; do bash -n "$f"; done` | 全部通过（17/17） |
| `ruby -e 'require "yaml"; ...'` on chart/values/argocd/release YAML | chart/values + 41 ArgoCD docs parse；0 bad |
| `scripts/delivery-render-check.sh` | `SKIP helm not installed`；YAML fallback PASS |
| `scripts/delivery-version-check.sh` | 正确报告缺失 helm/kubeconform，exit 1（工具未安装） |
| `scripts/delivery-release-check.sh --env kind` | exit 1，报告全零 digest/REPLACE_ME（占位符合预期，未伪称可发布） |
| `scripts/delivery-secret-scan.sh` | clean tree rc0；注入假 secret 的反向控制 rc1 |

## 未执行 / 残余风险

- `helm lint`/`helm template`/`kubectl apply --dry-run=server`/kind/k3d bootstrap/ArgoCD Synced-Healthy/port-forward smoke/backup-restore/rollback/IdP 负向矩阵全部未执行。
- Release manifest digest 为占位；`repoURL`/`targetRevision` 为占位，必须由实际仓库替换。
- 因此 production-delivery 任务除 1.2、1.4 外均保持未勾选；AC-08/AC-10 只能由 kind/k3d 与 staging-like 证据判定，当前 `not passed`。

## kind 脚本执行验证（2026-11-23，stage-2-integration 2.8）

- `KIND_CLUSTER_NAME=ontoaxis-kind-test KIND_REGISTRY_PORT=5055 ./deploy/kind/kind-up.sh` exit 0：创建 1 control-plane + 2 worker，CNI/StorageClass/registry attach/kubeconfig 成功。
- 备注：默认 5000 端口被占用时需用 `KIND_REGISTRY_PORT` 覆盖；脚本本身可用。
- Helm/ArgoCD 应用与备份/恢复/回滚未在本轮执行（需要已部署工作负载与镜像）。

## Helm 实跑（2026-11-23）
- `helm version --short` = v4.3.2+g8346aaa
- `helm lint deploy/helm/ontoaxis-{platform,data,app,observability,jobs}` 全部 `1 chart(s) linted, 0 chart(s) failed`（仅 icon 建议）
- `bash scripts/delivery-render-check.sh` = PASS：5 chart × 4 overlays 共 20 组确定性渲染，YAML 20 文件通过（ruby fallback）。
- 结论：task 2.1、2.5 已验证；kubeconform 未安装，2.6 仍只完成重复渲染部分。

## 镜像 digest 契约（2026-11-23）
- `helm template t deploy/helm/ontoaxis-app --set components.ontoaxis-core.image.digest=` → exit 1（required digest）
- `helm template t deploy/helm/ontoaxis-app --set components.ontoaxis-core.image.repository=ghcr.io/ontoaxis/core:latest` → exit 1（拒绝 :latest）
- `helm template t deploy/helm/ontoaxis-app` → exit 0，渲染出 4 个 `image: ...@sha256:` 引用。
- 结论：task 2.3 已验证。

## kubeconform 与 Make 目标（2026-11-23）
- `kubeconform` 0.8.0 已安装；`bash scripts/delivery-render-check.sh` 对 20 组渲染全部 `PASS kubeconform`，RESULT PASS。
- 新增 `make delivery-lint` / `make delivery-render` / `make delivery-render-check`；`make delivery-lint` 5/5 通过，`make delivery-render-check` PASS。
- 结论：task 2.6、2.7 已验证。

## 必填 Secret 引用校验（2026-11-23）
- 用 values 覆盖 `components.ontoaxis-core.secretRefs[0].secretName=""` 后 `helm template` 失败：
  `ontoaxis: ontoaxis-core secretRef ONTOAXIS_DATABASE_URL secretName is required`
- 缺失 `key` 同样由 deployment.yaml `required` 拒绝。
- 结论：task 2.4 已验证。

## 工具链版本（2026-11-23）
- 安装/确认：helm 4.3.2、kubectl 1.29.2、kind 0.33.0、k3d 5.9.0、kubeconform 0.8.0、gitleaks 8.30.1；argocd CLI 本地可选未安装。
- `scripts/delivery-version-check.sh` → `RESULT: PASS (required present) — 1 optional command(s) missing`。
- `deploy/README.md` 版本表已更新为实测版本；task 1.3 已验证。

## Helm template + server dry-run（2026-11-23）
- 重建/恢复 kind 集群 `ontoaxis-edge`（v1.37.0，Ready），`kind export kubeconfig` 成功。
- 安装 ESO v0.9.11 CRD bundle；创建 `oa-system/oa-app/oa-data/oa-observability` 命名空间。
- 5 个 chart 以 `deploy/values/kind.yaml` 渲染后执行 `kubectl apply --dry-run=server` → **exit 0**，所有 Deployment/Service/PDB/ExternalSecret 均 server dry-run 通过。
- 结论：task 2.2 已验证。

## 密钥引导脚本（2026-11-23）
- 无环境变量运行 `scripts/delivery-secrets-bootstrap.sh` → exit 1，列出缺失键名（不打印值）。
- 提供全部键的本地占位值后运行 → exit 0，在 `external-secrets/ontoaxis-bootstrap` 创建 Secret，键数量与脚本清单一致；随后删除测试 Secret。
- `.env.kind.example` 只含键名；`.env.kind` 由 `.gitignore` 覆盖。task 4.3 已验证。

## ESO 安装（2026-11-23）
- `deploy/kind/bootstrap.sh` 固定 `ESO_VERSION`，Helm 安装 `external-secrets/external-secrets` 到 `external-secrets` 命名空间并 `--set installCRDs=true`；另提供 bundle 回退。
- kind 集群：`kubectl -n external-secrets get pods` 三个 Pod（external-secrets、cert-controller、webhook）均 `1/1 Running`（安装版本 0.9.11）。
- 运行 `ESO_VERSION=v0.9.11 SKIP_ARGOCD=1 SKIP_ENVOY=1 SKIP_APPS=1 deploy/kind/bootstrap.sh` 已执行到密钥引导阶段（因未提供 .env.kind 值退出），ESO 部分成功。
- 结论：task 4.1 已验证。

## 资源画像与 PDB 推导（2026-11-23）
- `docs/operations/delivery-runbook.md` 新增 kind / kind-small / staging-like 的副本、requests/limits、PDB minAvailable 对照表。
- 数值与 `deploy/values/{kind,staging-like}.yaml` 一致；PDB 推导规则 `max(1, replicas-1)` 已记录。task 3.5 已验证。

## ESO 密钥同步（2026-11-23）
- 发现并修复 `deploy/argocd/secrets/{kind,staging-like}/10-externalsecrets.yaml`：ESO kubernetes provider 的 `remoteRef` 需 `key: ontoaxis-bootstrap` + `property: <flat-key>`，原写法把 flat key 当作 Secret 名导致 `SecretSyncedError`。
- 创建本地 bootstrap Secret 后 apply `deploy/argocd/secrets/kind/`：5 个 ExternalSecret 全部 `SecretSynced / READY=True`。
- 目标 Secret：`oa-app/ontoaxis-core-secrets` 3 keys、`oa-system/ontoaxis-keycloak-secrets` 3 keys、`oa-system/ontoaxis-openfga-secrets` 1 key、`oa-data/ontoaxis-data-secrets` 5 keys、`oa-data/ontoaxis-backup-secrets` 2 keys。task 4.2 已验证。

## 密钥清单一致性（task 4.4，2026-11-23）
- 修复 ESO 目标 Secret 的 key 名与 chart `secretKeyRef` 不一致：core `database-url/meilisearch-master-key`、keycloak `admin-password`、data `postgres-password/s3-access-key/s3-secret-key/trino-credentials`、openfga `datastore-uri/preshared-key`、backup `postgres-database/postgres-user/postgres-password`。
- 扩展 `delivery-secrets-bootstrap.sh` 与 `.env.kind.example`：新增 `trino-credentials`、`openfga-datastore-uri`、`postgres-database`、`postgres-user`。
- 新增 `scripts/delivery-secret-inventory-check.sh`：渲染 chart 提取全部 `secretKeyRef`，核对目标 Secret 键；解析 kind ExternalSecret 的 `remoteRef.property`，核对 bootstrap Secret。
- 运行结果：`secretKeyRef entries: 12; ESO properties: 13` → `OK`。5 个 ExternalSecret 均 `SecretSynced / READY=True`。task 4.4 已验证。

## 缺失密钥行为（task 4.5，2026-11-23）
- 从 `external-secrets/ontoaxis-bootstrap` 删除 `openfga-token` 后 force-sync：
  - `ontoaxis-openfga` ExternalSecret 变为 `SecretSyncedError / READY=False`
  - 事件 `property openfga-token does not exist in data of secret "ontoaxis-bootstrap"`，不含任何密钥值
- 恢复 bootstrap Secret 后 force-sync → `SecretSynced / READY=True`。
- 结论：task 4.5 已验证（缺失键 fail-closed，不回退默认值）。

## 密钥扫描（task 4.7，2026-11-23）
- `scripts/delivery-secret-scan.sh` 全仓扫描：`gitleaks: no leaks found`。
- 负向夹具：临时 git 仓库含高熵 AWS key → `gitleaks: leaks detected (exit 1)`，脚本 exit 1。
- `.github/workflows/ci.yml` 新增 `secret-scan` job（`gitleaks/gitleaks-action@v2`）。
- 结论：task 4.7 已验证。

## kind 集群启停（task 6.1，2026-11-23）
- `deploy/kind/kind-up.sh`（`KIND_REGISTRY_PORT=5055`）：1 control-plane + 2 workers、`ontoaxis-registry`，kubeconfig 写入 `dist/kind/kubeconfig`，exit 0。
- 重复执行：识别现有集群与 registry，复用且 exit 0（幂等）。
- `deploy/kind/kind-down.sh`：删除集群与 registry 容器，`kind get clusters` 只剩既有 `ontoaxis-edge`，无 `ontoaxis-kind`/`ontoaxis-registry` 残留。
- 结论：task 6.1 已验证。

## 镜像构建与 digest（task 6.2，2026-11-23）
- 修复 `scripts/delivery-images.sh` 的 web 构建上下文：`web/Dockerfile` 的 `COPY web/...` 需要仓库根上下文，原 `$ROOT/web` 导致 `"/web/nginx.conf": not found`；改为 `$ROOT`。
- 本地 kind registry `localhost:5055`；构建并推送 `ontoaxis-core`/`ontoaxis-web`。
- `dist/delivery/digests.env`：
  - CORE_DIGEST=`sha256:a6485178874d5be55089bae0386133077827575ad9ddd04bc8372fc5008c6f34`
  - WEB_DIGEST=`sha256:9c1a30ccacca04176e7529cc1e693dd019f23c3673276830d5d57a0e34da6dbb`
- `docker inspect RepoDigests` 与上述 digest 一致；`deploy/releases/kind.yaml` 的 core/web 条目已写入 repository/digest/commit。task 6.2 已验证。

## 跨 schema 回滚桌面推演（task 5.10，2026-11-23）
- `docs/operations/delivery-runbook.md` 新增“跨 schema 回滚流程”：冻结写入、schema 版本比对、备份恢复到独立实例、旧镜像部署、校验、恢复写入、禁止 down migration。
- 引用 `delivery-restore.sh` / `delivery-rollback.sh` / `delivery-smoke.sh`；真实生产步骤标注 `not executed`（无生产集群/备份）。task 5.10 已验证（桌面推演）。

## 可观测配置同步与漂移检查（task 2.8，2026-11-23）
- 新增 `scripts/delivery-sync-observability.sh`：将 `deploy/observability/{prometheus/prometheus.yaml,prometheus/rules.yaml,alertmanager/alertmanager.yaml,otel-collector.yaml}` 同步到 `deploy/helm/ontoaxis-observability/files/`，每文件带 `# digest: sha256:...` 与来源头。
- 新增 `scripts/delivery-config-check.sh`：校验 digest 头与正文；并检查 `deploy/argocd` 中存在 `deploy/edge/generated` Directory 引用。
- 漂移测试：修改 `deploy/observability/prometheus/rules.yaml` 后 `delivery-config-check` exit 1（DRIFT）；恢复并重新同步后 exit 0（OK）。
- `make delivery-config-check` / `make delivery-sync-observability` 已加入。task 2.8 已验证。

## tasks 3.1/3.3/3.4 静态校验
- `helm template` 每个 chart + `deploy/values/kind.yaml`：platform 5/5、data 7/7、app 4/4、observability 5/5 Deployment 均含 readinessProbe（helpers 同时定义 liveness/startup；jobs 为 CronJob 无探针）。
- deployment.yaml 渲染 `terminationGracePeriodSeconds`、`preStop`（HTTP 组件 sleep）；values.yaml 各组件设置。
- PDB：platform 5、data 7、app 4、observability 5，均 `minAvailable`；kind 单副本时 PDB 不阻塞（任务 3.4 静态验证）。
- 说明：未在真实集群观察 OOMKilled/探针失败事件；3.2 保持未勾选。

## task 7.3 恢复脚本负向演练（静态/本地）
- 案例1：manifest sha256 全 0 → `delivery-restore.sh` exit 1，`refusing corrupted dump: sha256 mismatch`，未创建 oa-restore。
- 案例2：dump 截断/篡改 1 字节 → exit 1，sha256 mismatch，未创建 oa-restore。
- 说明：正向恢复演练（真实备份→恢复→行数/RPO/RTO）仍需集群，task 7.2/7.4 保持未勾选。

## task 7.5 未覆盖范围记录
- `docs/operations/delivery-runbook.md` §9 已列明对象存储、Meilisearch、Kafka/Debezium、Kestra/Temporal、Trino/Nessie 为可重建状态，只有 Postgres 纳入备份；observability 历史不在备份范围。

## task 10.2 deploy README
- `deploy/README.md` 增加“生产化交付（Stage 2）”入口：Helm charts/values、静态检查、kind bootstrap、ArgoCD、promote/rollback、restore、smoke/diag，并保留 Compose 与回滚说明；命令与 scripts/Makefile 一致。

## task 9.2 delivery-diag.sh 注入故障定位
- 在 kind `oa-app` 注入 `diag-injected` Pod（不存在镜像 `localhost:5055/does-not-exist:bad`），执行 `DIAG_NAMESPACES='oa-app' scripts/delivery-diag.sh`。
- 产物 `/tmp/diag-test2`：`pods.txt` 显示 `ErrImagePull`；`events.txt` 含 `Failed to pull image... not found`、`ErrImagePull`、`ImagePullBackOff`；`describe.txt` 含该 Pod。
- 脚本不采集 Secret；对 password/token/secret/authorization 等键值做 `<redacted>` 替换，扫描无明文凭据。
- 注入 Pod 与临时 ns 已删除。

## task 6.5 k3d 路径
- `K3D_CLUSTER_NAME=ontoaxis-k3d-test K3D_REGISTRY_HOST=ontoaxis-k3d-registry.localhost K3D_REGISTRY_PORT=15050 deploy/kind/k3d-up.sh` 创建成功：1 server + 2 agents（`k3d cluster list` 显示 `1/1 2/2 true`），附带本地 registry。
- 集群内无 Traefik Pod（`get pods -A | grep traefik` 为空），符合 `--disable=traefik@server:0`。
- `deploy/kind/k3d-down.sh` 删除 cluster、network、volume、registry，输出 `k3d teardown complete`。
- 注：kubectl 通过 k3d 写入的 host.docker.internal kubeconfig 连接返回 EOF，未影响创建/销毁结论。

## task 9.3 delivery-evidence.sh
- 新增 `scripts/delivery-evidence.sh`：读取 `--smoke` JSON 的 environment/overall/checks[] 与 `--diag` 目录，输出 Markdown 表格到 `--out`（默认 `docs/stages/evidence/stage-2-production-delivery.md`）。
- 测试：用 2 项 smoke checks（edge-200、readyz passed）生成表格，字段 environment/overall/generated_at 与 evidence 路径正确；diag 路径写入脚注。

## task 7.1 备份 CronJob 与 manifest
- `helm template ontoaxis-jobs deploy/helm/ontoaxis-jobs -f deploy/values/kind.yaml | kubectl apply -f -`：CronJob `ontoaxis-backup` 创建成功，`schedule: "0 2 * * *"`、`concurrencyPolicy: Forbid`、`backoffLimit: 0`、`activeDeadlineSeconds: 3600`。
- `kubectl -n oa-data create job --from=cronjob/ontoaxis-backup backup-manual-test` 成功产生 Job `backup-manual-test`；Pod 因缺少 `ontoaxis-backup-secrets` 处于 Pending，但 Job/Pod manifest、共享 PVC `ontoaxis-backups`、ServiceAccount 均已验证。
- 备份脚本包含 `pg_dump -Fc`、sha256、size、schema_version、pg_version、manifest.json 与 retention 清理逻辑。
- 测试后删除 Job/CronJob/PVC/ExternalSecret/ConfigMap/ServiceAccount 与临时 `oa-data` namespace。

## task 8.1 staging OIDC 值 + staging web 镜像
- `deploy/values/staging-like.yaml`：`issuer: https://auth.staging.ontoaxis.example/realms/ontoaxis`（非 localhost）、`audience: ontoaxis-api`、`jwksUrl: https://auth.staging.ontoaxis.example/realms/ontoaxis/protocol/openid-connect/certs`。
- `helm template ontoaxis-app ... -f deploy/values/staging-like.yaml` 渲染出 `ONTOAXIS_OIDC_ISSUER=https://auth.staging.ontoaxis.example/realms/ontoaxis`、`ONTOAXIS_OIDC_AUDIENCE=ontoaxis-api`、`ONTOAXIS_OIDC_JWKS_URL=.../certs`；无生产密钥 env。
- 同 commit `a04ab4c` 另构建 staging web 镜像（`VITE_OIDC_AUTHORITY=staging`）：image id `sha256:0c41aa6c8ea0587f4a51160aabecf868268ef5c0a598ed157c015fc85be5bb35`；kind web 镜像 id `sha256:d2aaa67a55c9d38957cd92fef1f0a4f3fcb5db74be72a294c628044435f4bf0b`，digest 不同。

## task 8.2 claims 映射验证
- discovery `http://localhost:18083/realms/ontoaxis/.well-known/openid-configuration` 返回 issuer/token_endpoint；与本地 realm 契约一致。
- 用 `client_id=ontoaxis-web` 获取测试用户 token（admin），仅解码 payload、未记录原文：
  - `aud=ontoaxis-api`、`azp=ontoaxis-web`、`sub` 为 UUID
  - `tenant_id` string（Keycloak Organization ID）present
  - `org_ids` list[string] = `["11111111-1111-4111-8111-111111111111"]`
  - `workspace_ids` 缺省（v1 可空）
  - `roles` list[string] = `["tenant-admin","member","platform-admin"]`
  - `realm_access.roles` 亦存在
- fallback 决定：外部 staging IdP 必须复现同名/同类型 mapper（`tenant_id`、`org_ids`、`workspace_ids`、`roles`、audience `ontoaxis-api`）；否则 staging 部署前必须走 `delivery-idp-check.sh` 并记录 `IDP_CLAIMS_FALLBACK` 决策。

## Local IdP check (not kind staging-like; task 8.3 not ticked)
- `scripts/delivery-idp-check.sh --issuer http://localhost:18083/realms/ontoaxis --core-url http://127.0.0.1:18080`:
  - discovery issuer/endpoints/alg: PASS
  - JWKS keys with kid: PASS
  - controlled test token accepted by core read-only API: PASS (HTTP 200)
- Negative cases not executed (no wrong-audience/expired/bad-kid tokens available); 8.4 remains open.
- Task 8.3 verification requires kind staging-like, so not ticked.

## kind bootstrap (task 6.3)
- `deploy/kind/bootstrap.sh` run on context `kind-ontoaxis-kind` (3 nodes), no `.env.kind` initially -> failed with missing key names (expected); after creating git-ignored `.env.kind` with local-only values, rerun succeeded.
- Installed: namespaces `oa-system/oa-app/oa-data/oa-observability`, ArgoCD (6 deployments in `argocd`), ESO (3 deployments in `external-secrets`), Envoy Gateway (GatewayClass `eg` Accepted), bootstrap Secret, AppProject `ontoaxis`, root Application `ontoaxis-root`.
- `kubectl get ns` shows all four target namespaces Active; `kubectl get application` shows `ontoaxis-root` created (initially Unknown/Unknown while child apps sync).

## AppProject destination fix (before task 5.1)
- Root Application lives in namespace `argocd`; AppProject initially allowed only `oa-system/oa-app/oa-data/oa-observability`, so ArgoCD reported `InvalidSpecError: destination ... namespace 'argocd' do not match any of the allowed destinations`.
- Added `argocd` namespace destination to `deploy/argocd/projects/ontoaxis.yaml`; applied to `kind-ontoaxis-kind`.
- `kubectl get appproject ontoaxis` shows the new destination. Controller still reports the old condition because its Redis cache is unreachable (`dial tcp 10.96.132.126:6379: i/o timeout`); 5.1/5.2 remain unticked pending controller recovery.

## AppProject verification (task 5.1)
- `argocd proj get ontoaxis` inside `argocd-server` shows destinations oa-system/oa-app/oa-data/oa-observability/argocd, source repo, cluster-resource whitelist, and namespace blacklist (Secret/Event).
- Negative: created test Application `dest-test` targeting namespace `kube-system`; controller reported `application destination ... namespace 'kube-system' do not match any of the allowed destinations in project 'ontoaxis'`; Application deleted.
- Note: `argocd` destination was added so the app-of-apps root Application itself is valid; it does not grant workload deployment into argocd beyond Application/AppProject objects.
