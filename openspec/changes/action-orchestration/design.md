# Design: Action Orchestration

## Decisions

### D1. 模块边界

`internal/modules/actions/` 包含 `module.go`、`domain/`、`repo/`、`service/`、`api/`、`migrations/`。模块依赖 `ontology`，但 Stage 1 不做 object type 存在性实时校验；集成 change 通过 ontology service 注入校验。

### D2. Action Type 与 Execution

`action.action_types` 保存名称、展示名、参数定义与状态；`action.action_executions` 保存参数快照、状态、结果/错误与时间戳。执行 ID 与 Action Type ID 使用 UUIDv7。

### D3. Adapter

`service.Adapter` 接口固定为 `Start(ctx, actionTypeID, executionID) error`；默认 `NoopAdapter` 返回 nil，不执行真实工作流。Stage 2 可用 Temporal/Kestra adapter 替换，不改 API。

### D4. 状态机

Execution 状态为 `queued/running/succeeded/failed/cancelled`。Run 创建 `queued`；Cancel 拒绝 `succeeded/cancelled`；Retry 只允许 `failed/cancelled` 并创建新记录。Stage 1 不自动推进状态，adapter 可实现。

### D5. 错误与租户

使用 `internal/platform/httperr` 统一错误信封；租户从 `authn.FromContext` 或 `X-Tenant-Id`。所有 repo SQL 显式带 `tenant_id`，Stage 1 不启用 RLS。

## Risks

- 参数 schema 校验仅覆盖 `required` 列表，不覆盖类型/范围。
- Noop adapter 不会真正执行动作，Stage 1 只验证 API 与 adapter 边界。
- 真实 PostgreSQL 迁移/状态机集成验证留给 `stage-1-core-integration`。
