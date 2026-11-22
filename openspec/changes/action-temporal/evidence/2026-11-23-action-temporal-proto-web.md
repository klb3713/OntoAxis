# Action Temporal Proto / Web Gates Evidence

> 2026-11-23，本地 macOS x86_64。对应 tasks 5.1、11.3；5.2 的 `make generate-check` 依赖提交后的干净工作区，当前保留为 open。

## Proto（5.1）

`proto/ontoaxis/v1/actions/actions.proto` 追加（字段编号只增不改）：

- `ActionExecution` 新增 `object_id/attempt/current_step_index/workflow_id/workflow_run_id/approval_state/failure_code/cancel_requested_at/steps`
- 新增 `ExecutionStep`、`ApprovalTask`
- 新增 RPC `ListApprovalTasks`、`GetApprovalTask`、`DecideApprovalTask`
- 新增 `ListApprovalTasksRequest/Response`、`GetApprovalTaskRequest`、`DecideApprovalTaskRequest`

验证：

```text
$ make proto-lint      # buf lint via docker, exit 0
$ make proto-breaking  # buf breaking against main, exit 0
```

生成（5.2 部分）：

```text
$ make generate        # exit 0
 M gen/ontoaxis/v1/actions/actions.pb.go
 M gen/ontoaxis/v1/actions/actionsconnect/actions.connect.go
 M web/src/generated/ontoaxis/v1/actions/actions_pb.ts
```

## Web（11.3）

```text
$ cd web && pnpm check          # tsc --noEmit，通过
$ pnpm test                     # 6 files / 33 tests passed
$ pnpm build                    # built successfully
$ pnpm exec playwright test e2e/approvals.spec.ts
  4 passed (3.1s)
```

新增 `web/e2e/approvals.spec.ts`：列表→批准→服务端刷新、拒绝、无权限、处理完全部待审批后的空态；4 passed。mock provider 仅在测试 flag `__MOCK_APPROVAL_FORBIDDEN__` 下返回 403。

## 限制

- `make generate-check` 会因生成文件未提交而失败，待提交后由 CI 验证；不伪造通过。
- Playwright 为 mock UI 证据，不是真实后端审批证据。
- 未 commit、未 archive。


## Worker 审计（4.6）

`cmd/ontoaxis-actions-worker/main.go` 将 `Activities.Audit` 接到结构化 `slog`：

```text
event, tenant_id, execution_id, step_index, status, failure_code
```

Activities 已发射 `action.execution.started/submitted`、`action.approval.requested/approved/rejected`、`action.step.compensated/compensation_failed`、`action.execution.finished/succeeded/failed/cancelled/timed_out/compensation_failed`。审计回调失败不会伪造成功：事件写入失败由上层 worker 日志暴露，执行状态仍由 Temporal 重试/状态机决定。

验证：`go build ./cmd/ontoaxis-actions-worker/`、`go test ./internal/modules/actions/...` 通过。


## 幂等提交（2.5 / 5.3）

- `repo.RuntimeStore` 暴露 `PutIdempotency`/`GetIdempotency`；Postgres 实现按 `(tenant_id, key)` 唯一，同 key 同 hash 返回首记录，异 hash 返回 `ErrConflict`，24h TTL 由迁移默认值/查询条件约束；`TestRuntimeIdempotency` 覆盖重复、冲突、空 key、缺失。
- `service.WithIdempotencyKey(ctx,key)` + `Run`：创建执行前查记录；同 key 同 hash 直接返回首执行（不再启动引擎），异 hash 返回 `ErrConflict`；创建后写入记录，并发冲突时把本次创建的 queued 执行投影为 `failed/duplicate_submission` 并返回首执行。
- API `POST /api/v1/actions/types/{id}/run` 读取 `Idempotency-Key`（≤255），透传到 service。
- 测试：`TestRunIsIdempotentByContextKey`（二次提交同 ID、引擎只启动一次、改 payload 409）；`go test ./internal/modules/actions/...` 通过。

限制：`make generate-check` 仍因生成文件未提交而失败，5.2 保持 open。


## 授权接缝（6.2 / 6.3）

- API 使用 `authz.DecisionGuard`，在 `internal/app/runtime.go` 注入；`actions.Module.RequireDecisionGuard()` 在装配阶段检查，缺失则拒绝启动。
- 动作映射：`action_type:run/read/update/delete/validate/preview`、`action_execution:read/cancel/retry/approve`；资源 fact 带 action-type 祖先。
- 顺序：认证→租户→输入校验→租户内加载→授权→审计→副作用；跨租户先 404。
- 测试覆盖：allow + decision header、deny 403 且无副作用、跨租户 404 且未决策、授权引擎超时 503 且无副作用、无租户上下文 401、列表父 scope。
- 命令：`go test ./internal/modules/actions/api/ ./internal/app/...` 通过。


## API 审计（6.5）

- `api.AuditSink` + `WithAuditSink`；`internal/app/runtime.go` 注入 `slogAuditSink`，`actions.Module.SetAuditSink` 透传到 handler。
- 审批决定在授权之后、服务调用之前发 `action.approval.decided`；sink 失败返回 503，且不调用 `DecideApprovalTask`（`TestApprovalDecisionFailsClosedWhenAuditFails`）。
- Worker 侧 `action.execution.*`/`action.step.*`/`action.approval.*`/`action.compensation.*` 事件通过 `Activities.Audit` 输出，失败不伪造成功。
- 限制：执行提交/取消的 API 侧事件当前由 worker 终态事件覆盖，未在 API 再发一份；如需严格 API 事件，后续可扩展。


## 最终门禁（2026-11-23）

```text
$ make build                # exit 0
$ make test                 # exit 0（全部 package ok）
$ make test-race            # exit 0
$ make vet                  # exit 0
$ make license-check        # exit 1（已知：github.com/nexus-rpc/nexus-proto-annotations 无许可证元数据）
```

`openspec validate action-temporal --strict` 通过；当前 51/55。

剩余：5.2（generate-check 需提交后验证）、7.2（API/workflow/activity span 未接）、9.2（真实 Temporal+Postgres env-gated 集成测试未写）、11.1（license-check 失败）。


## 7.2 / 11.1（2026-11-23）

- API span：`internal/modules/actions/api/tracing.go`，`action.api.run|cancel|retry|approve|reject`，无 tenant/user/execution/object 属性。
- Worker span：`internal/modules/actions/temporal/tracing.go`，首个 activity 边界打开 `action.workflow.execute`，各 activity 打开 `action.activity.<Name>`，无业务标识属性。
- 标签白名单：`internal/modules/actions/worker/worker_test.go` 断言指标体不含 `tenant_id`/`execution_id`/`object_id`/`user_id`。
- `go test ./internal/modules/actions/...` 通过（含 `TestActionSpansAreBounded`、`TestAPISpanIsBounded`）。
- license 例外：`Makefile` `license-check` 增加 `--ignore github.com/nexus-rpc/nexus-proto-annotations`（经负责人批准，依赖树其余包仍检查）；`make license-check` exit 0。


## 9.2 真实 Temporal + Postgres（2026-11-23）

新增 `internal/modules/actions/temporal/integration_test.go`（`//go:build integration`），未设置环境变量时 skip，设置时执行真实 Temporal workflow + 真实 Postgres repo：

```text
ONTOAXIS_ACTIONS_INTEGRATION=1
ONTOAXIS_TEST_DATABASE_URL=postgres://ontoaxis:ontoaxis@postgres:5432/ontoaxis?sslmode=disable
ONTOAXIS_TEST_TEMPORAL_HOST=temporal:7233
```

在 compose `postgres + temporal + core` 网络内运行：

```text
go test -tags=integration -run TestActionWorkflowWithRealTemporalAndPostgres -v ./internal/modules/actions/temporal/
--- PASS: TestActionWorkflowWithRealTemporalAndPostgres (0.47s)
```

当前 54/55；仅剩 5.2（`make generate-check` 需要生成文件已提交）。
