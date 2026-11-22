# Design: production-delivery

## Context

Stage 2 的 Wave 0/Wave 1 已提供 Compose 本地栈、`oa-edge` 路由生成与基础观测资产；`deploy/compose/` 是本地唯一入口，`deploy/edge/generated/` 是 Envoy 路由/策略真源，`deploy/observability/` 是 Prometheus/OTel/Alertmanager 配置真源。ARCHITECTURE §11 已定义 `oa-system` / `oa-app` / `oa-data` / `oa-observability` 命名空间布局，§14.7 要求 Helm chart 分组、ArgoCD App-of-Apps、CI 含 helm lint，§15.2 把生产化交付与生产 IdP 列为 P2 并明确落在本 change。

当前缺口：没有 chart、没有 GitOps、没有 kind/k3d 安装路径、没有 probes/resources/PDB、没有外部密钥注入、没有备份/恢复与镜像回滚演练、没有非本地 IdP 的 issuer/JWKS/claims 验证。Stage 2 brief 同时禁止真实云账号、生产密钥、真实客户数据、多区域 HA/DR 与生产容量规划；Stage 2 change map 规定本 change 拥有 `deploy/helm/`、`deploy/argocd/`、`deploy/compose/` 的写入范围。

前置约束：Stage 2 Wave 1 的 G1 门禁当前没有 `pass` 记录，本 change 的实现任务必须等门禁满足后再执行（规划产物可先行评审）。AC-08/AC-10 的证据只能来自 kind/k3d 与受控 staging-like 环境，prod-like 只做渲染与本地 profile 验证，真实云 provider 标为未执行。

## Goals

1. 用 Helm 以确定、可审计的方式渲染全部运行时组件，镜像按 digest 固定。
2. 用 ArgoCD app-of-apps 实现 GitOps 同步、sync wave 顺序与漂移收敛。
3. 提供 kind 与 k3d 的可重复安装/卸载、镜像加载与失败诊断。
4. 为长期运行组件配置探针、requests/limits、优雅终止与 PDB。
5. 用 External Secrets Operator 从外部密钥源注入凭据，仓库零明文密钥。
6. 提供可校验的备份/恢复演练与损坏备份拒绝路径。
7. 提供 release manifest、坏部署回滚演练与跨 schema 回滚说明。
8. 验证非本地 OIDC issuer、JWKS、audience 与 claims 映射。
9. 提供交付烟测、失败证据采集、环境晋升流程与受控环境边界。
10. 复用 `deploy/edge/generated/` 与 `deploy/observability/`，不产生第二份真源。

## Non-Goals

- 不连接真实云账号、不提交生产密钥、不使用真实客户数据；不做生产切换或生产发布。
- 不做多区域 HA/DR、容量规划、自动扩缩容、托管服务选型（Stage 2 brief 明确排除）。
- 不提供公网 TLS 证书、真实 DNS、生产 LoadBalancer/Ingress；不验证真实外部 secret provider（Vault/AWS SM）连通性。
- 不备份对象存储与 Meilisearch（通过重建/重新索引恢复），不备份 Kafka/Kestra/Temporal 状态（Stage 2 视为可重建）。
- 不执行 down migration、不手工修改迁移历史、不改变业务 API/授权判定逻辑。
- 不用 Argo Rollouts/金丝雀/蓝绿替换现有 Deployment 发布方式。
- 不替换 Compose 作为本地开发主入口。

## Target Topology

| 命名空间 | 工作负载 | 说明 |
| --- | --- | --- |
| `argocd` | ArgoCD（upstream 安装，bootstrap 脚本管理） | GitOps 控制器；不放入 `oa-*` 命名空间 |
| `external-secrets` | ESO controller/webhook | 密钥同步；bootstrap 安装 |
| `oa-system` | Envoy Gateway、Keycloak、OpenFGA | 入口、身份、授权；对应 ARCHITECTURE §11 |
| `oa-data` | PostgreSQL、Kafka、Debezium、Apicurio、Kestra、Temporal、Trino、Nessie | 权威数据与数据平面 |
| `oa-app` | ontoaxis-core、web、Meilisearch、Valkey、SeaweedFS | 应用与支撑存储 |
| `oa-observability` | OTel Collector、Prometheus、Alertmanager、Jaeger、Perses | 观测；复用既有配置 |
| `oa-restore` | 临时 PostgreSQL + 恢复校验 Job | 仅恢复演练期间存在，结束后删除 |

说明：

- PostgreSQL 归入 `oa-data`。ARCHITECTURE §11 列表未显式列出 PostgreSQL，此处按"权威数据在数据命名空间"收敛；偏离记录在 `deploy/README.md` 与本 design 中，`docs/architecture/runtime-topology.md` 的同步由 `stage-2-integration` 在阶段文档更新时处理。
- 集群级 GatewayClass 与 Envoy Gateway 控制面由 bootstrap 脚本安装一次（复用 `deploy/edge/kind/install-envoy-gateway.sh`），ArgoCD 只管理命名空间内 workload。
- 服务端口沿用 `docs/architecture/services-and-ports.md`：core 8080/9090、web 80、Postgres 5432、Keycloak 8080、OpenFGA 8080、Trino 8080、Meilisearch 7700、Valkey 6379、SeaweedFS 8333；Prometheus 9090、OTel 4318、Jaeger 16686、Alertmanager 9093。

## Chart and Values Layout

```text
deploy/helm/
  ontoaxis-platform/      # postgres, keycloak, openfga, valkey, seaweedfs
  ontoaxis-data/          # kafka, debezium, apicurio, kestra, temporal, trino, nessie
  ontoaxis-app/           # ontoaxis-core, web, meilisearch
  ontoaxis-observability/ # otel-collector, prometheus, alertmanager, jaeger, perses
  ontoaxis-jobs/          # backup CronJob、restore/smoke 模板（test-only 值）
deploy/values/
  kind.yaml               # 1 副本 + 低资源 + 本地 registry + port-forward 烟测
  kind-small.yaml         # CI 用，裁剪重负载（Kafka/Kestra/Temporal 单副本或禁用）
  staging-like.yaml       # 非本地 issuer、2 副本关键组件、自动同步
  prod-like.yaml          # 仅渲染验证；手动同步；外部 secret provider 仅声明
deploy/releases/
  kind.yaml               # digest/commit/chartVersion 发布记录
  staging-like.yaml
  prod-like.yaml
deploy/argocd/
  projects/ontoaxis.yaml  # AppProject（repo/destination 白名单）
  root-app.yaml           # app-of-apps 根 Application
  apps/                   # 各 lifecycle 的 Application
  secrets/                # ExternalSecret / ClusterSecretStore（仅引用）
deploy/kind/              # kind/k3d 安装、镜像、诊断、卸载脚本
scripts/delivery-*.sh     # 烟测、晋升、回滚、备份/恢复、证据、密钥扫描
```

取值原则：

- 每个 chart 一个 `values.yaml` 默认值 + 环境覆盖文件；chart 用 `required` 校验必填项：`image.digest`（格式 `sha256:[0-9a-f]{64}`）、namespace、外部 secret 名称、环境名。
- 环境名限定为 `kind` / `kind-small` / `staging-like` / `prod-like`；未声明环境直接渲染失败，避免"默认值误部署"。
- 镜像以 `repository@sha256:...` 渲染，`pullPolicy: IfNotPresent`；禁止 `latest`、禁止只有 tag 没有 digest。
- 渲染确定性：chart 内不使用随机函数/时间戳；`scripts/delivery-render-check.sh` 对每个 chart×环境渲染两次并比较 sha256，再跑 kubeconform（`-ignore-missing-schemas` 忽略 CRD），输出到 `dist/helm-render/` 不入库。
- 选择自研最小 chart 而非上游社区 chart：镜像版本已在 Compose 固定、许可证白名单安全、无 chart repo 网络依赖、单一 values 契约；替代方案（Bitnami/upstream chart）因镜像来源漂移、License/维护风险与版本锁定困难被拒绝。
- 选择"每个生命周期一个独立 chart + App-of-Apps 组合"而非 umbrella chart：ArgoCD 可独立同步/回滚某一组，wave 顺序更直观；umbrella 会把所有组件耦合进一个 Application，失败定位与选择性同步都更差。

## ArgoCD Sync and Promotion

- `AppProject ontoaxis`：限制 repo URL、允许的 destination 命名空间（`oa-*`、`external-secrets`、`oa-restore`、`argocd` 除外）与 clusterResourceWhitelist（Namespace、GatewayClass 等仅 bootstrap 使用）。
- 根 Application `deploy/argocd/root-app.yaml` 指向 `deploy/argocd/apps/`（Directory recurse），子 Application 使用 sync wave：
  - `-10`：命名空间、ResourceQuota/LimitRange、ServiceAccount/RBAC；
  - `-5`：ExternalSecret/ClusterSecretStore（密钥先于工作负载）；
  - `0`：platform；
  - `1`：data；
  - `2`：app（core/web/meilisearch），core 迁移在启动时执行，故等 platform 就绪后再起；
  - `3`：edge（`deploy/edge/generated/` 的 Directory Application，Controller 安装由 bootstrap 负责）；
  - `4`：observability；
  - `5`：jobs（backup CronJob；smoke Job 以 `Hook: PostSync` 或手动 Job 形式存在）。
- 同步策略：kind/staging-like 使用 `automated: {prune: true, selfHeal: true}`；prod-like 不开启 automated，需要显式 sync。`retry` 有限次（5 次退避），`PruneLast` 用于 jobs，`ServerSideApply` 用于较大的 CRD 清单。
- 成功判定：所有 Application `Synced` + `Healthy`；Deployment 的 health 由 ArgoCD 内置判断，Job 以完成状态判断。失败时保留 `OutOfSync`/`Degraded` 与事件，烟测必须失败，不写成功证据。
- 漂移检测：ArgoCD 默认定时比较；演练用手工 `kubectl patch` 制造漂移，验证 self-heal 收敛并记录事件；`scripts/delivery-drift-check.sh` 输出 `argocd app diff` 摘要作为证据。
- 晋升：`scripts/delivery-promote.sh --from <env> --to <env> --digest <digest>`：
  1. 读取来源环境的烟测结果 JSON 与 release manifest，要求 `passed: true`；
  2. 校验 digest 存在（本地 registry manifest 或 OCI digest 文件）；
  3. 更新目标环境 values 中的 digest 与 `deploy/releases/<to>.yaml`，运行渲染检查；
  4. 生成一条本地提交（不自动 push；评审后由人推送，staging-like 也可由 CI PR 完成）；
  5. 目标环境 ArgoCD 同步（prod-like 由人点击 sync）。
- 回滚：`scripts/delivery-rollback.sh --env <env> --to <release>` 使用上一 release manifest 的 digest 写回 values → 提交 → 同步 → 等 Healthy → 烟测 → 写回滚标记。Git revert 是等价路径，脚本只是把它自动化并记录证据。

## Secret Strategy

- 使用 External Secrets Operator（`external-secrets` 命名空间，Helm 安装，版本固定并在 `deploy/README.md` 记录）。
- 每个环境一个 `ClusterSecretStore`：
  - kind / staging-like：`kubernetes` provider，读取 `external-secrets` 命名空间中的 bootstrap Secret `ontoaxis-bootstrap`；该 Secret 由 `scripts/delivery-secrets-bootstrap.sh` 从 git-ignored 的 `.env.kind`/环境变量创建，绝不入库。
  - prod-like：仅声明 `vault`/`aws-sm` 形态的 Store 模板（`secretStoreRef` 指向外部 provider），Stage 2 **不连接、不验证**，证据标记 `not executed`。
- ExternalSecret 命名与键路径约定：`ontoaxis/<env>/<component>/<key>`；目标 Secret 名称固定为 `ontoaxis-core-secrets`、`ontoaxis-keycloak-secrets`、`ontoaxis-data-secrets`、`ontoaxis-openfga-secrets`、`ontoaxis-backup-secrets`，chart 只引用名称与键名。
- 必须注入的密钥（覆盖面由 `deploy/argocd/secrets/README.md` 列出）：数据库口令、Keycloak bootstrap admin 与 OIDC client secret、OpenFGA token、Meilisearch master key、Trino 凭据、SeaweedFS/S3 access/secret、备份口令。
- 生效方式：`refreshInterval: 1h` + `creationPolicy: Owner`；启动时读取 env 的组件（core、部分 data 组件）在轮换后由 runbook 触发滚动重启，支持热加载的组件（如 OIDC JWKS 由运行期拉取）免重启。缺失 Secret 时 ESO 状态为 `SecretSyncedError`，Pod 保持 Pending/未就绪，chart 不提供任何默认口令。
- 防泄漏：`gitleaks`（CI 必需）+ `scripts/delivery-secret-scan.sh`（扫描 values 中 `password|secret|token|dsn` 等键是否出现字面值，要求只在 `secretKeyRef`/`remoteRef` 中出现）；证据采集器对 token、口令、connection string 凭据做占位替换。
- 替代方案：Seald Secrets（需要管理私钥与轮换，Stage 2 成本高）、直接提交加密 Secret（仍违反零明文密钥与轮换要求）、`kubectl create secret` 手工流程（不可 GitOps 复现）——均被拒绝。

## Backup and Restore

- 权威状态 = PostgreSQL。备份 CronJob `ontoaxis-backup`（`deploy/helm/ontoaxis-jobs`，`oa-data` 命名空间）：
  - 每日 `0 2 * * *`，支持 `kubectl create job --from=cronjob/ontoaxis-backup` 手动触发；
  - `pg_dump -Fc` 输出到 PVC `ontoaxis-backups`（kind 用 local-path，单节点），文件名 `<UTC timestamp>/ontoaxis.dump`；
  - 同目录 `manifest.json`：`backup_id`、`created_at`、`sha256`、`size_bytes`、`schema_version`（读 `goose_db_version` 的最大 version_id）、`image_digest`、`pg_version`；
  - 保留最近 7 份；失败时 Job 非零退出并保留事件。
- 恢复流程 `scripts/delivery-restore.sh --backup <dir>`：
  1. 校验 manifest 字段齐全、`sha256` 匹配，否则拒绝；
  2. 创建干净 `oa-restore` 命名空间与独立 PostgreSQL；
  3. `pg_restore` 到全新库；
  4. 校验 `goose_db_version` 与 manifest 一致、代表性表行数（tenants/objects/datasets 等由脚本参数指定）；
  5. 以恢复库为 DSN 启动一个 core 实例，检查 `/readyz` 与只读 API；
  6. 输出校验结果与实测耗时；结束后删除命名空间（`--keep` 可保留供排障）。
- 验证分级：L1 校验和/manifest → L2 恢复成功/schema 版本 → L3 代表性行数 → L4 core `/readyz` → L5 只读 API smoke（需要 staging token；不可用时标 `not passed`，不伪造）。
- 负向演练：复制备份并篡改 1 字节 → 校验和不匹配 → 拒绝；截断 dump → `pg_restore` 失败 → 恢复脚本非零并保留证据。
- 恢复目标：Stage 2 暂定目标 RPO ≤ 24h（每日备份）、RTO ≤ 60min（恢复实测）。这两个数字**仅用于演练记录与趋势对比**，未签核为生产承诺，证据中明确标注。
- 范围与 residual risk：Kafka/Kestra/Temporal/Trino/Nessie 在 Stage 2 视为可重建；对象存储与 Meilisearch **不在备份范围**，Meilisearch 由 `object-search-meilisearch` change 的重建/重新索引路径恢复，对象存储数据需重新导入；该缺口在证据与 runbook 中标记，不声称"完整备份"。
- 迁移策略（Stage 2 决定）：core 启动时由 `goose.UpContext` 自动迁移（见 `docs/architecture/migrations.md`），当前没有独立 migrate 子命令；因此 Stage 2 所有环境 core 副本数为 1，发布时使用 `Recreate` 策略，等 platform/data 就绪后再启动 core，避免多副本并发迁移。引入独立迁移 Job（`PreSync` hook）需要 core 增加 migrate 子命令（行为变更，另开 change），列为 open decision。

## Image Digest and Rollback

- 镜像构建：CI `docker` job 构建 core/web，tag 为 git short SHA；kind 环境推到本地 registry `localhost:5001`（`kind-registry` 容器，由 `kind-up.sh` 创建并接入 kind 网络），`docker push` 后用 `docker buildx imagetools inspect` 或 registry API 取 digest。所有 values 只写 digest。
- `deploy/releases/<env>.yaml` 记录：`environment`、`chartVersion`、每个组件的 `digest`、`commit`、`chart`、`created_at`、`operator`；由晋升/回滚脚本写入并与 values 做一致性检查（`scripts/delivery-release-check.sh`）。运行中 Pod 的 imageID 必须与 release manifest 一致，否则视为漂移。
- 发布记录生成后，digest 即不可变；禁止"重打同一 tag"。
- 坏部署演练（两种故障）：
  1. 缺失镜像：使用不存在 digest → `ImagePullBackOff` → ArgoCD `Degraded`/`ProgressDeadlineExceeded`；
  2. 就绪失败：`values-kind-bad.yaml` 把 core readiness path 覆盖为 `/not-ready`（chart 支持探针 path 覆盖，仅测试用）→ 滚动超 `progressDeadlineSeconds` → `Degraded`。
  两种都执行回滚脚本到上一 release，验证 Healthy + 烟测通过，并保留 ArgoCD 事件与 rollout 历史作为证据。
- 跨 schema 回滚：先比对 release manifest 的 `schema_version` 与运行库 `goose_db_version`；若目标镜像期望的 schema 早于已应用版本，禁止直接换镜像，改为"停止写入 → 恢复最近备份到独立实例 → 部署旧镜像 → 校验"，记录于 runbook。不执行 down migration，不改迁移历史。
- web 环境差异：`web/Dockerfile` 用构建期 `VITE_*` 注入 OIDC authority，不同环境需要各自构建（同 commit 不同 digest）。Stage 2 接受该差异并在 release manifest 记录每环境 digest；改为运行期配置注入属于前端行为变更，另开 change（open question）。

## Staging IdP

- 主路径：外部管理的 staging IdP（`https://auth.staging.ontoaxis.example/realms/ontoaxis`，非 localhost、TLS），由运维在受控环境提供；仓库只配置 issuer、audience（`ontoaxis-api`）、可选的 jwksUrl/discovery，以及 web 的 OIDC authority（构建期）。测试用户为合成账号，凭据只存在于 staging 密钥源，由脚本从环境变量/Secret 读取。
- 需要的 claims 映射（以现有 realm 为契约基线）：`tenant-id`、`org-ids`、`workspace-ids`、`realm-roles`；audience mapper 输出 `ontoaxis-api`。外部 IdP 必须提供等价 mapper，否则 staging 管理端需补齐——这是**外部依赖**，在 tasks 中作为前置确认项；不可得时使用 fallback 并标记 partial。
- 验证脚本 `scripts/delivery-idp-check.sh`：
  - discovery 文档与 issuer 一致；
  - 受控测试用户登录/token 获取；
  - 用 token 调 core 只读业务 API，验证 200 与身份上下文（租户/角色映射符合预期）；
  - 负向：非 `ontoaxis-api` audience 的 token → 401；来自其他 issuer（本地 Compose Keycloak）的 token → 401；过期 token → 401；错误签名/无 kid → 401；缺失映射 claims 的 token → 401/403；
  - JWKS 轮换：轮换 staging IdP realm 签名密钥（需外部管理权限）后，新 token 被接受、旧 token 在有效期内按缓存策略可用、服务不重启；无管理权限时标记 `not executed`。
- fallback：若外部 staging IdP 暂不可用，则在 kind/k3d 中部署独立的 staging-like Keycloak，使用非 localhost issuer + 自签 TLS（`deploy/kind/tls/` 生成临时证书，仅测试用），执行同样的验证；结果中 S2-AC-10 标 `partial` 并说明原因，不声称完成真实预发 IdP 验证。
- 本地 Compose Keycloak（`localhost:18083`）永远不产生 S2-AC-10 证据；烟测在本地环境会把该项写成 `not passed`。
- 安全评审：IdP 配置与 claims 映射属于 Stage 2 brief §7 要求的安全评审范围，评审记录作为证据的一部分（评审清单：issuer 白名单、audience 严格匹配、JWKS 来源、claim 名称固定、无生产密钥）。

## kind/k3d Test Flow

前置版本（写进 `deploy/README.md`，CI 与脚本检查）：Helm ≥ 3.16、kubectl ≥ 1.30、kind ≥ 0.25、k3d ≥ 5.8、ArgoCD CLI ≥ 2.13、kubeconform、gitleaks、jq。

1. `deploy/kind/kind-up.sh`：创建 kind 集群（1 control-plane + 2 workers，用于 PDB/drain 演练），启动 `kind-registry` 容器并接入 `kind` 网络，配置 containerd registry mirror（`localhost:5001`）；已存在时复用（幂等）。
2. `scripts/delivery-images.sh`：构建 core/web（tag=commit）、推本地 registry、解析 digest，写入 `dist/delivery/digests.env` 与 `deploy/releases/kind.yaml`。
3. `deploy/kind/bootstrap.sh`：namespaces、ArgoCD（Helm 固定版本）、ESO（Helm 固定版本）、Envoy Gateway（复用 `install-envoy-gateway.sh` + GatewayClass）；从 `.env.kind` 创建 `ontoaxis-bootstrap` Secret；应用 AppProject 与 root app。
4. `deploy/kind/verify.sh`：等待 Application Synced/Healthy、Pod Ready；跑 `scripts/delivery-smoke.sh --env kind`；失败时跑 `scripts/delivery-diag.sh` 自动采集诊断。
5. `deploy/kind/kind-down.sh`：删除 kind 集群与 registry 容器（`--keep-registry` 可保留）。
6. k3d：`deploy/kind/k3d-up.sh`（`--agents 2`、禁用 k3d 自带 traefik 避免与 Envoy Gateway 冲突、registry 用 k3d 管理或共用本地 registry），后续步骤复用同一组脚本；`k3d-down.sh` 对称。
7. 入口验证：Stage 2 统一用 `kubectl port-forward svc/<envoy-service> 18088:80` 做烟测，避免依赖 LoadBalancer/NodePort 与云 provider；kind/k3d 端口映射作为可选加速项，不作为证据前提。真实 LoadBalancer 验证属于 Stage 3。
8. CI 分工：`delivery-static`（每次必跑）做 helm lint/template、kubeconform、gitleaks、shellcheck/actionlint、`make edge-generate-check`；`delivery-kind`（workflow_dispatch 或定时）用 `kind-small` profile 做部署+烟测（GitHub 2C/7G runner 资源有限，不跑 Kafka/Kestra/Temporal 全量）；备份/恢复、回滚、IdP 轮换等长时演练在本地 kind/k3d 执行并提交证据，命令与耗时记录在 evidence 文件。
9. 失败诊断：`scripts/delivery-diag.sh` 采集 `kubectl get pods -A`、按时间排序的事件、未就绪 Pod 的 describe、日志尾部（脱敏）、registry manifest、ArgoCD Application 状态与条件；所有输出落入证据目录，脚本在安装失败时自动调用。

## Reuse of Edge and Observability

- Edge：`deploy/edge/generated/envoy-httproute.yaml` 仍是唯一路由/策略真源，ArgoCD Application `ontoaxis-edge` 以 Directory 方式直接 apply（sync wave 3），不做 chart 内拷贝；集群级 `GatewayClass` 与 Envoy Gateway 控制面由 bootstrap 安装。`make edge-generate-check` 继续在 CI 中防止生成物漂移。
- Observability：`deploy/observability/` 是 Prometheus/Alertmanager/OTel 配置真源。chart 通过 `scripts/delivery-sync-observability.sh` 把配置文件同步到 `deploy/helm/ontoaxis-observability/files/` 并以 ConfigMap 挂载；`make delivery-config-check` 检测同步物是否过期（与 edge-generate-check 同样的模式），避免第二份真源。替代方案（ArgoCD multi-source + kustomize 生成 ConfigMap）被拒绝：引入 kustomize 层与 Prometheus Operator 依赖，超出 Stage 2 范围。
- 观测契约不变：`runtime-observability` capability 的告警与指标契约继续有效，本 change 只改变部署载体。

## Evidence and Failure Handling

- 证据文件（源文件由本 change 提供，stage-2-integration 汇总到 G2）：`docs/stages/evidence/stage-2-production-delivery.md`，表格列：检查项、命令、结果、证据路径、环境、digest、时间；日志放 `docs/stages/evidence/logs/production-delivery/`（现有 `.gitignore` 允许该路径的日志入库）。
- 烟测输出 JSON：`delivery-smoke-<env>-<timestamp>.json`，字段 `environment`、`digest`、`started_at`、`checks[] {name, status: pass|fail|not passed, detail}`、`overall`。
- 失败分类与 runbook 条目：缺失/损坏镜像、Secret 未同步、Pod 未就绪/超时、迁移失败、数据库不可达、ArgoCD 同步失败/Degraded、备份校验失败、回滚失败、edge 路由 502。`docs/operations/delivery-runbook.md` 按 observability runbook 风格编写：入口表 → 每类故障的判定/诊断命令 → 修复 → 回滚 → 证据采集；并覆盖 ARCHITECTURE §14.7 要求的最小事故集（镜像 tag 缺失、init 脚本权限、迁移列缺失、网关 DNS 缓存、端口冲突）。
- 环境边界标注：所有证据显式写 `kind`/`k3d`/`staging-like`/`prod-like(render-only)`；需要真实云资源的步骤写 `not executed`；未实现项写 `not passed`，绝不写成 `pass`。

## Decisions

| # | 决策 | 理由 | 备选与拒绝原因 |
| --- | --- | --- | --- |
| D1 | 自研最小 Helm chart（按生命周期分组） | 镜像已固定、License 白名单、离线可渲染、values 契约可控 | 上游社区 chart：镜像来源/维护/版本漂移风险 |
| D2 | 独立 chart + App-of-Apps | 分组同步、独立回滚、wave 清晰 | umbrella chart：耦合、失败定位差 |
| D3 | ESO + `kubernetes` provider（kind/staging-like） | 可离线复现、仓库零密钥、prod-like 可替换 provider | Sealed Secrets（私钥管理）、明文 Secret（违规） |
| D4 | Edge 生成物用 ArgoCD Directory 直接 apply | 复用既有真源，避免拷贝漂移 | chart 内嵌拷贝：需同步步骤且易漂移 |
| D5 | Observability 配置同步进 chart + 漂移检查 | ConfigMap 挂载需要 chart 内文件，漂移检查可接受 | multi-source + kustomize：引入额外层次 |
| D6 | 镜像 digest + release manifest | 可审计、可回滚、可验证运行摘要 | tag 部署：不可复现、易被覆盖 |
| D7 | 备份只覆盖 PostgreSQL；分级校验 L1–L5 | Stage 2 权威状态是 DB，其他可重建 | 全量备份：范围外且成本高 |
| D8 | 同 schema 回滚换镜像；跨 schema 走恢复 | 迁移 forward-only，无 down migration | 执行 down migration：违反 migrations 契约 |
| D9 | core 副本 1 + Recreate，迁移在启动时执行 | 现有 core 无独立 migrate 命令，避免并发迁移 | 多副本并发启动：迁移竞争；独立 Job：需 core 新增命令（另开 change） |
| D10 | kind/k3d 烟测用 port-forward；CI 静态门禁 + 可选 kind job | 不依赖云 LoadBalancer，runner 资源可控 | 全量 kind CI：超时/资源不足风险 |
| D11 | 主路径外部 staging IdP，fallback 集群内 staging-like IdP | 满足 AC-10 本意，同时保留不可用时的可复核路径 | 只用本地 Compose Keycloak：不满足 AC-10 |
| D12 | staging-like 自动同步、prod-like 手动同步 | 开发效率与生产变更控制平衡 | 全部自动：prod-like 误变更风险 |

## Risks / Trade-offs

| 风险 | 影响 | 缓解 | 残余风险 |
| --- | --- | --- | --- |
| GitHub runner 资源不足跑全量栈 | CI 无法覆盖完整部署 | `kind-small` profile、静态门禁必跑、全量本地演练+证据 | 完整栈在 CI 中未持续验证 |
| 真实外部 secret provider/ArgoCD repo 认证未验证 | prod-like 路径未经真实环境验证 | 声明 provider 仅模板、证据标 `not executed` | prod 上线前需一次真实连通性验证 |
| staging IdP 管理与 mapper 依赖外部团队 | AC-10 无法完整验证 | 前置确认；fallback 集群内 staging-like IdP 标 partial | 外部 IdP 真实行为仍可能不同 |
| web 每环境构建镜像 | 同 commit 多 digest，提升可复现性风险 | release manifest 记录每环境 digest；运行期配置注入列为后续 change | 发布审计需按环境区分 |
| 对象存储/Meilisearch 未备份 | 灾难恢复不完整 | 重建/重新索引路径 + 证据标注 | Stage 3 需补对象存储备份 |
| PDB 在单副本/单节点下可能无意义 | 演练结论弱 | kind 2 workers + 关键组件 2 副本；记录 minAvailable 推导 | 真实集群拓扑差异 |
| Secret 轮换导致重启 | 单副本有短暂不可用 | runbook 规定滚动窗口；关键组件 2 副本 | 轮换窗口内可用性下降 |
| 迁移与回滚耦合 | 跨 schema 回滚需停机+恢复 | 明确 runbook 步骤、备份先行 | RTO 受备份恢复速度限制 |
| kind/k3d 依赖网络下载（ArgoCD/ESO/Envoy Gateway 镜像） | 离线/弱网失败 | 镜像版本固定、bootstrap 支持预加载镜像、失败诊断 | 首次安装时间波动 |
| 演练耗时长 | 迭代慢 | 脚本化 + 分层（快速烟测 vs 完整演练） | 人为跳过完整演练的风险 |

## Migration Plan

1. 前置：G1 门禁记录为 `pass`；确认 staging IdP 归属（或接受 fallback）；固定工具与依赖版本。
2. 静态资产先行：chart 骨架、values、渲染检查、CI 静态 job —— 不触碰 Compose 与运行时。
3. 逐步部署：namespaces → platform → data → app → edge → observability；每一步在 kind 验证就绪后再进下一步。
4. 演练：备份/恢复 → 坏部署回滚 → 漂移 self-heal → IdP 验证 → 跨 schema 回滚说明。
5. 文档与证据：delivery-runbook、deploy/README、evidence 文件、traceability 更新。
6. Compose 保持不变；本 change 不改业务代码，回滚方式为移除新增交付资产（对 Compose 无影响）。

## Open Questions

1. staging IdP 由谁提供、是否已有可用的 claims mapper？——**已决（2026-11-23）**：Stage 2 不连接真实外部 IdP；采用集群内 staging-like Keycloak（非 localhost issuer + 临时证书）作为 fallback，AC-10 结论标 `partial`，不宣称生产 IdP 验证。
2. RPO/RTO 暂定值（24h/60min）是否需要负责人签核为 Stage 2 目标？——仅影响演练结论措辞，设计不变。
3. web 是否改为运行期配置注入（消除每环境构建）？——属于前端行为变更，留给后续 change。
4. 对象存储/Meilisearch 备份是否纳入 Stage 3？——当前明确标 residual risk。
5. 真实外部 secret provider（Vault / AWS Secrets Manager）选择——Stage 2 不连接；实现前需安全与平台团队确认。
