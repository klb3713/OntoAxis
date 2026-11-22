# Tasks: production-delivery

> 状态说明：实现任务在 Stage 2 Wave 1 G1 门禁记录为 `pass` 之前不得执行；本文件初始全部未勾选。每个任务完成后必须附可复核证据（命令、输出路径、日期），未执行项标 `not passed`。

## 1. 前置与门禁

- [ ] 1.1 确认 Stage 2 Wave 1 G1 门禁记录为 `pass`，并在 change 内记录门禁来源（证据文件/日期）；未满足则本 change 只保留规划产物。验证：门禁记录可追溯到 `docs/stages/evidence/` 中的通过项。
- [x] 1.2 确认 staging IdP 的归属、访问方式与 claims mapper 可用性；若不可得，记录采用集群内 staging-like IdP 的 fallback 决定与 partial 结论。验证：决策写入 design Open Questions 与 `traceability.md`。
- [x] 1.3 固定工具与组件版本（Helm、kubectl、kind、k3d、argocd CLI、kubeconform、gitleaks、ESO、Envoy Gateway、ArgoCD、Prometheus/OTel/Jaeger/Perses 镜像 digest），写入 `deploy/README.md` 版本表。验证：`scripts/delivery-version-check.sh` 输出所有版本并与文档一致。
- [x] 1.4 确认受控环境边界清单：允许 kind/k3d/staging-like/prod-like(render-only)，禁止真实云凭据、生产密钥、真实客户数据；在 `deploy/README.md` 与 runbook 中各记录一份。验证：评审确认清单存在且无生产连接信息。

## 2. Helm chart 与 values

- [x] 2.1 创建 chart 骨架 `deploy/helm/ontoaxis-platform|data|app|observability|jobs`（Chart.yaml、values.yaml、templates/_helpers.tpl、NOTES.txt），命名空间与标签统一为 `app.kubernetes.io/*` 与 `ontoaxis.io/component`。验证：`helm lint deploy/helm/*` 全部通过。
- [x] 2.2 用 chart 渲染 `oa-system`/`oa-data`/`oa-app`/`oa-observability` 的工作负载：Postgres、Keycloak、OpenFGA、Valkey、SeaweedFS、Kafka、Debezium、Apicurio、Kestra、Temporal、Trino、Nessie、core、web、Meilisearch、OTel Collector、Prometheus、Alertmanager、Jaeger、Perses；镜像全部来自 Compose 已固定版本。验证：`helm template` 渲染后 `kubectl apply --dry-run=server` 在 kind 集群通过。
- [x] 2.3 实现镜像 digest 契约：`image.repository` + `image.digest`（`sha256:...`），拒绝 `latest`、缺失 digest、只有 tag 的镜像、未知环境名。验证：负向用例 `helm template` 非零退出并给出字段级错误；正向用例渲染出 `repo@sha256:` 引用。
- [x] 2.4 实现必填密钥引用校验：chart 用 `required` 拒绝缺失的 secretKeyRef/ExternalSecret 名称；values 只出现 Secret 名称与键名。验证：删除必填键后渲染失败并列出缺失 key。
- [x] 2.5 编写 `deploy/values/{kind,kind-small,staging-like,prod-like}.yaml` 与共享默认值；kind 1 副本低资源、kind-small 裁剪重负载、staging-like 关键组件 2 副本、prod-like 仅渲染验证。验证：四套 values 全部渲染成功且差异可解释。
- [x] 2.6 新增 `scripts/delivery-render-check.sh`：每 chart×环境渲染两次比较 sha256、kubeconform 校验（`-ignore-missing-schemas`）、输出 `dist/helm-render/`。验证：重复运行结果一致；故意改 values 后能检测到渲染变化。
- [x] 2.7 新增 `make delivery-lint`、`make delivery-render`、`make delivery-render-check` 目标，并在 `deploy/README.md` 记录用法。验证：`make delivery-render-check` 在干净工作区通过。
- [x] 2.8 新增 `scripts/delivery-sync-observability.sh` 与 `make delivery-config-check`：把 `deploy/observability/` 的 Prometheus/Alertmanager/OTel 配置同步进 `ontoaxis-observability` chart 的 `files/`（带内容 digest 头），漂移时检查失败；确认 `deploy/edge/generated/` 以 ArgoCD Directory 方式引用而不拷贝。验证：修改源配置后 `delivery-config-check` 失败，重新同步后通过。

## 3. 探针、资源与 PDB

- [ ] 3.1 为所有长期运行组件配置 startup/readiness/liveness 探针：HTTP 组件用 `/healthz`、`/readyz`，不使用 `/metrics` 作为存活探针；配置 initialDelaySeconds/periodSeconds/timeoutSeconds/failureThreshold。验证：kind 中人为改坏 readiness path 后 Endpoint 摘除且容器不重启。
- [ ] 3.2 为每个组件配置 requests/limits；为 memory 敏感组件设置合理 limit 并验证 OOMKilled 可观察。验证：kind 中降低 limit 复现 OOMKilled 事件并被 `delivery-diag.sh` 采集。
- [ ] 3.3 配置优雅终止：`terminationGracePeriodSeconds`、`preStop`（HTTP 组件 sleep 或 drain）；core 关闭时完成在途请求。验证：滚动更新期间无 5xx（烟测在滚动窗口内连续请求）。
- [ ] 3.4 为关键无状态组件（core、web、keycloak、trino、meilisearch 等）配置 PDB，`minAvailable` 由副本数推导并写入注释；kind 使用 2 workers。验证：`kubectl drain` 一个 worker 时 PDB 阻止或阻塞低于 minAvailable 的驱逐；滚动更新期间可用副本不低于 PDB。
- [x] 3.5 记录资源画像与 PDB 推导到 `docs/operations/delivery-runbook.md`（含 kind-small 与 staging-like 差异）。验证：runbook 中的数值与 chart values 一致（脚本/人工核对）。

## 4. 密钥注入

- [x] 4.1 新增 bootstrap 安装脚本安装 External Secrets Operator（Helm，固定版本）到 `external-secrets` 命名空间，并记录版本与来源。验证：`kubectl -n external-secrets get pods` 就绪，版本与文档一致。
- [x] 4.2 新增 `deploy/argocd/secrets/`：每环境一个 `ClusterSecretStore`（kind/staging-like 用 `kubernetes` provider 读取 `ontoaxis-bootstrap`；prod-like 仅模板声明外部 provider 并不连接），以及组件 `ExternalSecret`（core/keycloak/data/openfga/backup）。验证：kind 中 ExternalSecret 状态 Synced，目标 Secret 与 key 完整。
- [x] 4.3 新增 `scripts/delivery-secrets-bootstrap.sh`：从 git-ignored `.env.kind` 或环境变量创建 `ontoaxis-bootstrap` Secret；提供 `.env.kind.example` 仅列键名不含值；确认 `.env.kind` 被 `.gitignore` 覆盖。验证：不提供变量时脚本报缺失键名；提供后 Secret 创建成功且 `git status` 无敏感文件。
- [x] 4.4 完成密钥键清单与 `deploy/argocd/secrets/README.md`：数据库口令、Keycloak bootstrap admin 与 OIDC client secret、OpenFGA token、Meilisearch master key、Trino 凭据、SeaweedFS/S3 access/secret、备份口令；每项标注来源与轮换责任人。验证：清单与 chart 中每个 secretKeyRef 一一对应（脚本核对）。
- [x] 4.5 验证缺失/非法密钥行为：删除某必需 key 后 Pod 未就绪，错误信息不含密钥值；外部 store 不可达时保持未就绪、不回退默认口令。验证：负向用例通过并采集事件作为证据。
- [ ] 4.6 验证轮换路径：轮换数据库口令与 OIDC client secret（在 kind staging-like 环境），refresh 后按 runbook 重启相应组件并验证服务恢复；记录无需重建镜像。验证：轮换前后 Pod 镜像 digest 不变，烟测通过。
- [x] 4.7 新增 `gitleaks` 配置与 `scripts/delivery-secret-scan.sh`（扫描 values/脚本/证据中的字面密码、token、DSN 凭据；识别 `secretKeyRef`/`remoteRef` 合法引用），并接入 CI。验证：提交含明文 secret 的测试夹具时扫描失败并阻断；仅引用密钥的 values 通过。

## 5. ArgoCD、晋升与回滚

- [x] 5.1 新增 `deploy/argocd/projects/ontoaxis.yaml`：AppProject 限制 repo、destination 命名空间与 cluster 资源白名单。验证：`argocd proj get ontoaxis` 输出符合预期；越权 destination 被拒绝。
- [ ] 5.2 新增 `deploy/argocd/root-app.yaml` 与 `deploy/argocd/apps/`：app-of-apps，sync wave -10/-5/0/1/2/3/4/5 分别对应命名空间、密钥、platform、data、app、edge、observability、jobs。验证：kind 中所有 Application `Synced/Healthy`，wave 顺序在 ArgoCD UI/CLI 可复核。
- [ ] 5.3 配置同步策略：kind/staging-like `automated{prune,selfHeal}`；prod-like 无 automated；失败重试有限次；jobs 用 `PruneLast`。验证：kind 提交后自动同步；prod-like 提交后保持 OutOfSync 直到显式 sync。
- [ ] 5.4 验证 wave 依赖：platform 未就绪时 data/app 等待；core 在 platform+data 就绪后启动（迁移在 core 启动时执行）。验证：从空集群部署观察 Application 进度与事件，记录等待证据。
- [ ] 5.5 验证漂移收敛：手工 `kubectl patch` Deployment/ConfigMap 后 Application 变 OutOfSync，self-heal 恢复期望状态并记录事件。验证：`scripts/delivery-drift-check.sh` 输出 diff 摘要与恢复后状态。
- [ ] 5.6 实现 release manifest `deploy/releases/<env>.yaml` 与 `scripts/delivery-release-check.sh`：digest、commit、chartVersion、created_at、operator；与 values 一致性检查、与运行中 Pod imageID 一致性检查。验证：不一致时脚本非零退出并报告组件。
- [ ] 5.7 实现 `scripts/delivery-promote.sh`：读取来源 env 烟测 JSON（要求 `passed: true`）、校验 digest 存在、更新目标 values 与 release manifest、跑渲染检查、生成提交（不自动 push），prod-like 需人工 sync。验证：缺证据时拒绝；成功时提交 diff 只含 digest/chart 版本等非敏感字段。
- [ ] 5.8 实现 `scripts/delivery-rollback.sh`：按上一 release manifest 回写 digest、提交、同步、等待 Healthy、跑烟测、写回滚标记。验证：对坏部署演练执行后恢复到上一 digest，Healthy 且烟测通过。
- [ ] 5.9 执行坏部署回滚演练两种故障：缺失镜像 digest（ImagePullBackOff）与 readiness path 覆盖（progress deadline）；记录 ArgoCD Degraded 事件、回滚耗时与最终状态。验证：两次演练均回滚成功，证据含 `argocd app history`、事件与烟测 JSON。
- [x] 5.10 记录跨 schema 回滚流程到 runbook：比对 schema 版本、停止写入、恢复备份到独立实例、部署旧镜像、校验；不执行 down migration。验证：runbook 步骤经一次桌面推演/演练验证（可标注 `not executed` 的步骤说明原因）。

## 6. kind/k3d 安装

- [x] 6.1 新增 `deploy/kind/kind-up.sh` / `kind-down.sh`：1 control-plane + 2 workers、`kind-registry` 容器与 containerd mirror、幂等判断；输出 kubeconfig 上下文。验证：干净环境一次安装成功，重复执行无破坏；卸载后无残留容器/端口。
- [x] 6.2 新增 `scripts/delivery-images.sh`：构建 core/web（tag=git short SHA）、推送本地 registry、解析 digest 写入 `dist/delivery/digests.env` 与 `deploy/releases/kind.yaml`。验证：digest 与 registry manifest 一致，release manifest 字段完整。
- [ ] 6.3 新增 `deploy/kind/bootstrap.sh`：namespaces、ArgoCD、ESO、Envoy Gateway（复用 `deploy/edge/kind/install-envoy-gateway.sh` 与 GatewayClass）、bootstrap Secret、AppProject/root app；就绪超时失败。验证：空集群执行后 Application 全部 Synced/Healthy。
- [ ] 6.4 新增 `deploy/kind/verify.sh`：等待就绪并调用 `scripts/delivery-smoke.sh --env kind`；失败自动调用 `scripts/delivery-diag.sh`。验证：正常环境退出 0；注入失败后退出非零并产出诊断目录。
- [ ] 6.5 新增 k3d 路径 `deploy/kind/k3d-up.sh` / `k3d-down.sh`（`--agents 2`、禁用 k3d traefik、registry 配置），复用同一 bootstrap/verify 脚本。验证：k3d 与 kind 烟测检查项与结果一致，证据标注集群类型。
- [ ] 6.6 验证安装失败诊断：使用不存在镜像 digest 触发 ImagePullBackOff，确认脚本非零退出并采集事件/describe/日志（脱敏）。验证：诊断目录内容可复核，不含密钥。
- [ ] 6.7 实现入口验证方式：`kubectl port-forward` 到 Envoy Gateway service（默认 18088）并记录到 runbook；kind/k3d 不依赖 LoadBalancer/NodePort。验证：烟测通过 port-forward 访问 edge 路由；记录该方式与真实 ingress 的差异。

## 7. 备份与恢复

- [ ] 7.1 新增 `ontoaxis-jobs` chart 中的备份 CronJob（每日 02:00 UTC，支持手动 `kubectl create job --from=cronjob`）：`pg_dump -Fc` 到 PVC、生成 `manifest.json`（backup_id/created_at/sha256/size/schema_version/image_digest/pg_version）、保留 7 份、失败非零退出。验证：手动触发一次，产物与 manifest 字段齐全、sha256 可复算。
- [ ] 7.2 新增 `scripts/delivery-restore.sh`：校验 manifest 与 sha256、创建 `oa-restore` 命名空间与独立 Postgres、`pg_restore`、校验 goose 版本与代表性行数、启动 core 校验 `/readyz` 与只读 API、结束清理。验证：从最近备份完整恢复，校验结果全部通过。
- [x] 7.3 负向演练：篡改备份 1 字节与截断 dump，确认恢复脚本拒绝执行并保留证据，不启动恢复实例。验证：两种损坏均被拒绝，错误信息指出校验失败原因。
- [ ] 7.4 记录实测 RPO/RTO：备份时间点、恢复开始/完成时间、数据点差异，与 Stage 2 暂定目标（RPO ≤24h、RTO ≤60min）对比并标注"仅技术验证，非生产承诺"。验证：证据表格含时间戳与实测值，结论措辞符合要求。
- [x] 7.5 记录未覆盖范围：对象存储与 Meilisearch 由 `object-search-meilisearch` 重建路径恢复，Kafka/Kestra/Temporal/Trino/Nessie 视为可重建；在 evidence 与 runbook 标 residual risk。验证：评审确认未宣称完整备份。
- [ ] 7.6 将备份/恢复命令、校验分级（L1–L5）与失败分类写入 `docs/operations/delivery-runbook.md`。验证：按 runbook 从零执行一次恢复演练成功。

## 8. staging IdP

- [x] 8.1 在 `deploy/values/staging-like.yaml` 配置非本地 OIDC issuer（`https://auth.staging...`，非 localhost）、audience `ontoaxis-api`、可选 jwksUrl；web 以同 commit 另构建 staging 镜像并记录 digest 差异。验证：渲染后的 core/web 配置中 issuer 为 staging 地址，无生产密钥。
- [x] 8.2 确认外部 IdP 提供 claims 映射（tenant-id、org-ids、workspace-ids、realm-roles、audience mapper）；不可得时记录 fallback 决定。验证：discovery 与 token 解码结果确认 claims 名称/类型符合 core 期望（不记录 token 原文）。
- [ ] 8.3 新增 `scripts/delivery-idp-check.sh`：discovery issuer 一致性、受控测试用户登录/token、core 只读 API 200 与身份上下文映射；凭据只从密钥源读取。验证：kind staging-like 环境成功；日志不含 token。
- [ ] 8.4 负向测试：错误 audience、其他 issuer（本地 Compose Keycloak）、过期 token、坏签名/未知 kid、缺失映射 claims，全部 401/403 fail-closed 且不产生部分授权。验证：每项结果与 HTTP 状态记录为证据。
- [ ] 8.5 JWKS 轮换测试：轮换 staging IdP 签名密钥后新 token 被接受、旧 token 在有效期内可用、服务无需重启；无 IdP 管理权限时标 `not executed` 并记录原因。验证：`delivery-idp-check.sh` 在轮换前后各跑一次，记录 kid 变化。
- [ ] 8.6 fallback 路径：新增 staging-like in-cluster Keycloak（非 localhost issuer + `deploy/kind/tls/` 临时证书）与相同验证脚本入口；结论标 `partial` 并说明原因。验证：fallback 环境登录与负向测试通过，证据明确标注 partial。
- [ ] 8.7 安全评审：按 Stage 2 brief §7 提交 IdP 配置/claims 映射评审清单（issuer 白名单、audience 严格匹配、JWKS 来源、claim 名称固定、无生产密钥），记录评审人与结论。验证：评审记录入库；未评审前不得把 AC-10 标为 pass。

## 9. 烟测、证据与 CI

- [ ] 9.1 实现 `scripts/delivery-smoke.sh`（参数 `--env`、`--evidence-dir`）：edge 200、core `/readyz`、IdP discovery、ArgoCD 应用 Synced/Healthy、探针/PDB/Secret 生效、最近备份标记、运行 digest 与 release manifest 一致；输出 JSON 含 environment/digest/started_at/checks[]/overall，非零退出表示失败，未执行项 `not passed`。验证：全绿退出 0；注入失败退出非零；JSON 字段可被脚本复核。
- [x] 9.2 实现 `scripts/delivery-diag.sh`：采集 Pods、事件（按时间）、未就绪 describe、日志尾部（脱敏）、registry manifest、ArgoCD 状态与条件；自动在失败时调用。验证：诊断产物不含 token/口令，且能定位到注入的故障。
- [x] 9.3 实现 `scripts/delivery-evidence.sh`：把烟测 JSON、diag、命令与结果汇总为 `docs/stages/evidence/stage-2-production-delivery.md` 的表格片段（环境、命令、结果、证据路径、时间），不含密钥。验证：生成片段可被 stage-2-integration 直接汇总。
- [ ] 9.4 CI 新增 `delivery-static` job（必跑）：helm lint/template 全环境、渲染确定性检查、kubeconform、gitleaks、shellcheck/actionlint、`make edge-generate-check`、`make delivery-config-check`。验证：CI 通过；人为引入 latest 镜像或明文 secret 时 CI 失败。
- [ ] 9.5 CI 新增 `delivery-kind` job（workflow_dispatch/定时，kind-small profile）：kind 创建、镜像构建加载、bootstrap、部署与烟测，记录耗时与资源限制。验证：一次完整 CI run 通过；若 runner 资源不足，记录降级方案（拆分 job/仅本地）。
- [ ] 9.6 补齐 `make delivery-*` 目标（lint、render-check、config-check、smoke、diag、backup、restore、idp-check、promote、rollback、kind-up/down）。验证：每个目标在干净环境按 `make help`/README 说明可执行。
- [ ] 9.7 运行完整证据链：kind 安装 → 部署 → 烟测 → 备份/恢复 → 坏部署回滚 → 漂移自愈 → IdP 验证，产出 `docs/stages/evidence/stage-2-production-delivery.md` 与日志。验证：每项有命令、结果、时间与证据路径；未执行项标 `not passed`；不含生产凭据/客户数据。

## 10. 文档与评审

- [ ] 10.1 新增 `docs/operations/delivery-runbook.md`（仿 observability-runbook 结构）：入口表、故障分类（缺失镜像、Secret 未同步、Pod 未就绪、迁移失败、DB 不可达、ArgoCD Degraded、备份校验失败、回滚失败、edge 502）、每类诊断/修复/回滚/证据命令，覆盖 ARCHITECTURE §14.7 最小事故集。验证：按 runbook 处理至少一次注入故障成功。
- [x] 10.2 更新 `deploy/README.md`：生产化交付入口、版本表、kind/k3d 快速开始、晋升/回滚/备份命令、原有 Compose 与回滚说明保留并指向 runbook。验证：文档中命令与实际脚本/目标一一对应。
- [ ] 10.3 更新 `traceability.md`：把计划证据替换为实际证据路径与结论，标注 S2-AC-08/AC-10 的 pass/partial/not passed，及 G2 门禁依赖。验证：追溯矩阵与 evidence 文件一致。
- [ ] 10.4 全量门禁：`make lint`、`make test`、`openspec validate production-delivery --strict`、`openspec validate --all --strict`、`helm lint`、`actionlint`、`shellcheck`、gitleaks 全部通过。验证：命令输出与退出码入库为证据。
- [ ] 10.5 负责人评审：交付设计、密钥路径、受控环境边界、AC-08/AC-10 证据完整性；确认没有真实云账号/生产密钥/客户数据。验证：评审记录入库；评审通过后才允许勾选完成并准备归档（本 change 不自动归档）。
