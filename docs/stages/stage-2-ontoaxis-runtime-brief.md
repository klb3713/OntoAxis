# Stage 2 — OntoAxis 真实运行时与生产入口（已退出）

> 状态：exited（2026-11-28；负责人 klb3713 确认 Stage 2 退出并归档 `stage-2-integration`）<br>
> 阶段编号：S2<br>
> 负责人：klb3713<br>
> 最后更新：2026-11-28<br>
> 目标设计：[`ARCHITECTURE.md`](../../ARCHITECTURE.md) v1.3（重点第 2、3、6、7、9、11、12、14、15 章）<br>
> 上一阶段：Stage 1 已退出（`exited`，2026-11-22；见 [`stage-1-ontoaxis-core-brief.md`](stage-1-ontoaxis-core-brief.md) 与 [`stage-1-exit-report.md`](stage-1-exit-report.md)）

## 1. 目的

Stage 1 已经完成"单进程核心 + 9 个业务模块 + Keycloak + Trino + Web + Compose + CI"的本地闭环。  
Stage 2 不再扩展业务模块或页面数量，而是把以下三类仍为占位、adapter 或本地信任模式的能力推进到**可部署、可授权、可运行**：

1. **真实执行运行时**：Temporal、Kestra + dbt、Kafka Connect/Debezium CDC；Meltano 后移到 Stage 3；
2. **生产入口与安全边界**：独立 `oa-edge`、OpenFGA + Cedar、生产 IdP；
3. **生产交付与可观测**：Helm/ArgoCD、备份/回滚、OTel/SLO、Meilisearch 搜索索引。

本 Stage 完成后，OntoAxis 具备"预生产可部署"能力，但不等于生产运营就绪；多区域 HA、容量规划与真实客户数据仍不在范围内。

## 2. 交付范围

### 2.1 必须交付

- **Edge 与身份**：独立 `oa-edge` 部署清单（K8s Envoy Gateway；Compose 用 Traefik）、JWT/租户上下文、限流、路由负向测试；两种实现都从 `config/edge/routes.yaml` 生成/校验；生产 IdP 的 issuer/JWKS/claims 映射验证。
- **授权**：OpenFGA 关系授权模型 + Cedar 属性策略，二者接入 `identity-edge` 与业务模块；OpenFGA 负责关系，Cedar 负责动作/属性策略，不重复检查；越权、跨租户、无策略默认拒绝均需测试。
- **Action 真实执行**：Temporal workflow/activity、重试、补偿、人工审批；替换 Stage 1 的 no-op/内存 adapter。
- **Pipeline 真实执行**：Kestra Flow 发布/运行/状态回写、dbt 项目结构与 SQL 转换；真实实例端到端。
- **Connector 真实同步**：Kafka Connect/Debezium CDC 至少一条真实同步链路，并生成 Dataset；Meltano 明确后移到 Stage 3。
- **对象搜索**：Meilisearch 索引由 outbox 消费者更新，支持增量索引、重建与查询。
- **生产交付**：Helm chart + ArgoCD Application 资产静态可渲染；运行态部署/探针/PDB/Secret/备份恢复/镜像回滚验收整条移交独立 change `delivery-runtime-acceptance`（见 §4 S2-AC-08 与 change-map §12）；Stage 2 以 **Compose 单环境**验证功能。
- **可观测**：OTel trace、指标规范、SLO/SLI、关键告警、仪表盘与一个可复现负载基线。
- **质量门**：Stage 1 回归全部通过；新增路径有单元/集成/E2E 证据；CI 继续全绿。

### 2.2 明确不交付

- 不新增第 10 个业务模块，不把 `ontoaxis-core` 拆回多服务；
- 不追求完整 Foundry 功能覆盖（AI/ML、BI、地理空间、Notebook、Workshop、Marketplace 等继续后置）；
- 不做多区域 HA、跨区域灾备、生产容量规划、自动扩缩容和 24×7 运营；
- 不使用真实客户数据、真实生产密钥或生产环境；
- 不兼容旧仓库服务、旧 Helm chart 或旧 SDK。

## 3. 前置条件

1. 本 brief 经负责人评审并标记为 `approved`；
2. `ARCHITECTURE.md` v1.3 作为目标设计基线；
3. Stage 1 已退出，12 个 change 已归档，11 个 capability spec 已写入；
4. 已确认 Stage 2 的外部组件版本与部署方式：OpenFGA、Cedar、Temporal、Kestra、dbt、Kafka Connect/Debezium、Meilisearch、Envoy Gateway/Traefik、Helm/ArgoCD；Meltano 不在 Stage 2 范围；
5. 本地具备 Docker Compose（kind/k3d 集群由交付 change `delivery-runtime-acceptance` 负责，不在 Stage 2 前置条件）；外部组件只使用本地或预发环境；
6. Stage 2 不接入真实客户数据，使用合成 fixture 和受控测试账号；
7. 依赖基线：组件版本以 `deploy/deps/policy.yaml` + `deploy/deps/lock.yaml` 为准，
   `make deps-check` 通过；规则与例外见 [`docs/workflow/DEPENDENCIES.md`](../workflow/DEPENDENCIES.md)。

## 4. Stage 验收标准（AC）

| AC | 验收标准 |
|---|---|
| S2-AC-01 | Stage 1 全部回归通过：9 个模块边界不变，`go test ./...`、前端 `check/build/test`、`buf lint`/`breaking`、Compose smoke、Stage 1 Playwright 均保持绿色 |
| S2-AC-02 | `oa-edge` 独立部署可用：路由与 Stage 1 一致，JWT/租户上下文透传，未认证 401、越权 403、限流与未知路由行为可验证 |
| S2-AC-03 | OpenFGA + Cedar 授权生效：关系授权与属性策略各有允许/拒绝用例；跨租户、越权、无策略默认拒绝、策略变更审计均可复核 |
| S2-AC-04 | Action 通过真实 Temporal 执行：至少一个 workflow 包含 activity 失败重试、补偿和人工审批分支；替换 Stage 1 的 no-op adapter |
| S2-AC-05 | Pipeline 通过真实 Kestra + dbt 执行：发布 Flow、运行、状态回写、失败重试与 Dataset 输出均可验证 |
| S2-AC-06 | Connector 真实同步：Kafka Connect/Debezium CDC 至少一条链路，最终生成 Dataset 并可查询；Meltano 不在 Stage 2 验收范围 |
| S2-AC-07 | Meilisearch 对象搜索：outbox 驱动索引更新，对象新增/修改/删除后搜索结果一致；支持全量重建 |
| S2-AC-08 | 生产交付（**已整条移出 Stage 2**，2026-11-28 负责人决策）：Helm/ArgoCD/kind/k3d 的渲染门与运行态部署、探针/PDB/Secret、备份恢复与镜像回滚验收**全部**由独立 change `delivery-runtime-acceptance` 承接，**不在 Stage 2 验收范围** |
| S2-AC-09 | 可观测性：edge/core/worker 的 trace 贯通；关键指标、SLO 面板、告警规则和负载基线可复现 |
| S2-AC-10 | 生产 IdP（**已移出 Stage 2**，Stage 3 候选）：使用非本地 OIDC issuer/JWKS/claims 映射完成登录与 token 校验；不提交生产密钥。由 `production-idp-staging` 承接 |
| S2-AC-11 | Stage 2 证据与报告完成：全部 change 归档、`openspec validate --all --strict` 通过、CI 全绿、Stage 退出获负责人确认 |

## 5. Change 清单

| 建议目录名 | 类型 | 行为/能力边界 | 主要模块/目录 | 依赖 | 波次 |
|---|---|---|---|---|---|
| `runtime-observability` | 能力 | OTel trace、指标规范、SLO/SLI、仪表盘与测试 harness | `internal/platform/observability`、`deploy/observability` | Stage 1 | Wave 0 |
| `edge-gateway` | 能力 | 独立 `oa-edge`、JWT/租户透传、限流、路由负向测试 | `config/edge`、`deploy/edge` | `runtime-observability` | Wave 0 |
| `authorization-openfga-cedar` | 能力 | OpenFGA 模型、Cedar 策略、授权中间件与审计 | `internal/platform/authz`、`internal/platform/authn` | `edge-gateway` | Wave 0 |
| `connector-runtime` | 能力 | Kafka Connect/Debezium CDC 与真实同步 | `internal/modules/connectors`、`internal/modules/ingestion` | `edge-gateway` | Wave 1 |
| `pipeline-kestra-dbt` | 能力 | Kestra Flow、dbt 项目、运行状态回写 | `internal/modules/pipelines` | `edge-gateway` | Wave 1 |
| `action-temporal` | 能力 | Temporal workflow/activity、重试/补偿、审批 | `internal/modules/actions` | `edge-gateway`、`authorization-openfga-cedar` | Wave 1 |
| `object-search-meilisearch` | 能力 | 对象搜索：outbox 索引消费者、Meilisearch 搜索与重建 | `internal/modules/objects`、`internal/platform/search` | `runtime-observability` | Wave 2 |
| `production-delivery` | Stage 支撑 | Helm/ArgoCD 资产、Secret/PDB、备份恢复、回滚、本地产 IdP 证据（生产 IdP 移出）；已按收缩范围归档 2026-11-24 | `deploy/`、`config/` | 所有 Wave 1 | Wave 2 |
| `stage-2-integration` | Stage 支撑 | 全栈 E2E、跨模块修复、证据与 Stage 报告 | `cmd`、`internal/app`、`deploy/` | 所有 change | Wave 3 |
| `stage-2-dependency-governance` | Stage 支撑 | 依赖清单、锁文件门禁、Renovate 分级、月度审计 | `deploy/deps`、`scripts/deps.*`、`.github/workflows/deps.yml` | Stage 1 | Wave 0 |

## 6. 多 Agent 并行策略

### Wave 0 — 平台入口与授权（串行）

- `runtime-observability` → `edge-gateway` → `authorization-openfga-cedar`

### Wave 1 — 真实执行（3 个并行）

- `connector-runtime`
- `pipeline-kestra-dbt`
- `action-temporal`

### Wave 2 — 搜索与生产交付（2 个并行）

- `object-search-meilisearch`
- `production-delivery`（已按收缩范围归档 2026-11-24）

### Wave 3 — 集成（串行）

- `stage-2-integration`

### 共享文件所有权

- `cmd/ontoaxis-core/`、`internal/app/`：`stage-2-integration` 统一接线；
- `config/edge/`：`edge-gateway`；
- `deploy/compose/`、`deploy/helm/`、`deploy/argocd/`：`production-delivery`；
- `go.mod`、`go.sum`：由需要新依赖的 change 在 PR 中提出，`stage-2-integration` 合并；
- `deploy/deps/`、`renovate.json`、`.github/workflows/deps.yml`、`scripts/deps.py`、`scripts/deps.sh`：`stage-2-dependency-governance`；
- `web/`：默认不新增页面；如 Action 审批需要最小 UI，由 `action-temporal` 提供，`stage-2-integration` 合并。

## 7. 安全、数据与证据

- 授权默认拒绝；不得用 `AUTH_DISABLED`、默认 Principal 或关闭策略通过验收；
- OpenFGA 模型、Cedar 策略与生产 IdP 变更必须经过安全评审；
- 所有外部服务使用本地或预发实例；测试账号、密钥、Token 不写入仓库与证据；
- 本 Stage 使用合成 fixture；真实验收数据单独标注，不冒充生产数据；
- 每个 AC 必须有可复核的测试输出、CI 日志或运行记录；
- 未完成/未验证项只能标记 `planned`/`not passed`，不得写成通过。

## 8. 退出条件

1. 所有必需 change 已归档，Stage AC 全部有证据或明确标记未覆盖；
2. `openspec validate --all --strict` 通过；
3. `go build ./...`、`go test ./...`、前端 check/build/test、Playwright、`buf lint`/`breaking`、CI 全绿；
4. Compose 能启动并按 Stage 2 AC 完成 E2E（kind/k3d、交付运行态验收与生产 IdP 均不在 Stage 2 退出判据，移交独立 change）；
5. Stage 2 报告、架构文档同步、风险与遗留项记录完成；
6. 依赖基线可复核：`make deps-check` 通过，最近一次 `make deps-audit` 报告已归档，无过期例外；
7. 负责人确认 Stage 2 退出；不自动批准、不自动归档、不自动宣布完成。

## 9. 已确认决策（2026-11-22）

1. **oa-edge**：K8s 使用 Envoy Gateway，Compose 使用 Traefik；两者由 `config/edge/routes.yaml` 单一来源生成/校验，不手工维护两份路由。
2. **授权**：自托管 OpenFGA + PostgreSQL 负责关系授权；Cedar 负责动作/属性策略；两者边界明确、fail-closed，不重复检查。
3. **Connector**：Stage 2 只做 Kafka Connect/Debezium CDC；Meltano 后移到 Stage 3。
4. **Meilisearch**：保留在 Stage 2，但仅做对象搜索；排在 Wave 2，与生产交付并行。
5. **生产交付**：Stage 2 以 **Compose-only** 验证功能，直到正式发布（2026-11-28 负责人决策）；kind/k3d/ArgoCD 交付运行态验收整条移交独立 change `delivery-runtime-acceptance`（含原 6.1–6.5、7.7、E2E-10 与 S2-AC-08），生产级加固见 `production-delivery-hardening`，真实 staging IdP 见 `production-idp-staging`；不接触真实云账号、生产密钥或真实客户数据。

## 10. 关联文档

- [`ARCHITECTURE.md`](../../ARCHITECTURE.md)
- [`docs/stages/stage-2-change-map.md`](stage-2-change-map.md)
- [`docs/stages/stage-1-exit-report.md`](stage-1-exit-report.md)
- [`docs/workflow/RUNBOOK.md`](../workflow/RUNBOOK.md)
- [`openspec/config.yaml`](../../openspec/config.yaml)
