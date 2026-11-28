# Stage 2 Integration

> 状态：planning（2026-11-22 创建）。本 change 尚未实施：`tasks.md` 全部未勾选；proposal/design/traceability 中出现的命令与结果是“计划证据”，不是已发生的事实。
> 归属：Stage 2（`in_progress`），Wave 3（串行集成）。
> 依据：`docs/stages/stage-2-ontoaxis-runtime-brief.md`（S2-AC-01、S2-AC-11）、`docs/stages/stage-2-change-map.md`（Wave 3、共享文件所有权）、`ARCHITECTURE.md` 15.1–15.3。

## Why

Stage 2 的 Wave 0–2 把真实运行时、生产入口、授权与交付拆成 8 个独立 change 分别交付；只有把这些 change 装配进同一个 `ontoaxis-core` 进程、同一套 Compose 与 kind/k3d 环境，并跑通跨 change 的全栈端到端路径，才能证明 Stage 2 交付的是“预生产可部署的 OntoAxis”，而不是一组各自通过、互不联调的组件。Stage 1 的全量回归（9 个模块边界、契约、前端、Compose、Playwright）也必须在 Wave 3 重新证明仍然绿色，否则 Stage 2 的真实运行时改动可能已经破坏了既有核心。

本 change 是 Wave 3 唯一的支撑 change：只做装配、跨 change 修复、全栈 E2E、证据与 Stage 2 报告，不引入新的业务行为，也不替代任何能力 change 的验收职责。

## What Changes

- **模块装配**：在 `cmd/ontoaxis-core/` 与 `internal/app/` 完成 Wave 0–2 的最终接线；`internal/app/modules.go` 的 `BuildModules` 仍是唯一装配点，9 个业务模块的名称、依赖顺序与边界不变。Temporal、Kestra、Kafka Connect、Meilisearch、OpenFGA/Cedar 等新运行时通过既有模块的 option/adapter 接缝注入，不新增第 10 个模块。
- **依赖收口**：合并各 change 提出的 `go.mod`/`go.sum` 补丁并统一 `go mod tidy`；统一刷新 `make generate` 与 `make edge-generate` 的生成物，保证 `make generate-check`、`make edge-generate-check` 在干净工作区通过。
- **环境装配**：核对 Compose 的 `default`、`edge`、`observability` 与 Wave 1/2 运行时 profile 能在同一栈中共存，`deploy/edge/` 生成物、`deploy/observability/` 配置与 Helm/ArgoCD 交付物可被同一套全栈 E2E 使用；`deploy/compose/`、`deploy/helm/`、`deploy/argocd/` 的文件所有权仍归 `production-delivery`，本 change 只做装配级核对和经 owner 同意的缺陷修复。
- **跨 change 集成**：按依赖顺序逐个合并已归档 change，记录提交/证据链接；发现跨 change 缺陷时先分类（依赖缺失、跨 change 缺陷、环境问题、安全回归），再决定修复、回退给归属 change 或阻塞记录，禁止用关闭鉴权、删除断言或放宽 skip 的方式通过。
- **全栈 E2E**：在 Compose 与 kind/k3d 上执行跨 change 场景（身份→edge→模块→Temporal/Kestra/Kafka Connect→outbox→Meilisearch→查询/搜索、observability trace、生产交付演练），并重跑 Stage 1 全量回归，作为 S2-AC-01 的证据。
- **证据与报告**：在 `docs/stages/evidence/` 建立 Stage 2 集成证据索引与原始日志目录，起草 `docs/stages/stage-2-exit-report.md`，运行 `openspec validate --all --strict` 并记录 CI 结果，作为 S2-AC-11 的证据；**不自动批准、不自动归档、不自动宣布 Stage 退出**。

**Out of Scope（明确不做）**

- 不新增业务模块、页面或第 10 个模块；不改变任何模块的领域行为、公开 HTTP/Proto API 或数据库 schema。
- 不重新实现 Wave 0–2 change 的工作；不替它们补 AC 证据；不重写已归档 change 的 artifacts 或把历史证据“补绿”。
- 不做多区域 HA、生产环境部署、真实客户数据、真实生产密钥或 24×7 运营。
- 不自动归档本 change，不自动修改 Stage 2 brief 状态，不自动宣布 Stage 2 退出。
- 不扩大 Stage 2 范围；范围外需求记录到报告并留给 Stage 3 决策。

## Capabilities

本 change 不新增或修改 capability：它只做装配、集成验证、证据与报告，不改变任何可观察行为。因此 `.openspec.yaml` 设置 `skip_specs: true`，不创建 delta spec；既有 13 个 capability 契约（`openspec/specs/`）保持不变。与 `stage-1-core-integration` 的处理方式一致。

## Dependencies

Wave 3 准入要求 `docs/stages/stage-2-change-map.md` §4 的 G2 记录为 `pass`，且下列 change 已归档（或按 change map 规则记录阻塞）：

| Change | 波次 | 本 change 依赖的交付 | 当前状态（2026-11-22） |
| --- | --- | --- | --- |
| `runtime-observability` | Wave 0 | telemetry、指标、SLO/告警、observability profile 与 harness | 已归档：`openspec/changes/archive/2026-11-22-runtime-observability/`；主 spec `openspec/specs/observability/spec.md` |
| `edge-gateway` | Wave 0 | `config/edge/routes.yaml` 单一来源、`oa-edge`、负向响应与限流 | 已归档：`openspec/changes/archive/2026-11-22-edge-gateway/`；主 spec `openspec/specs/edge-gateway/spec.md` |
| `authorization-openfga-cedar` | Wave 0 | OpenFGA 关系授权、Cedar 属性策略、默认拒绝与审计 | 计划中（未归档） |
| `connector-runtime` | Wave 1 | Kafka Connect/Debezium CDC、同步状态回写、Dataset 生成 | 计划中（未归档） |
| `pipeline-kestra-dbt` | Wave 1 | Kestra Flow 发布/运行/状态回写、dbt 项目 | 计划中（未归档） |
| `action-temporal` | Wave 1 | Temporal workflow/activity、重试/补偿/审批 | 计划中（未归档） |
| `object-search-meilisearch` | Wave 2 | outbox 索引消费者、Meilisearch 索引与重建 | 计划中（未归档） |
| `production-delivery` | Wave 2 | Helm/ArgoCD、kind/k3d 部署、备份恢复、镜像回滚、生产 IdP 验证 | 计划中（未归档） |

依赖顺序按 change map §2 的 DAG：`runtime-observability` → `edge-gateway` → `authorization-openfga-cedar` →（`connector-runtime`、`pipeline-kestra-dbt`、`action-temporal`）→（`object-search-meilisearch`、`production-delivery`）→ `stage-2-integration`。任一前置 change 未归档或未经 G2 记录 `pass` 时，本 change 只能保持 planning/blocked，不得用部分栈的绿色结果冒充集成通过。

## Shared File Ownership

按 `docs/stages/stage-2-change-map.md` §5 执行；本 change 只合并、不改写归属方产物：

| 文件/目录 | Owner | 本 change 的规则 |
| --- | --- | --- |
| `cmd/ontoaxis-core/`、`internal/app/` | `stage-2-integration`（本 change） | 唯一装配点；其他 change 只提交接线补丁，由本 change 合并 |
| `config/edge/` | `edge-gateway`（已归档） | 不手工改路由；只运行生成/校验并在缺陷修复前征得 owner 同意 |
| `deploy/compose/`、`deploy/helm/`、`deploy/argocd/` | `production-delivery` | 不重写；只做装配级核对与经 owner 签核的缺陷修复，修复记录写入证据与报告 |
| `go.mod`、`go.sum` | 各 change 提交补丁 | 本 change 统一 tidy 与合并 |
| `web/` | `action-temporal`（如审批 UI） | 本 change 只合并其产物，不新增页面 |
| `docs/stages/evidence/` | `stage-2-integration`（本 change） | 统一证据索引与原始日志目录 |
| 其他 change 的 `openspec/changes/<name>/` | 各自 owner | 不修改；缺陷退回归属 change 或记录在报告 |

## Impact

- **代码**：`cmd/ontoaxis-core/`、`internal/app/` 的装配与接线；`go.mod`/`go.sum`；`Makefile` 仅在需要增加集成脚本目标时调整；不修改业务模块内部实现。
- **部署配置**：核对并集成 `deploy/compose/`、`deploy/edge/`、`deploy/observability/`、`deploy/helm/`、`deploy/argocd/`；实际文件改动以 owner 同意为前提。
- **证据与文档**：计划新增 `docs/stages/evidence/stage-2-integration.md` 与 `docs/stages/evidence/stage-2-integration/`，起草 `docs/stages/stage-2-exit-report.md`，并同步 `ARCHITECTURE.md` 15.x、change map、README/local-development、services-and-ports 等状态文档。
- **接口/数据**：无新增或破坏性接口；不改变数据库 schema；不新增外部服务，只使用前置 change 已引入的本地/预发实例。
- **风险**：本 change 只验证与汇总，不重新拥有 S2-AC-02–10（归属各自 change）；若集成阶段发现能力 change 的缺陷，按 `design.md` 的失败处理规则退回或带签核修复，不以“集成通过”替代其 AC 证据。
