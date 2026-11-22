# Integration patch: action-temporal

> 本文件记录本 change 无法直接修改的共享文件/部署归属，供 `stage-2-integration` 与 `production-delivery` 合并。
> 状态：2026-11-23，unit 实现完成；真实 Temporal E2E 待整合。

## 1. 应用装配（`internal/app/`、`cmd/ontoaxis-core/`）

1. `actions.New(pool)` 现在会读取执行引擎配置：
   - `ONTOAXIS_ACTIONS_EXECUTION_MODE`：`temporal`（默认）或 `noop`。
   - `temporal` 模式要求非空 `ONTOAXIS_TEMPORAL_HOST_PORT`；否则 `Module.InitError()` 非空、`Ready` 返回错误，服务路由不注册（fail-closed）。
   - 本地/测试无 Temporal 时显式设置 `ONTOAXIS_ACTIONS_EXECUTION_MODE=noop`，不得省略后期望自动降级。
2. 启动顺序建议：构造 modules → 检查每个模块的 `InitError()`（actions 暴露）→ 迁移 → 启动 HTTP。`Ready` 已包含 Temporal `CheckHealth`；`Close` 关闭 Temporal client（`Module.Close()`，需在应用关闭时调用）。
3. 建议的 env（由 `internal/platform/config` 合并）：
   - `ONTOAXIS_TEMPORAL_HOST_PORT=temporal:7233`
   - `ONTOAXIS_TEMPORAL_NAMESPACE=ontoaxis`
   - `ONTOAXIS_TEMPORAL_TASK_QUEUE=ontoaxis-actions`
   - `ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT=30m`
   - `ONTOAXIS_ACTIONS_WORKFLOW_RUN_TIMEOUT=5m`
4. Temporal adapter 目前由 actions 模块直接构造（`internal/modules/actions/temporal`），共享 `internal/platform/config` 的键未合并；整合时可改为平台 config 读取后 `actions.NewWithAdapter` 或保留 env 读取。
5. `go.mod` 新增 `go.temporal.io/sdk v1.51.0`、`go.temporal.io/api v1.64.3` 及间接依赖；本 change 已提交 go.mod/go.sum，整合负责 `go mod tidy` 复核。

## 2. Worker（`production-delivery`）

- 新二进制 `cmd/ontoaxis-actions-worker`，环境变量：
  - `ONTOAXIS_DATABASE_URL`（必需，temporal 模式）
  - `ONTOAXIS_TEMPORAL_HOST_PORT`、`ONTOAXIS_TEMPORAL_NAMESPACE`、`ONTOAXIS_TEMPORAL_TASK_QUEUE`
  - `ONTOAXIS_ACTIONS_WORKER_HTTP_ADDR`（默认 `:18086`）
- 健康/就绪/指标：`/healthz`、`/readyz`（Temporal CheckHealth）、`/metrics`（建议 metrics 端口 19091，由部署映射）。
- 生产副作用：`temporal.NoopEffectApplier` 是占位；启用 `object.update` 前必须注入 `objects.Service.UpdateObject` 适配器（快照/补偿），否则对象步骤不会产生业务效果。
- 需要 compose profile 片段（`deploy/compose/action-temporal/compose.temporal.yaml`：Temporal auto-setup dev 镜像 + worker）与 Helm/ArgoCD 清单；本 change 未创建（归属 production-delivery）。

## 3. 数据库

- 迁移 `internal/modules/actions/migrations/20261122200000_action_temporal.sql` 为 additive；`module.MigrationStatements()` 已按文件名排序执行所有 up 文件。整合确认 goose/迁移 runner 与 `db.ExecStatements` 兼容，并在预发执行 down 回滚演练。
- 新表：`action.execution_steps`、`action.approval_tasks`、`action.idempotency_keys`；`action.action_executions` 附加 10 列。无 RLS（与 Stage 1 actions 一致），租户隔离由查询条件保证。

## 4. 授权/审计

- 审批决定当前复用 `action_execution:cancel` 守卫；授权目录没有 `action_execution:approve`。整合/后续授权变更需新增该 action 与 Cedar/OpenFGA 映射后切换 `api/approvals.go` 注释处。
- 审计事件已由 `temporal.Activities` 的 Audit 回调发出（若注入）；API 侧审批暂未接 `authz` AuditSink，需 stage-2-integration 注入后断言 `action.approval.decided` 等事件。未注入时不得声称审计通过。
- 自审批限制、不可逆副作用默认策略、幂等/步骤台账保留期仍未决策（design Open Questions）。

## 5. 契约与前端

- 未改 proto；执行扩展字段通过现有 REST 执行响应暴露，审批通过 `/api/v1/actions/approvals` REST 暴露。若坚持 proto/buf 门禁，需在整合前补 proto 或把本 change 的 proto 任务标记为后续。
- 前端 `/approvals` 已接入；mock 测试通过，真实后端 E2E 未跑。
- Playwright `web/e2e/approvals.spec.ts` 未创建；审核时可作为 follow-up。

## 6. 验证待办（不得虚报）

- 真实 Temporal+Postgres E2E、`ONTOAXIS_ACTIONS_INTEGRATION=1` 集成测试、`scripts/actions-temporal-smoke.sh`、CI job、`docs/stages/evidence/stage-2-action-temporal.md` 均未完成。
- `make proto-lint/proto-breaking/generate-check`、`make license-check` 未运行。
- 连接真实 Temporal 后需覆盖：重试、逆序补偿、审批批准/拒绝/超时、取消、`compensation_failed`、跨租户 404、审计失败 fail-closed。