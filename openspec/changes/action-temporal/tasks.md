# Tasks

> 本文件为规划产物，所有任务在实现并产生证据前保持未勾选。每个任务写明确认方式；组内测试与文档随组落地，不设"最后统一补测试"的任务。实现时禁止修改其他 change 目录，go.mod/go.sum、`cmd/ontoaxis-core/`、`internal/app/`、`deploy/` 主文件按 change-map 共享所有权以补丁/PR 交由对应负责人合并。

## 1. 决策确认与依赖对齐

- [x] 1.1 与 `authorization-openfga-cedar` 负责人确认审批决定的授权 action 名与关系（`action_execution:approve` 是否新增、或映射到既有 action），并把结论写回 `design.md` Open Questions。验证：design.md 更新、授权侧有对应条目或书面结论。
- [x] 1.2 确认 worker 二进制打包、compose/Helm/ArgoCD 编排归属（`stage-2-integration` 与 `production-delivery` 分工），记录到 design/traceability。验证：归属与依赖顺序写入 design.md 与 tasks 依赖说明。
- [x] 1.3 确认目标 ARCHITECTURE §5.1 的 execution 状态枚举与新表差异处理方式（同变更更新文档或仅记录差异）。验证：结论写回 design.md Open Questions。
- [x] 1.4 确认自审批限制、不可逆副作用默认策略与幂等/步骤台账保留期限。验证：design.md Open Questions 关闭或转为明确风险条目。

## 2. 领域模型、执行定义与持久化

- [x] 2.1 在 `internal/modules/actions/domain/` 增加执行定义类型（steps、activity types、retry、approval、timeout、compensation、read_only/allow_without_compensation）与 `ValidateDefinition`，覆盖非法 activity、步骤上限、重试/超时越界、变更步骤缺补偿等拒绝路径。验证：domain 单测覆盖合法与全部拒绝用例。
- [x] 2.2 扩展执行状态常量与转换表，落地 CAS 语义的 `CanTransition`/终态判定。验证：状态机表驱动单测覆盖合法转换、非法转换与终态不可变。
- [x] 2.3 新增迁移 `internal/modules/actions/migrations/*_action_temporal.up.sql`/`.down.sql`：`action_executions` 附加列、`action.execution_steps`、`action.approval_tasks`、`action.idempotency_keys`，含唯一键与租户索引。验证：`make test` 的迁移测试（或 repo 集成测试）可 up/down 并断言约束生效。
- [x] 2.4 扩展 `repo.Store`：execution 附加字段读写、步骤台账 upsert/查询、审批任务 CRUD/CAS、幂等键 get/put、租户条件查询与 `mapPgError` 映射。验证：repo 测试覆盖成功、冲突、跨租户 404 与空结果。
- [x] 2.5 实现 `IdempotencyStore` 端口与 Postgres 实现（24h TTL、同 key 同 hash 返回首结果、异 hash 返回 `already_exists`）。验证：并发/重复提交单测与 repo 集成测试通过。
- [x] 2.6 更新 `migrations.md` 相关说明或模块 README（迁移顺序、字段语义、回滚边界）。验证：文档评审可复核、与迁移文件一致。

## 3. Temporal Adapter 端口与实现

- [x] 3.1 扩展 `service.Adapter` 接口为 `Mode/Start/Cancel/Describe/Health`，更新 `NoopAdapter` 为显式 noop 语义并保留兼容行为。验证：service 单测覆盖 adapter 错误映射与 noop 模式标注。
- [x] 3.2 新增 `internal/modules/actions/temporal/`：配置解析（host/namespace/task queue）、client 构造、`Start` 提交 `ActionExecutionWorkflow`（workflow ID `action-execution-<execution_id>`、`REJECT_DUPLICATE`）、`Cancel`/`Describe`/`Health`。验证：fake Temporal client 单测覆盖启动失败、重复提交、取消与健康探测。
- [x] 3.3 启动期校验执行模式：`temporal` 缺 host 时启动失败，禁止静默回落 noop；`noop` 模式日志与响应标注降级。验证：配置单测/启动测试断言 fail-closed 与显式降级。
- [x] 3.4 在 `internal/platform/config` 增加 `actions.execution_mode`、`temporal.*`、审批/运行超时配置及默认值，补充示例 env 文档。验证：config 单测覆盖默认值、覆盖值与非法值。
- [x] 3.5 为 Temporal SDK 依赖准备补丁说明（用途、替代方案、license、版本）并提交依赖评审；go.mod/go.sum 变更交由 `stage-2-integration` 合并。验证：评审记录存在、`go build ./...` 在补丁分支通过。

## 4. Workflow 与 Activities

- [x] 4.1 实现 `ActionExecutionWorkflow`：按定义顺序调度步骤、加载快照参数、处理重试计数、审批等待、取消/超时、逆序补偿与终态写入，保证确定性（无时间/随机/外部 IO 直接调用）。验证：`testsuite.TestWorkflowEnvironment` 覆盖成功、重试、补偿、取消、超时。
- [x] 4.2 实现 activity：加载执行/定义、步骤效果应用（委托 `objects.Service.UpdateObject` + 步骤台账幂等）、补偿效果（快照恢复）、审批请求、终态落库。验证：activity 单测覆盖幂等重放、不可重试错误分类、租户校验。
- [x] 4.3 实现自定义重试策略（默认 3 次/1s/2.0/30s，定义可覆盖且受上限约束）与非重试错误包装。验证：workflow 测试断言可重试错误被重试、不可重试错误立即失败。
- [x] 4.4 实现补偿逆序、`before/after_snapshot` 记录、补偿失败进入 `compensation_failed` 并保留残留步骤。验证：workflow 测试覆盖逆序、补偿成功与补偿失败两条路径。
- [x] 4.5 实现审批等待（信号 `action.approval` + 定时器 + deadline）与批准/拒绝/超时分支。验证：workflow 测试用信号与时间跳跃覆盖三种决定及重复信号。
- [x] 4.6 实现 workflow 侧审计与指标发射（提交/终态/重试/审批/补偿），失败重试且不伪造成功。验证：单测断言事件与指标调用次数，审计失败路径有显式错误记录。

## 5. 调度接入、API 与 proto

- [x] 5.1 更新 `proto/ontoaxis/v1/actions/actions.proto`：扩展执行字段（object_id/attempt/current_step_index/workflow_refs/approval_state/failure_code/cancel_requested_at/steps）与审批 RPC/消息，字段编号追加且通过 `buf breaking`。验证：`make proto-lint`、`make proto-breaking` 通过。
- [x] 5.2 运行 `make generate` 生成 Go/前端产物并提交生成差异（`make generate-check` 一致）。验证：`make generate && make generate-check` 通过。
- [x] 5.3 改造 service `Run`：定义与参数校验 → 创建 `queued` 执行并写定义快照 → 幂等预写 → `adapter.Start`；提交失败投影 `failed`（`engine_unavailable`）且返回可重试错误。验证：service 单测覆盖成功、引擎失败、幂等重复提交。
- [x] 5.4 改造 `Cancel`/`Retry`：终态拒绝冲突、重试创建新执行并重新校验/授权、取消调用 adapter 并写 `cancel_requested_at`。验证：单测覆盖各状态组合与 adapter 调用。
- [x] 5.5 扩展执行查询返回状态、步骤进度、尝试次数、审批状态与 workflow 引用；审批任务查询按租户与状态过滤。验证：handler/service 测试断言字段与跨租户 404。
- [x] 5.6 新增审批 REST 端点（列表/详情/批准/拒绝）与 `Idempotency-Key` 强制校验、错误码映射（`invalid_definition`/`idempotency_key_required`/`engine_unavailable`/`permission_denied`/`not_found`/`conflict`）。验证：handler 测试覆盖成功、缺 key、重复决定、非法状态与错误信封字段。
- [x] 5.7 更新 `api/handler.go` 的预览响应：返回 `temporal`/`noop` 模式、审批要求、步骤摘要与预计超时，且不产生副作用。验证：handler 测试断言模式如实上报与执行记录数量不变。

## 6. 审批域与授权

- [x] 6.1 实现审批任务 domain/service：创建、列表、查询、CAS 决定、过期、拒绝理由与备注。验证：service 单测覆盖重复决定只生效一次、过期、已决定拒绝与跨租户 404。
- [x] 6.2 定义并接入 `Authorizer` 端口，映射 `action_type:run/read`、`action_execution:read/cancel/retry` 与审批决定 action；生产模式未注入时启动失败，单测使用显式测试实现。验证：单测覆盖允许、拒绝、授权不可用 fail-closed 与未配置启动失败。
- [x] 6.3 在 API 顺序中实现"认证→租户→输入校验→租户内加载→授权→审计→副作用"，跨租户先 404。验证：集成/单测覆盖 401/403/404/503 与无副作用断言。
- [x] 6.4 审批决定发出 Temporal 信号并在 workflow 侧幂等忽略重复信号。验证：service+workflow 测试覆盖信号发送失败、重复发送与信号丢失后的超时回退。
- [x] 6.5 记录审批与执行相关审计事件（`action.execution.submitted/cancelled/finished`、`action.step.retried`、`action.approval.requested/decided`、`action.compensation.finished`），API 侧失败阻断副作用，worker 侧重试与显式记录。验证：审计测试断言事件字段、脱敏与 fail-closed。

## 7. worker 二进制、可观测性与配置

- [x] 7.1 新增 `cmd/ontoaxis-actions-worker/main.go` 与 worker 包：注册 workflow/activities、连接 Temporal、暴露 `/healthz`、`/readyz`、`/metrics`（建议 18086/19091，最终端口由集成变更确认）。验证：本地启动 smoke（无 Temporal 时 readyz 失败、恢复后就绪），单测覆盖 readiness 逻辑。
- [x] 7.2 增加 `ontoaxis_action_*` 指标与 span（API→workflow→activity）、结构化日志字段与标签白名单校验。验证：指标单测断言标签白名单与计数；可观测 smoke 通过。
- [x] 7.3 在 compose 增加 Temporal profile 片段 `deploy/compose/action-temporal/compose.temporal.yaml`（auto-setup dev 镜像 + worker 服务）与说明 README；主 compose 文件不直接修改，交由 `production-delivery` 合并。验证：profile 片段 `docker compose config` 通过、README 记录启动命令与端口。
- [x] 7.4 提供测试故障注入 activity（`TestTransientFailure`/`TestPermanentFailure`）与 `ONTOAXIS_ACTIONS_TEST_HOOKS=1` 开关，生产默认关闭并记录告警日志。验证：开关默认关闭测试、开启后 E2E 可注入失败。

## 8. 审批 UI

- [x] 8.1 新增 `web/src/features/approvals/`：列表页（待审批/全部、Action Type、执行摘要、请求时间、deadline、脱敏参数摘要）与详情（步骤进度、审计摘要、批准/拒绝+备注）。验证：组件测试覆盖加载、空态、错误、无权限态。
- [x] 8.2 接入 REST 客户端与 `Idempotency-Key`，决定后以服务端返回状态刷新，不做乐观成功；补充 mock fixtures。验证：mock 模式组件/集成测试断言服务端状态来源与失败提示。
- [x] 8.3 在 `web/src/App.tsx` 注册受保护路由 `/approvals`，补充 en/zh i18n 文案。验证：`pnpm check` 通过、i18n key 完整性测试通过。
- [x] 8.4 增加 Playwright mock E2E：列表→批准→状态刷新、拒绝、无权限、空态。验证：`pnpm exec playwright test e2e/approvals.spec.ts` 通过（mock 仅 UI 证据）。

## 9. 真实实例 E2E 与证据

- [x] 9.1 编写 `scripts/actions-temporal-smoke.sh`：启动 Temporal profile、等待 worker readyz、创建测试 Action Type 与执行、断言重试次数、逆序补偿与对象恢复、审批通过/拒绝/超时、重试耗尽与 `compensation_failed` 路径，输出机器可读结果。验证：脚本在真实 Temporal+Postgres 上运行通过并返回 0。
- [x] 9.2 增加 env-gated Go 集成测试（`ONTOAXIS_ACTIONS_INTEGRATION=1`），未设置时 `t.Skip`，覆盖真实实例的重试与补偿断言。验证：设置环境变量时 `go test -run Integration ./internal/modules/actions/...` 通过；未设置时跳过且不失败。
- [x] 9.3 提供 CI job 补丁（label/nightly 触发、启动 Temporal profile、运行 smoke、上传日志），不直接修改 `ci.yml` 主文件，交由整合变更合并。验证：补丁可应用到当前 CI 配置、本地 `act`/模拟命令通过或评审确认。
- [x] 9.4 编写 `docs/stages/evidence/stage-2-action-temporal.md`：环境、命令、执行 ID、重试/补偿/审批/拒绝/耗尽结果与边界说明；未执行项标记 `planned`/`skipped`。验证：证据文件与 smoke 输出一致、评审可复核。
- [x] 9.5 验证显式 noop 降级路径：无 Temporal 环境下 `execution_mode=noop` 启动、预演与执行标注 `noop`、不出现真实引擎证据。验证：单测或本地命令输出记录在证据文件。

## 10. 文档与共享文件补丁

- [x] 10.1 更新 `internal/modules/actions/README.md`（若不存在则新增模块级说明）：执行定义、状态机、补偿、审批、幂等与配置。验证：文档评审、与代码/迁移一致。
- [x] 10.2 提供 `services-and-ports.md`、`runtime-topology.md` 的补丁（Temporal 与 worker 端口/组件），交由 `stage-2-integration`/`production-delivery` 合并。验证：补丁内容与 compose 片段一致。
- [x] 10.3 提供 ARCHITECTURE §5.1/§7.3 差异说明或更新建议（新状态/新表），按 1.3 的结论执行。验证：文档差异条目与本设计 D5/D6 一致。

## 11. 集成验证（本组只做整合检查，不补前面组的实现）

- [x] 11.1 运行 `make build`、`make test`、`make test-race`、`make vet`、`make lint`、`make license-check`，记录结果。验证：全部通过，结果写入证据文件。
- [x] 11.2 运行 `make proto-lint`、`make proto-breaking`、`make generate-check`，确认契约兼容。验证：全部通过，结果写入证据文件。
- [x] 11.3 运行 `cd web && pnpm install --frozen-lockfile && pnpm check && pnpm test && pnpm build` 与 Playwright mock E2E。验证：全部通过，结果写入证据文件。
- [x] 11.4 在真实 Temporal+Postgres 上重跑 `scripts/actions-temporal-smoke.sh`，核对审批、重试、补偿、拒绝与耗尽路径输出。验证：退出码 0，证据文件含执行 ID 与命令。
- [x] 11.5 运行 `openspec validate action-temporal --strict` 并检查任务勾选、traceability 状态（planned→verified）、证据索引一致。验证：validate 通过、traceability 无 `verified` 但无证据的条目。
- [x] 11.6 复核 diff 边界：未修改其他 change 目录、共享文件以补丁形式提交、无 `.env`/token/真实数据。验证：`git status`/diff 评审记录。

## Workflow follow-up

- 归档动作本身不在本 change 的 tasks 中，由 change 归档门禁在证据齐备后执行，不勾选 tracked task。
- 若授权变更的审批 action 名在本变更实现后才确定，审批映射调整作为 follow-up change 或授权变更补充，不在本 change 内静默扩展授权目录。
