# Proposal: Action Orchestration

## Why

Stage 1 已批准，`ontology-definition` 提供 object type 与 action 相关元数据模型，但动作类型、校验/预演与执行入口尚缺失。`actions` 是 Stage 1 最后一个 Wave 2 模块，需以最小垂直切片证明 Action Type 契约与可替换的 Temporal/Kestra adapter 边界，真实动作执行留到 Stage 2。

## What Changes

- 新增 `actions` 业务模块，实现 Action Type CRUD、Validate、Preview、Run、Execution 查询/取消/重试。
- 在 `action` schema 新增 `action.action_types`、`action.action_executions` 表。
- 在 `/api/v1/actions` 提供 REST API：`/types` CRUD、`/types/{id}/validate|preview|run`、`/executions` 查询与 cancel/retry。
- 提供 `Adapter` 接口与默认 `NoopAdapter`；Stage 1 不调用真实 Temporal/Kestra。
- 以 fake store + 单元测试覆盖成功、非法输入与状态拒绝路径。

**Out of scope**

- 不实现真实动作执行、Temporal/Kestra 实例或 worker。
- 不实现对象级参数 schema 的完整校验（Stage 1 仅检查 required 参数存在与 action type 状态）。
- 不实现前端 Actions 页面。
- 不修改 `go.mod`、`cmd/`、`internal/app/`、`internal/platform/`、`deploy/` 或其他模块目录。

## Capabilities

### New Capabilities

- `actions`: Action Type 元数据、校验/预演、执行记录与 adapter 替换边界的行为契约。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/modules/actions/`：`module.go`、`domain/action.go`、`repo/store.go`、`service/service.go`、`api/handler.go`、`migrations/20261122190000_actions_init.sql` 与测试。
- 新增 `/api/v1/actions` 路由；由 `stage-1-core-integration` 在总装时注册。
- 数据库新增 `action` schema 与 2 张表。
- 复用既有依赖，不新增第三方依赖。
