# Proposal: action-temporal

## Why

Stage 1 的 `actions` 模块只有契约级 `NoopAdapter`：`Run` 创建 `queued` 执行记录后直接返回，没有任何真实执行、重试、补偿或人工审批，也没有执行状态的后续投影。Stage 2 要求把动作编排推进到"真实执行"——Temporal workflow/activity、失败重试、saga 补偿与人工审批分支（Stage 2 brief S2-AC-04、`docs/stages/stage-2-change-map.md` Wave 1）。Wave 0 的 `authorization-openfga-cedar` 提供授权判定与审计接缝，`edge-gateway` 已归档；本 change 是 Wave 1 中唯一负责 Action 真实执行的交付包，落点 `internal/modules/actions/`。

## What Changes

- **真实执行引擎边界**：`Run` 通过可替换 adapter 提交 Temporal workflow；执行状态由 worker 投影回 `action.action_executions`，API 不再同步执行业务步骤。
- **Action Type 执行定义**：Action Type 增加可校验的执行定义（步骤、活动类型、重试策略、审批要求、超时），提交时快照到执行记录；定义变更不影响在途执行。
- **执行状态机扩展**：新增 `waiting_approval`、`compensating`、`timed_out`、`compensation_failed` 状态与终态约束，取消/重试规则随之调整。
- **可靠执行语义**：幂等键（提交与命令接口、workflow/activity 级）、失败重试与指数退避、按逆序执行的幂等补偿。
- **人工审批**：审批任务生命周期、批准/拒绝/过期分支、审批决定 API 与最小审批界面。
- **取消与超时**：运行中取消、排队取消、执行超时与审批超时，均走补偿后进入明确终态。
- **授权与审计**：复用 Wave 0 的授权守卫（含审批决定）与审计 Sink，新增 Action 执行审计事件；跨租户与越权默认拒绝。
- **可观测与验收**：执行/活动指标、trace 传播、worker 就绪语义；真实 Temporal + PostgreSQL 上覆盖重试、补偿、审批分支的 E2E。
- **BREAKING（默认执行模式）**：默认执行模式由 no-op 改为真实 Temporal 提交；no-op 仅在显式配置下保留为本地无外部依赖测试的降级模式，且必须在预演与执行详情中如实标注。部署侧必须提供 Temporal 实例与 worker，否则执行失败关闭（fail-closed），不得静默回落 no-op。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `actions`：修改执行入口与 adapter 语义、校验与预演、执行记录与状态操作、租户边界与错误契约；新增执行定义、状态机、幂等、重试/补偿、审批、取消/超时、授权守卫、审计事件、可观测、审批界面与真实实例 E2E 要求。

## Impact

- **代码**：`internal/modules/actions/`（api/service/repo/domain/migrations，新增 Temporal adapter、workflow/activity、worker 包）、新增 `cmd/ontoaxis-actions-worker/`、`web/`（审批页面、路由、i18n、mock 夹具、Playwright）、`scripts/`（Temporal E2E 脚本）、`.github/workflows/ci.yml`（新增 action E2E job 补丁）、`go.mod`/`go.sum`（Temporal SDK，PR 提出由集成 change 合并）。
- **依赖与共享文件**：依赖 `authorization-openfga-cedar` 的授权判定与审计 Sink（并需其授权目录补充审批决定动作，见 design 未决事项）；依赖 `edge-gateway` 的租户/路由上下文；`deploy/compose/` Temporal profile 片段由 `production-delivery` 合并；`cmd/`、`internal/app/` 装配补丁由 `stage-2-integration` 合并；证据源文件交由 `stage-2-integration` 归档至 `docs/stages/evidence/`。
- **运维**：新增 Temporal 服务（Compose profile / kind）、worker 进程与健康端点、数据库增量迁移与配置项；不回滚已写入的审计与执行历史。
- **非目标**：不引入通用 workflow 编辑器或动作插件市场；不做 Pipeline/Kestra 改造；不做多区域 HA、容量规划；不接入真实客户数据；不改变 Stage 1 其余模块契约与前端页面结构。
