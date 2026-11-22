# action-temporal 文档与共享文件补丁

> 本文件是交给 `stage-2-integration` / `production-delivery` 合并的补丁说明，不直接修改共享文档。

## services-and-ports.md

新增：
- `ontoaxis-actions-worker`：HTTP `/healthz`、`/readyz`、`/metrics`，默认端口 `18086`（worker HTTP，指标同端口）。
- Temporal server：客户端 `7233`；Temporal UI `8080`（仅本地 profile）。
- 说明：生产环境的 worker 副本数、Temporal 持久化与网络策略由 `production-delivery` 的 Helm/ArgoCD 资产决定。

## runtime-topology.md

新增组件：
- `ontoaxis-actions-worker`（独立进程）：注册 `ActionExecutionWorkflow` 与活动，连接 Temporal namespace `ontoaxis`、task queue `ontoaxis-actions`。
- Temporal：工作流编排引擎；core 仅作为客户端提交/取消/描述，不内嵌 Temporal SDK 运行时。
- 状态：`queued/running/waiting_approval/compensating/succeeded/failed/cancelled/timed_out/compensation_failed`；worker 负责推进 `attempt/current_step_index`。
- 依赖：Postgres（执行/步骤/审批/幂等表）、对象服务（副作用活动）、OpenFGA/Cedar（API 授权）。

## ARCHITECTURE.md §5.1 / §7.3 差异建议

- §5.1 `action.action_executions` 追加列：`object_id`、`definition_snapshot`、`workflow_id`、`workflow_run_id`、`attempt`、`current_step_index`、`cancel_requested_at`、`approval_state`、`failure_code`、`failure_message`。
- 新增表：`action.execution_steps`、`action.approval_tasks`、`action.idempotency_keys`（迁移 `20261122200000_action_temporal.sql`）。
- §7.3 增加 Temporal 组件与 worker 的部署边界：core 不直接执行工作流；worker 独立二进制；compose 片段仅本地 profile，生产由 `production-delivery` 合并。
- 无公共 proto/RPC 契约变更；审批通过 REST `/api/v1/actions/approvals` 暴露。

## 验证

- 以上内容与 `deploy/compose/action-temporal/compose.temporal.yaml`、迁移和 `internal/modules/actions/README.md` 一致。
