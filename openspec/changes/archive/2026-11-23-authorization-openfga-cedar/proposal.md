# Proposal: authorization-openfga-cedar

## Why

Stage 2 的 S2-AC-03 要求 OpenFGA 关系授权与 Cedar 属性/动作策略真正生效：关系与属性各有允许/拒绝用例，跨租户、越权、无策略默认拒绝与策略变更审计均可复核。当前仓库（见 `docs/architecture/authorization-boundary.md` §15）只有认证链路：`internal/platform/authn` 把 JWT claims 转成 `TenantContext`，业务模块的 `api/` handler 仅凭内部租户头做租户绑定，没有任何对象级授权，角色也没有被强制。

`edge-gateway` 已归档，Wave 0 只差本 change。授权边界文档 `docs/architecture/authorization-boundary.md` 已定稿，负责人与安全评审已于 2026-11-22 确认（§13.2/§13.3）；该文档是本 change 唯一的行为边界，本 change 不得削弱或扩大它。

本 change 是 Wave 0 最后一个 change，为后续所有模块 change 提供唯一的授权判定点，避免各模块各自实现角色/租户/管理员判断而形成互相冲突的检查。

## What Changes

- 新增 capability `authorization`：单一授权判定点、OpenFGA 关系判定、Cedar 属性/动作策略、默认拒绝（含依赖故障）、租户层级与资源事实、审计决策事件、授权守卫、Explain 诊断、负向测试与安全评审门。
- 新增 `internal/platform/authz` 包：`Authorize(ctx, Request) → Decision` 契约、动作目录、上下文元组构造、OpenFGA `Check` 客户端、Cedar 求值、审计 sink、默认拒绝到 HTTP 的映射、守卫。
- 新增策略资产：`policies/openfga/model.fga`（v1 关系模型）与 `policies/platform/schema.cedar.json`、`tenant-isolation.cedar`、`role-matrix.cedar`、`tenancy-scope.cedar`、`resource-state.cedar`（`go:embed`，启动编译，摘要写审计）。
- 模块接缝：`internal/modules/*/api/` 的每个业务 handler 声明已登记动作与资源事实，在副作用前调用守卫；handler 只保留输入校验和租户内加载，不再自写角色/同租户/管理员判断，也不直接调用 OpenFGA/Cedar。
- 审计：`authz.decision`（每次判定）、`authz.policy_change`（启动）、`authz.explain`，append-only JSON stdout，同步写、失败 fail-closed、字段脱敏。
- 启动门禁：配置校验、Cedar 编译与 schema 交叉校验、OpenFGA store/model/零元组校验、动作目录交叉校验、policy_change 审计、readyz 纳入 authz。
- Explain/diagnostics：只允许调用者本人对目标资源的决策，需 `authorization:explain`（admin 组）并写 `authz.explain`。
- 默认拒绝矩阵与负向测试：跨租户、越权、缺失事实、依赖故障逐条自动化；集成测试使用真实 OpenFGA（固定版本、专用 store、上传模型）与真实 cedar-go，并挂真实业务 handler。
- **BREAKING**：Stage 1 的宽松行为按 v1 收紧——viewer 只读；写/删/调用需 member+；组织/工作空间/项目管理需 workspace-admin+；SavedQuery 删除需 owner 且 delete 组（viewer owner 也拒绝）；Pipeline 仅 published 可触发；ActionType 仅 active 可运行；Connector 仅 active 可同步；行动执行取消/重试受状态机约束。原先可能成功的越权请求会变为 403/404。
- 安全评审门：`docs/architecture/authorization-boundary.md` 已定稿，默认拒绝矩阵与审计字段已于 2026-11-22 通过安全评审；实现不得扩大或削弱边界。

## Capabilities

### New Capabilities

- `authorization`：OntoAxis 的唯一授权判定点与行为契约，覆盖关系授权（OpenFGA）、属性/动作策略（Cedar）、默认拒绝与依赖故障、租户层级与资源事实、审计决策事件、授权守卫集成、Explain 诊断、负向测试与安全评审门。

### Modified Capabilities

无。本 change 不建立第二套授权行为真源：Stage 1 → v1 的角色矩阵与资源状态收紧由 `authorization` 的守卫契约统一表达，并以本 change 的 delta spec 作为可评审的行为契约。边界文档 §12.1 要求的模块级 delta（`query-surface`、`pipelines`、`actions`、`connectors`、`ingestion`、`tenancy`）按 `docs/stages/stage-2-change-map.md` 由对应模块 change 在各自 delta 中同步；本 change 先锁定守卫与用例，避免同一行为出现两份可分歧的规格（见 `traceability.md`「未覆盖/后续」）。

## Impact

- 代码：新增 `internal/platform/authz/`（config/decision/catalog/tuples/fga/cedar/audit/guard/errors/metrics/trace 等文件）与 `policies/`；修改 `internal/modules/*/api/` 授权接缝；`go.mod`/`go.sum` 增加 OpenFGA SDK、cedar-go、testcontainers 依赖（本 change 提交补丁，由 stage-2-integration tidy）；`internal/app/`、`cmd/` 的构造函数与接线以 patch 交给 stage-2-integration，本 change 不直接修改。
- 配置与部署：新增 `ONTOAXIS_AUTHZ_*` 配置与禁用项检测（`AUTHZ_FAIL_OPEN`/`AUTH_DISABLED` 出现即启动失败）；OpenFGA 实例、store 与 model 由部署方/流水线提供，本 change 不修改 `deploy/`（归 production-delivery），仅给出启动校验与配置契约；readyz 增加 authz 维度。
- 行为：认证之后、副作用之前的统一授权；模块不再自写角色/租户/管理员判断；Stage 1 宽松行为收紧（**BREAKING**）；未注册路由仍保持 404。
- 数据：无数据库迁移；不读写 `dataset_permissions`；v1 不写持久关系元组；审计只写 stdout JSON，不建审计表。
- 证据：默认拒绝矩阵逐行结果、角色×组矩阵、testcontainers 集成测试原始输出与 CI job、审计样本与 decision id 一致性；原始输出交 integration 放入 `docs/stages/evidence/stage-2-authorization/`。
- 依赖与门禁：依赖已归档的 `edge-gateway`；实现依赖安全评审签核（blocking）；依赖固定版本 OpenFGA 与 cedar-go；负责人已确认 §13.2 决策。`openspec validate authorization-openfga-cedar --strict` 必须通过后才提交评审。
