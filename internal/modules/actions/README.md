# actions module

执行动作类型（ActionType）与动作执行（Execution）。Stage 2 起执行由 Temporal 承载，本模块只负责定义、准入、状态投影、审批与取消/重试；不直接执行业务步骤。

## 执行定义

`ActionType.parameters.execution_definition` 接受：

```json
{
  "steps": [
    {
      "id": "update-object",
      "activity_type": "object.update",
      "parameters": {"object_type_id": "...", "object_id": "...", "properties": {}},
      "requires_approval": false,
      "read_only": false,
      "compensation": {"activity_type": "object.update", "parameters": {}}
    }
  ],
  "retry_policy": {"max_attempts": 3, "initial_interval": "1s", "backoff": 2.0, "max_interval": "30s"},
  "approval_timeout": "30m",
  "workflow_timeout": "5m"
}
```

- 变更型步骤必须提供 `compensation`，否则 `ValidateExecutionDefinition` 拒绝；纯读步骤用 `read_only: true`。
- 已知 `activity_type`：`object.update`、`object.create`、`object.delete`、`noop`、测试专用 `test.transient_failure` / `test.permanent_failure`。
- 步骤上限 50；重试最多 3 次、初始间隔 1s、上限 30s。

## 状态机

`queued → running → succeeded | failed | cancelled`，并扩展 `waiting_approval`、`compensating`、`timed_out`、`compensation_failed`。终态不可回退；所有写入使用 CAS。

## 执行模式

- `ONTOAXIS_ACTIONS_EXECUTION_MODE=temporal`（默认）：需要 `ONTOAXIS_TEMPORAL_HOST_PORT`；缺失时 `Module.InitError()` 非空，模块 fail-closed。
- `ONTOAXIS_ACTIONS_EXECUTION_MODE=noop`：仅用于本地/测试，Preview 与执行响应会标注 `noop`；不得用于生产验收。
- 相关 env：`ONTOAXIS_TEMPORAL_NAMESPACE`、`ONTOAXIS_TEMPORAL_TASK_QUEUE`、`ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT`、`ONTOAXIS_ACTIONS_WORKFLOW_RUN_TIMEOUT`。

## 审批

`requires_approval` 步骤在 workflow 中创建 `action.approval_tasks` 并等待信号 `action.approval`。REST：

- `GET /api/v1/actions/approvals?status=pending`
- `GET /api/v1/actions/approvals/{id}`
- `POST /api/v1/actions/approvals/{id}/approve`
- `POST /api/v1/actions/approvals/{id}/reject`

批准/拒绝必须携带 `Idempotency-Key`。当前授权复用 `action_execution:cancel` 占位；授权目录新增 `action_execution:approve` 后切换。

## 迁移

`migrations/*.up.sql` 按文件名排序执行；`20261122200000_action_temporal.sql` 新增执行附列、`execution_steps`、`approval_tasks`、`idempotency_keys`。down 文件仅供回滚演练。

## 已知限制

真实副作用当前由 worker 的 `NoopEffectApplier` 占位；真实 Temporal+Postgres E2E、worker compose profile、proto 字段扩展与审计 sink 注入尚未完成。见 `openspec/changes/action-temporal/integration-patch.md`。