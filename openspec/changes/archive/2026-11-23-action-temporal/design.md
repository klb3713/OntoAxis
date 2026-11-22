# Design: action-temporal

## Context

Stage 1 的 `actions` 模块只交付了元数据、REST API 与 `service.Adapter` 接口，唯一实现是 `NoopAdapter`：`Run` 在请求线程内创建 `queued` 执行后调用 `adapter.Start`，`NoopAdapter.Start` 直接返回成功，不会真正推进任何步骤。ARCHITECTURE §7.3 规定 Action 的编排由 Temporal 负责（长事务、重试、定时器、人工审批等待、saga 补偿），`actions` 模块只持久化 Action Type 与 Execution 元数据并通过 Temporal Adapter 提交 workflow。

本变更对应 Stage 2 Wave 1 的 S2-AC-04：真实 Action 执行、重试/补偿/审批与最小审批 UI、真实实例 E2E。依赖 `authorization-openfga-cedar`（Wave 0）提供的授权与审计接缝：本设计以端口形式消费 `Authorize` 与 `AuditSink`，不自行实现授权引擎或审计后端。若该依赖尚未归档，`actions` 侧以可注入端口 + 生产 fail-closed 的方式落地，避免并行开发期出现放行路径。

现有约束：

- `internal/modules/actions/` 已存在 domain/service/repo/api/migrations 分层；`ActionService` 有 12 个 RPC；`ActionExecution` 只有 id、action_type_id、status、parameters、result、error 与时间戳。
- 当前 `action.action_executions` 缺少 `object_id`、执行定义快照、尝试次数、workflow 引用与取消时间；没有补偿、审批、步骤与幂等表。
- `web/` 没有审批页面，默认不新增页面；审批 UI 是本变更拥有的例外，由 `stage-2-integration` 合并。
- `deploy/compose/`、`deploy/helm/`、`deploy/argocd/` 归 `production-delivery`；`cmd/ontoaxis-core/`、`internal/app/`、`go.mod`/`go.sum` 归 `stage-2-integration` 合并。本变更只提交 profile 片段、新二进制源码与依赖补丁说明。
- 本地 compose 尚无 Temporal；`go.mod` 尚无 Temporal SDK。

## Goals / Non-Goals

**Goals**

- 用真实 Temporal 执行 Action：workflow 定义在 Go 代码中，activity 边界清晰、输入可序列化、幂等可重入。
- Action Type 能声明执行定义（步骤、活动类型、重试、审批、超时），提交时快照到执行记录。
- 完整执行状态机、幂等提交、重试/退避、逆序补偿、人工审批、取消与超时、租户隔离、授权守卫、审计事件与可观测性。
- 默认执行模式从 Stage 1 的 no-op 切换为真实引擎；no-op 只作为显式降级。
- 最小审批 UI 与真实实例 E2E，并提供可复核证据。

**Non-Goals**

- 不实现 Pipeline/Kestra 编排（§7.3 边界：Pipeline 走 Kestra）。
- 不做通用 DSL、可视化工作流编辑器、任意脚本执行或插件市场。
- 不做 Temporal 集群运维、多区域/多命名空间治理、备份恢复；只在本地/测试环境提供 dev 实例。
- 不做 Actions 管理页面（Action Type CRUD UI）、审批权限管理后台或 RBAC 管理。
- 不实现跨模块 outbox、消息总线或新的事件平台；审计沿用授权变更的 Sink 端口。
- 不改变其他 Stage 2 change 的能力边界，也不更新 ARCHITECTURE 正文；目标 schema 差异记录在 Open Questions 与证据中。

## Decisions

### D1. 以 Adapter 端口封装 Temporal，不把引擎类型泄漏到模块内

`internal/modules/actions/service.Adapter` 从当前 `Start(ctx, actionTypeID, executionID uuid.UUID) error` 扩展为显式执行契约：

```go
type ExecutionRef struct {
    ExecutionID uuid.UUID
    WorkflowID  string
    RunID       string
}

type Adapter interface {
    Mode() ExecutionMode // ExecutionModeTemporal | ExecutionModeNoop
    Start(ctx context.Context, in ExecutionInput) (ExecutionRef, error)
    Cancel(ctx context.Context, ref ExecutionRef, reason string) error
    Describe(ctx context.Context, ref ExecutionRef) (ExecutionInfo, error)
    Health(ctx context.Context) error
}
```

`ExecutionInput` 只包含 `TenantID`、`ExecutionID`、`ActionTypeID`、`ObjectID` 与快照引用；不携带完整 parameters（见 D10）。Temporal 实现放在 `internal/modules/actions/temporal/`，workflow/activity 定义放 `internal/modules/actions/temporal/workflow/`，与 Stage 1 `pipelines/kestra` 的 adapter 形态对齐。

理由：模块服务与应用层不 import Temporal SDK；工作流引擎替换、单测与显式 no-op 都可能。拒绝的替代方案：在 service 内直接创建 Temporal client（耦合、无法单测引擎不可用路径）；复用进程内 goroutine 队列（无持久化、无重试、无法跨重启，违反 Stage 2 要求）；用 Kestra 执行 Action（违反 §7.3 职责边界）。

### D2. 执行模式显式配置，禁止静默回落

新增配置：

| key | env | 默认 | 语义 |
| --- | --- | --- | --- |
| `actions.execution_mode` | `ONTOAXIS_ACTIONS_EXECUTION_MODE` | `temporal` | `temporal` / `noop` |
| `temporal.host_port` | `ONTOAXIS_TEMPORAL_HOST_PORT` | 空 | `temporal:7233` |
| `temporal.namespace` | `ONTOAXIS_TEMPORAL_NAMESPACE` | `ontoaxis` | 命名空间 |
| `temporal.task_queue` | `ONTOAXIS_TEMPORAL_TASK_QUEUE` | `ontoaxis-actions` | 任务队列 |
| `actions.approval_timeout` | `ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT` | `30m` | 审批决定期限上限 |
| `actions.workflow_run_timeout` | `ONTOAXIS_ACTIONS_WORKFLOW_RUN_TIMEOUT` | `5m` | 执行兜底超时 |

`execution_mode=temporal` 且缺少 `temporal.host_port` 时启动失败（fail-closed），不得回落 `noop`。`noop` 模式仅用于本地无 Temporal 的测试：`Preview` 与执行详情必须返回 `adapter:"noop"` 与降级说明，`Run` 仍走 Stage 1 的同步 no-op 语义并记录 `engine_unavailable` 风险标注。测试通过环境变量显式选择，不得按 host 探测自动切换。

理由：Stage 2 G1 要求"adapter 退化显式、无假成功"，静默回落会把基础设施故障伪装成成功。

### D3. 独立 worker 二进制，core 只提交

新增 `cmd/ontoaxis-actions-worker/main.go` 与 `internal/worker/actions/`（或 `internal/modules/actions/worker/`，最终路径由实现任务确认）：

- core：只构造 Temporal client、提交/取消 workflow、查询 describe；不注册任何 workflow/activity。
- worker：连接同一命名空间与任务队列，注册 `ActionExecutionWorkflow` 与全部 activity；暴露 `/healthz`、`/readyz`、`/metrics`（`readyz` 只反映 Temporal 连接与命名空间可用）。
- 端口约定建议 worker 18086 / metrics 19091；`services-and-ports.md` 由 `stage-2-integration` 统一同步。
- compose profile 片段 `deploy/compose/action-temporal/`（`temporal` 服务用 Temporal auto-setup dev 镜像 + `ontoaxis-actions-worker`）由本变更提供，由 `production-delivery` 合并进主 compose/Helm/ArgoCD。

理由：workflow/activity 常驻轮询需要独立部署单元，与 API 缩容/发布解耦；也避免 API 实例重复注册 worker 造成任务争抢语义不清晰。

### D4. 命名空间、任务队列与 workflow ID

- 命名空间 `ontoaxis`（env 可覆盖），工作流注册在执行端。
- 任务队列 `ontoaxis-actions`（env 可覆盖），按动作域隔离。
- workflow ID 固定 `action-execution-<execution_id>`，保证"一个执行 = 一个 workflow"；重复提交由幂等键前置拦截，Temporal 侧的 `WorkflowIDReusePolicy=REJECT_DUPLICATE` 作为第二道防线。
- workflow 类型名 `ActionExecutionWorkflow`；activity 类型名使用稳定字符串（如 `actions.ApplyStepEffect`），不使用匿名 logger 或闭包序列化。
- workflow 输入只含 ID 与租户，不含参数明文（见 D10）。

### D5. 持久化：附加列 + 三个新表，保持向后兼容

新增迁移（`internal/modules/actions/migrations/`，按现有 up/down 形态）：

- `action.action_executions` 附加：`object_id uuid NULL`、`definition_snapshot jsonb NOT NULL DEFAULT '{}'`、`workflow_id text`、`workflow_run_id text`、`attempt int NOT NULL DEFAULT 0`、`current_step_index int`、`cancel_requested_at timestamptz`、`approval_state text NOT NULL DEFAULT 'none'`、`failure_code text`、`failure_message text`（脱敏后）。
- `action.execution_steps`：执行步骤台账。`id`、`execution_id`、`step_index`、`activity_type`、`status`（pending/running/succeeded/failed/compensated/compensation_failed）、`attempt`、`input_hash`、`before_snapshot jsonb`、`after_snapshot jsonb`、`error text`、`started_at`、`finished_at`，`UNIQUE (execution_id, step_index)` 提供 activity 幂等。目标 §5.1 无此表，属本变更新增，需在实现任务中同步 ARCHITECTURE 差异说明（Open Questions）。
- `action.approval_tasks`：`id`、`execution_id`、`step_index`、`status`（pending/approved/rejected/expired）、`requested_at`、`deadline_at`、`decided_at`、`decided_by`、`comment text`、`reason_code`；`UNIQUE (execution_id, step_index)` 保证一个步骤一个待审批任务。
- `action.idempotency_keys`：`tenant_id`、`key`、`request_hash`、`execution_id`、`response jsonb`、`created_at`；`PRIMARY KEY (tenant_id, key)`，24 小时过期清理（TTL 由清理任务或查询条件实现，本变更不引入 Valkey）。

所有新表带 `tenant_id` 并建 `(tenant_id, ...)` 索引；不使用 RLS（与 Stage 1 一致），租户隔离由查询条件保证。迁移必须先于代码发布，且 down 脚本可安全回退（不删除 action 既有数据）。

### D6. 状态机与并发控制

目标状态扩展为：

```text
queued ──► running ──► succeeded
  │           │  └──► waiting_approval ──► running
  │           │  └──► compensating ──► failed | cancelled | timed_out | compensation_failed
  │           └──► failed | cancelled | timed_out
  └──► cancelled
```

终态：`succeeded`、`failed`、`cancelled`、`timed_out`、`compensation_failed`。所有状态写入使用 `WHERE id=$1 AND status IN (...) RETURNING` 的条件更新（CAS），失败即冲突；`attempt` 与 `current_step_index` 只由 worker 在持有执行推进权时更新。API 层读取不得因 worker 写入产生状态倒退；详情接口按 `updated_at` 一致性读取。

目标 §5.1 的六态（queued/running/succeeded/failed/cancelled）是本设计的子集；`waiting_approval`、`compensating`、`timed_out`、`compensation_failed` 是新增扩展，须在实现任务中补充文档与迁移检查约束（不用数据库 CHECK 限定，或用一个覆盖新旧集合的 CHECK——实现时二选一并写测试）。

### D7. 幂等

- API：所有 `POST`（Run/Cancel/Retry/Approve/Reject）要求 `Idempotency-Key`；缺失返回 `idempotency_key_required`。同 key 同 `request_hash` 返回首次结果；同 key 不同 hash 返回 `already_exists`；key 不能跨租户复用。Stage 1 的无副作用 POST（Create/Update/Delete Action Type）建议同样支持但非阻塞项，实现任务中标注。
- workflow：`WorkflowIDReusePolicy=REJECT_DUPLICATE`，workflow ID 由 execution_id 决定。
- activity：引擎至少一次语义；`execution_steps` 的 `(execution_id, step_index)` 唯一键 + `input_hash` 校验把副作用收敛为一次；重复执行已 `succeeded` 的步骤直接返回记录结果。
- 幂等记录 24h 保留；执行记录永久保留（本变更不做归档策略，见 Open Questions）。

理由：Postgres 幂等表在无 Valkey 的本地环境可用；Valkey 方案待独立变更统一缓存层后替换，接口保持 `IdempotencyStore` 端口即可。

### D8. 重试与退避

- activity 重试策略：`InitialInterval=1s`、`BackoffCoefficient=2.0`、`MaximumInterval=30s`、`MaximumAttempts=3`。定义可覆盖但受上限约束（attempts ≤ 5、最大间隔 ≤ 2m）。
- 可重试分类：Temporal/网络错误、`ErrEngineUnavailable`、上游 5xx、乐观并发冲突、`context.DeadlineExceeded`（步骤级）。
- 不可重试分类：`ErrInvalidInput`、`ErrPermissionDenied`、`ErrNotFound`、审批拒绝、定义非法；activity 返回 `temporal.NewNonRetryableApplicationError` 或包装错误类型，workflow 立即进入失败/补偿路径。
- workflow 层不对业务失败做整体重试；仅对 worker 崩溃/引擎重连等由 Temporal 自身恢复（workflow 代码必须确定性、无外部时间/随机源）。
- 重试计量：`attempt` 与步骤记录尝试数对外可见；"仍在重试"绝不映射为 `succeeded`。

### D9. 补偿（saga）

- 每个执行定义步骤可声明 `compensation`（activity 类型 + 反向参数模板）；纯读步骤可声明 `read_only: true` 无补偿；变更型步骤缺少补偿且未显式 `allow_without_compensation: true` 时定义校验失败（默认拒绝不可回滚的业务步骤）。
- 步骤成功后写入 `before_snapshot`/`after_snapshot`；失败、取消、审批拒绝或超时后，按 `step_index` 逆序调度补偿 activity；补偿 activity 幂等（如 `objects.UpdateObject` 以快照恢复，重复恢复无副作用）。
- 补偿自身使用独立重试（1s/2.0/30s/3 次）；任一补偿失败即进入 `compensation_failed`，保留 `execution_steps` 中 `compensation_failed` 与残留快照，且 API/审计必须显式呈现"未完全回滚"。
- 若存在已成功的不可补偿步骤，失败路径不报告 `failed` 而报告 `compensation_failed`（避免"失败=已回滚"的错误语义）。补偿期间取消请求只标记意图，不再中断补偿。

副作用委托：MVP 的真实业务副作用选 `objects.Service.UpdateObject`（对象属性更新）与测试夹具中的"创建/删除测试对象"步骤；activity 通过接口注入 `objects` 服务，避免 actions 直接写对象表。

### D10. 审批 workflow

- 定义步骤 `requires_approval: true` 时，workflow 在应用该步骤前创建 `approval_tasks` 行（`pending`，deadline = now + 定义超时或默认 30m），发审计 `action.approval.requested`，进入 `waiting_approval`。
- workflow 用 `workflow.Await` + `workflow.NewTimer(deadline)` 等待信号 `action.approval`（payload：`decision`、`approver_id`、`comment`、`decision_id`）。信号经 Temporal client 从 API 发出，API 负责授权、幂等与任务状态 CAS。
- 批准：任务 `approved`，审计 `action.approval.decided`，workflow 继续。
- 拒绝：任务 `rejected`、`reason_code=approval_rejected`，workflow 进入补偿并最终 `cancelled`。
- 超时：任务 `expired`，workflow 进入补偿并最终 `timed_out`。
- 重复决定：API 以 `WHERE status='pending'` CAS，只有首个成功；workflow 侧信号重复时忽略并记录。
- 参数与租户：审批读取在 API 层完成授权与租户过滤；workflow 只信 execution_id，不消费外部租户参数。
- 自审批：当前审批目录无 `action_execution:approve` action，且无"禁止自审批"约束。本设计先以 `action_execution:cancel` 之外的占位动作名隔离于 `actions` 模块内部常量，并列为 Open Question；实现任务必须等 `authorization-openfga-cedar` 提供正式 action 名后再接线（依赖顺序见 tasks）。

### D11. 授权守卫

- 新端口 `Authorizer interface { Authorize(ctx, Request) (Decision, error) }`，由授权变更的实现在 app 装配时注入；`Request` 复用其 `ResourceFact{Type,ID,TenantID,Ancestors,Status,OwnerID}` 契约（执行祖先 → Action Type）。
- 需授权操作：`action_type:run`（Run）、`action_type:read`（Preview/详情读 Action Type）、`action_execution:read/cancel/retry`；审批列表/决定映射到授权变更最终确定的 action（Open Question，暂以 `action_execution:read` + 内部审批关系校验双保险）。
- 顺序：认证 → 租户 → 输入校验（400）→ 租户内加载（跨租户 404）→ Authorize → 审计预写 → 副作用。授权错误/超时 fail-closed 返回 `service_unavailable`；拒绝返回 `permission_denied` 且不产生副作用。
- worker/activity 侧：以服务身份执行，不重新做用户授权；但每次 activity 前校验执行属于输入租户（DAO 层条件查询），越租户即 non-retryable 错误。workflow 不携带用户主体，避免长期在历史中的权限漂移被静默继承。
- 未注入 Authorizer 时：生产模式启动失败（fail-closed）；单测使用显式 `AllowAllForTest` 或 `DenyAll` 的测试实现，不得默认放行。

### D12. 审计事件

复用授权变更定义的 `AuditSink` 端口与事件信封（`schema_version`、`event_id`、`event_type`、`occurred_at`、`tenant_id`、`request_id`、`trace_id`、`actor`、`action`、`resource`、`decision`、`reason_code`、`decision_id`、`error` 等）。新增事件类型（均在 `action.` 命名空间，需在授权变更或本变更文档中登记）：

| event_type | 触发点 | 关键字段 |
| --- | --- | --- |
| `action.execution.submitted` | API Run 审计通过、提交 workflow 前 | execution_id, action_type_id, mode |
| `action.execution.cancelled` | 取消请求成功 | execution_id, reason |
| `action.execution.finished` | 进入任一终态（worker 侧） | execution_id, final_status, failure_code |
| `action.step.retried` | activity 发生重试 | execution_id, step_index, attempt, error_code |
| `action.approval.requested` | 审批任务创建 | execution_id, step_index, deadline |
| `action.approval.decided` | 批准/拒绝/过期 | execution_id, step_index, decision, approver |
| `action.compensation.finished` | 补偿结束 | execution_id, result, residual_steps |

API 侧 Sink 失败 → `audit_unavailable` 503，不创建执行、不提交 workflow；worker 侧失败 → 有限重试 + 结构化错误日志（不阻塞 workflow 终态，但必须在 spans/metrics 中体现）。禁止记录 token、完整 parameters、SQL、堆栈或敏感值；`failure_message` 落库前脱敏（长度截断 + 敏感字段替换）。

### D13. 可观测性

- 指标（前缀 `ontoaxis_`，标签仅 `module`、`operation`、`status`、`result`、`driver`、`environment` 等白名单，禁止 tenant/execution 标签）：
  - `ontoaxis_action_executions_total{status,mode}`
  - `ontoaxis_action_execution_duration_seconds{status}`（终态时延）
  - `ontoaxis_action_activity_retries_total{activity_type,result}`
  - `ontoaxis_action_approval_wait_seconds{decision}`
  - `ontoaxis_action_workflows_in_flight{task_queue}`
  - `ontoaxis_action_engine_requests_total{operation,result}`
- span：API `HTTP POST /api/v1/actions/...` → module `actions.run`；worker 内 `actions.workflow` → `actions.activity.<name>`；Temporal client 与 activity 通过 OTel 拦截器传播上下文；补偿与审批等待各自子 span。
- 日志：结构化 JSON，字段 `module=actions`、`operation`、`execution_id`、`workflow_id`、`step_index`、`attempt`、`request_id`、`trace_id`；tenant 只在日志，不进指标。
- worker readiness：`/readyz` 探测 Temporal 连接与命名空间；连接断开返回 503，恢复后自动就绪。
- 告警建议（实现任务写入 profile 片段/README，不新建规则文件）：`compensation_failed` 发生率 > 0、审批等待超过 deadline 的比例、worker readyz 持续失败、workflow 提交失败率。

### D14. API 与 proto 增量

- `ActionExecution` 扩展：`object_id`、`attempt`、`current_step_index`、`workflow_id`、`workflow_run_id`、`approval_state`、`failure_code`、`cancel_requested_at`、`steps[]` 摘要。新增字段编号从现有字段之后追加，保持 wire 兼容。
- 新增 RPC：`ListApprovalTasks`、`GetApprovalTask`、`ApproveApprovalTask`、`RejectApprovalTask`（也可并入 `ActionService`；最终以 proto 文件 `ActionService` 扩展为准，命名须与授权目录一致）。`buf breaking` 必须通过；生成代码由 `make generate` 产出并纳入 tasks。
- REST 路由：`/api/v1/actions/executions/{id}/approval` 或独立 `/api/v1/actions/approvals`（设计采用独立资源 `/approvals`，列表按租户 + 状态过滤；详见 spec）。
- 错误码新增：`invalid_definition`、`idempotency_key_required`、`already_exists`（复用）、`conflict`（复用）、`engine_unavailable`、`audit_unavailable`、`permission_denied`、`compensation_failed`（详情状态而非错误码）、`approval_expired`；均映射既有 HTTP 400/409/503/403/404。

### D15. 最小审批 UI

范围仅审批工作台，不做 Action 管理页：

- `web/src/features/approvals/`：`ApprovalListPage`（Refine `useList`，tab: pending/all，显示 action type、执行摘要、请求时间、deadline、参数摘要脱敏）、详情抽屉/页（步骤进度、审计轨迹摘要、批准/拒绝 + 备注表单）。
- 路由 `/approvals` 加入 `App.tsx` 受保护路由与 i18n（en/zh），复用现有 `PageHeader`/表单组件与错误处理模式。
- 数据层：mock provider 加审批 fixtures；real 模式走新 REST 端点；决定后以服务端返回状态刷新，不做乐观成功；401/403/503 分别给出明确提示。
- 测试：Vitest 组件测试 + Playwright mock E2E（列表、批准、拒绝、无权限、空态）；mock 仅用于 UI 验证，不得作为真实实例证据。

### D16. 测试策略与真实实例

分层：

1. **单元**：domain 校验与状态机、定义校验、idempotency key 逻辑、错误映射、activity 幂等（内存 repo fake）；workflow 用 `go.temporal.io/sdk/testsuite` 的 `TestWorkflowEnvironment` 驱动时间/信号，覆盖成功、重试（`OnActivity` 返回可重试错误）、补偿、审批批准/拒绝/超时、补偿失败、取消。
2. **真实实例集成（env-gated）**：`ONTOAXIS_ACTIONS_INTEGRATION=1` 时用 compose profile（Temporal dev server + Postgres + worker）跑真实 workflow，覆盖 activity 重试、逆序补偿、审批通过分支与负向路径；未设置环境变量时 `t.Skip`。CI 增加可选 job（仅 main/nightly 或 label 触发），由整合变更合并 CI 文件。
3. **冒烟脚本**：`scripts/actions-temporal-smoke.sh` 启动 profile、等待 worker readyz、提交 E2E 定义（步骤一对象更新成功、步骤二受控瞬时失败、步骤三永久失败），断言重试次数、逆序补偿与对象恢复、审批分支、拒绝与耗尽路径；输出 JSON 结果供证据。
4. **契约/UI**：`make proto-lint`、`make proto-breaking`、`make generate-check`；web `pnpm check`、`pnpm test`、Playwright mock E2E。
5. **E2E 证据**：写入 `docs/stages/evidence/stage-2-action-temporal.md`（由 `stage-2-integration` 汇总目录，本变更提供内容），记录环境、镜像/命名空间、执行 ID、命令与结果；环境不可用必须记 `skipped`/`planned`，不得写成通过。

测试夹具的故障注入 activity（`TestTransientFailure`、`TestPermanentFailure`）必须由 `ONTOAXIS_ACTIONS_TEST_HOOKS=1` 显式开启，生产配置默认关闭，启动时日志告警，并在 spec/证据中标注为非生产能力。

### D17. 失败路径汇总

| 触发 | 行为 | 终态 |
| --- | --- | --- |
| 授权/审计不可用（API） | 拒绝副作用，503 | 不创建执行 |
| 引擎提交失败 | 执行投影 failed，`engine_unavailable` | `failed` |
| activity 可重试错误 | 按退避重试，最多 3 次 | 成功或进入下一路径 |
| activity 耗尽重试（无已成功副作用） | 直接失败 | `failed` |
| activity 耗尽重试（有已成功副作用） | 逆序补偿 | `failed` 或 `compensation_failed` |
| 不可重试错误 | 立即补偿 | `failed` |
| 审批拒绝 | 补偿 | `cancelled` |
| 审批超时 | 补偿 | `timed_out` |
| 运行取消 | 停止调度、补偿 | `cancelled` / `compensation_failed` |
| 执行兜底超时 | 停止、补偿 | `timed_out` / `compensation_failed` |
| 补偿失败 | 保留残留快照、告警 | `compensation_failed` |
| worker 崩溃 | Temporal 重放，activity 幂等续跑 | 由最终步骤决定 |

## Risks / Trade-offs

- **新增基础设施**：Temporal 带来部署、监控、升级与容量成本；dev auto-setup 镜像不能直接用于生产。缓解：profile 片段与 Helm 值交由 production-delivery 统一治理，本变更只声明契约与本地验证。
- **授权目录缺口**：`action_execution:approve` 尚不存在，审批决定暂时映射到 `action_execution:cancel` 或内部关系校验，存在权限语义偏差风险；必须与授权变更负责人确认后落地。
- **跨系统副作用不能完全补偿**：对象更新可快照恢复，但外部 API 调用不可逆；设计以定义校验默认拒绝不可补偿变更步骤 + `compensation_failed` 显式呈现，仍存在残留风险。
- **幂等边界**：activity 幂等依赖 ledger；若副作用在写 ledger 前成功、写 ledger 失败，重试会重复副作用。缓解：先写"意图/attempt"再执行、成功后 CAS 写结果；对象更新本身以快照覆盖，重复应用等价（幂等更新）。
- **目标 schema 漂移**：新状态与新表超出 ARCHITECTURE §5.1 目标定义；需在实现任务中同步文档，评审接受前不得声称与架构一致。
- **Breaking 默认模式**：Stage 1 用户/测试依赖 no-op 成功语义；切到 temporal 后本地未配置 Temporal 会启动失败。缓解：`actions.execution_mode=noop` 显式开关、迁移说明与 CI compose-smoke 使用显式 noop 或启动 Temporal profile。
- **Temporal SDK 新依赖**：增加构建体积与 license/供应链审查；Temporal Go SDK 为 MIT，需走依赖评审（AGENTS.md 要求说明用途与替代方案）。
- **worker 部署归属未定**：新二进制由谁打包、Helm 与 ArgoCD 如何编排尚未确定，可能造成本变更验收依赖 production-delivery 的排期。
- **审批 UI 权限与租户**：前端只展示服务端返回；若授权/租户解析缺陷，审批数据可能越权展示，需 E2E 覆盖跨租户 404 与无权限态。
- **CI 成本**：真实实例 job 较重，必须 opt-in/label 触发，避免每个 PR 都拉 Temporal。

## Migration Plan

1. **数据库**：先应用 additive 迁移（新列 + 三张新表），旧行 `definition_snapshot='{}'`、`workflow_*` 为空、`attempt=0`；旧状态值不变。down 脚本只删除新表/新列，不触碰既有数据。
2. **代码**：先发布 worker（注册 workflow/activity，暂不接收新执行，可先部署但 queue 无流量）；再发布 core，把 `actions.execution_mode` 默认设为 `temporal` 并按环境显式配置 `noop`（仅本地无 Temporal 环境）；最后启用 gateway/授权装配。发布顺序写入 rollout 文档。
3. **兼容**：Stage 1 的 `NoopAdapter` 保留为 `noop` 模式实现，既有测试改为显式选择模式；`Preview` 与执行详情按模式返回 `adapter` 字段，避免旧响应断言失效。
4. **回滚**：把执行模式切回 `noop` 只影响新提交；已提交 workflow 需要等待完成或取消（worker 需保留）。数据库列可保留不删；回滚步骤与风险写入证据文档。
5. **文档与证据**：实现后同步 `services-and-ports.md`、`runtime-topology.md`（Temporal 端口/组件）与 ARCHITECTURE 差异说明；证据写入 `docs/stages/evidence/stage-2-action-temporal.md`；归档前跑 `openspec validate --strict` 与整合门禁。

## Open Questions

1. 审批决定的授权 action 名与授权负责人：是否在 `authorization-openfga-cedar` 中新增 `action_execution:approve`（需要关系/策略评审）；未确认前实现只能采用占位并标注风险。
2. 是否禁止自审批（提交人 = 审批人）；当前授权文档无此约束，若需要应作为 Cedar 条件或服务端显式规则，属于安全评审项。
3. worker 二进制与 compose/Helm 编排的归属：由 `stage-2-integration` 还是 `production-delivery` 负责打包与发布；需在 tasks 依赖中显式标注。
4. `execution_steps`/`approval_tasks`/`idempotency_keys` 的留存与清理策略（期限、清理任务归属）；24h 幂等 TTL 是否满足审计与重试窗口。
5. 目标 §5.1 的 execution status 枚举是否在本变更内更新为扩展集合，还是保留目标文档并在实现任务中记录差异。
6. 是否需要 `object_id` 在 API 层成为必填（Stage 1 允许无对象执行）；本设计假设 MVP 仍允许 `NULL`，由执行定义区分对象型/系统型动作。
7. 执行定义的 schema 版本化策略：未来定义结构变化时如何兼容已快照的在途执行（当前以快照原样执行，不做版本迁移）。
8. 外部不可逆副作用的允许范围与审批规则：是否要求 `allow_without_compensation` 必须叠加审批，作为安全默认。
9. 审批 UI 的导航位置（侧边栏入口/通知）与是否需要 badge 计数；最小实现可先只做路由与列表。
10. 真实实例 E2E 在 CI 的触发方式（label、nightly 还是手动）与超时预算。

### 实现期决议（2026-11-23，随 tasks 1.3 关闭）

- 目标 §5.1 不在本 change 内改写为扩展状态集合；实现采用 additive 迁移（execution 附列 + 三张新表）与 REST 投影，目标文档差异由 `stage-2-integration` 在 Stage 2 收口时统一更新。理由：§5.1 属目标设计而非迁移契约，改写会与并行 change 冲突；差异已在 `integration-patch.md` 记录。
- `action_executions` 不加 CHECK 约束，状态合法性由 domain CAS 保证；迁移只加列/索引/唯一键，down 仅删除本变更新增对象。
- 真实业务副作用（`objects.Service.UpdateObject`）仍待整合注入；当前 worker 以 `NoopEffectApplier` 装配，`integration-patch.md` 明确标注，不得宣称副作用可用。

### 实现期决议（2026-11-23，随 tasks 1.1/1.2/1.4/2.6 关闭）

- **1.1 审批授权 action**：新增 `action_execution:approve`（admin 组，映射 `can_write`），已加入 `internal/platform/authz/catalog.go` 与 `policies/platform/schema.cedar.json`；`/approvals/{id}/approve|reject` 不再复用 `action_execution:cancel`。
- **1.2 worker 归属**：`ontoaxis-actions-worker` 二进制、代码与本地 compose profile 归 `action-temporal`；Helm/ArgoCD/生产编排由 `production-delivery` 负责；`internal/app`/`cmd/ontoaxis-core` 接线由 `stage-2-integration` 应用 `integration-patch.md`。
- **1.4 自审批/不可逆/保留**：当前安全模型未禁止自审批（审批人需 `action_execution:approve`，即 tenant-admin/platform-admin）；不可逆副作用必须显式 `compensation`，否则需 `allow_without_compensation` 并由审批门控；幂等 TTL 24h；execution_steps/approval_tasks 随执行保留，清理任务归部署运维，未在本 change 实现。
- **2.6 迁移说明**：`internal/modules/actions/README.md` 已记录迁移顺序、执行附列/三张新表语义与 down 仅删新增对象的回滚边界。
