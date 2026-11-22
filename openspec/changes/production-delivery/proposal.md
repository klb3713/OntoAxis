# Proposal: production-delivery

## Why

Stage 1 已退出，Stage 2 的运行时能力在 Wave 0/Wave 1 逐步落地，但交付方式仍停留在 `deploy/compose/` 的单机 Compose：没有 Helm chart、没有 ArgoCD GitOps、没有 kind/k3d 安装路径、没有 probes/resources/PDB、没有外部密钥注入与备份恢复演练，也没有非本地 IdP 的登录与 Token 验证。Stage 2 AC S2-AC-08 要求生产化交付资产在 kind/k3d 上可部署、可观测、可回滚，S2-AC-10 要求预发 IdP 的 issuer/JWKS/claims 映射经过真实验证，同时禁止提交生产密钥。当前缺口使 Stage 2 无法给出可复核的交付证据；本 change 定义并落地这两项 AC 的交付契约（不涉及真实云账号、生产密钥与客户数据）。

## What Changes

- 新增 capability `production-delivery`，定义可复现的生产化交付行为契约：Helm 渲染与摘要固定、环境 values 与受控晋升、ArgoCD 同步与漂移收敛、kind/k3d 安装、探针/资源/PDB、密钥注入、备份恢复、镜像回滚、预发 IdP、交付烟测与受控环境边界。
- 新增 `deploy/helm/` 分组 chart：`platform`（Postgres、Keycloak、OpenFGA、Valkey、SeaweedFS）、`data`（Kafka、Debezium、Apicurio、Kestra、Temporal、Trino、Nessie）、`app`（ontoaxis-core、web、Meilisearch）、`observability`；镜像按 digest 固定，values 按 kind / staging-like / prod-like 分离。
- 新增 `deploy/argocd/` AppProject + app-of-apps，按 sync wave 同步（命名空间/CRD/ESO → platform → data → app → edge → observability → jobs）；kind/staging-like 自动同步+self-heal，prod-like 手动同步。
- 复用 `deploy/edge/generated/` 的 Envoy 路由/策略与 `deploy/observability/` 的 Prometheus/OTel 配置，新增漂移检查，避免出现第二份路由/告警真源。
- 新增 External Secrets Operator 流程：Git 中只提交 `ExternalSecret`/`ClusterSecretStore` 引用与 key 名称；kind 用本地 fake/kubernetes provider 从 git-ignored `.env.kind` 注入；prod-like 仅声明外部 provider，不连接真实云。
- 新增探针/资源/PDB/优雅终止，并固定 Kubernetes 命名空间布局 `oa-system` / `oa-app` / `oa-data` / `oa-observability`（外加 `argocd`、`external-secrets`）。
- 新增备份/恢复演练：CronJob 执行 `pg_dump -Fc` 并产出校验和/manifest，恢复到干净命名空间后验证迁移版本、代表性数据与只读 API；损坏备份必须被拒绝。
- 新增镜像 release manifest（digest↔commit↔chart 版本）、晋升脚本与 GitOps 回滚演练（失败注入 → Degraded → 回退上一已验证摘要）。
- 新增 staging IdP 配置与验证：非本地 OIDC issuer/JWKS、audience、claims 映射（tenant-id / org-ids / workspace-ids / realm-roles）、负向 token 测试与 JWKS 轮换；本地 Compose Keycloak 不作为 S2-AC-10 证据。
- 新增 `deploy/kind/`（kind 与 k3d 双路径）一键安装/卸载、镜像加载与失败诊断；新增 `scripts/delivery-smoke.sh`、失败证据采集、`make delivery-*` 目标、CI 校验 job 与 `docs/operations/delivery-runbook.md`。
- 不修改业务模块边界、API 语义、认证/授权判定逻辑或既有 Compose 默认路径；Compose 仍是本地开发主入口。

## Capabilities

- **New capabilities**: `production-delivery` — 生产化交付的行为契约（chart/values、GitOps 同步、密钥、备份恢复、摘要与回滚、预发 IdP、交付烟测、受控环境边界）。
- **Modified capabilities**: 无。本 change 不修改 `edge-gateway`、`observability` 等既有 capability 的行为契约；只消费其已归档产物。

## Impact

- 新增目录/文件：`deploy/helm/**`、`deploy/argocd/**`、`deploy/kind/**`、`scripts/delivery-*.sh`、`docs/operations/delivery-runbook.md`、`docs/stages/evidence/stage-2-production-delivery.md`（证据源文件，由 stage-2-integration 汇总）。
- 修改文件：`Makefile`（delivery 目标）、`.github/workflows/ci.yml`（delivery 校验 job）、`deploy/README.md`（生产化交付与回滚路径）、`.gitignore`（kind 本地密钥/缓存模式；已有 `.env*` 规则覆盖 `.env.kind`）、`openspec/changes/stage-2-integration/traceability.md` 汇总由该 change 负责。
- 依赖/工具（本地或 CI 安装，不新增 Go 依赖）：Helm 3、kubectl、kind、k3d、argocd CLI、kubeconform、gitleaks、External Secrets Operator、Envoy Gateway（沿用既有 v1.2.6 版本）。
- 运行时：kind/k3d 或受控 staging-like 集群；新增命名空间、ServiceAccount 与 RBAC；不读取真实云账号、生产密钥或真实客户数据。
- 证据边界：交付证据只来自 kind/k3d 与受控 staging-like 环境；prod-like 只做渲染与本地 profile 验证，明确标记为未连接真实 provider。
- 风险：CI runner 资源可能不足以跑全量栈；外部密钥 provider 与真实 ArgoCD repo 认证未真实验证；备份未覆盖对象存储/Meilisearch；跨 schema 回滚依赖恢复路径而非 down migration。
- 依赖顺序：实现前需 Wave 1 G1 门禁记录为 `pass`（当前无记录），见 `traceability.md` 与 `tasks.md` 1.1；本 change 的规划产物可先行评审，实现必须等门禁满足。
