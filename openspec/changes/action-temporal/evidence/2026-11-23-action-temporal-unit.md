# Evidence: action-temporal (unit slice)

> 状态：实现中证据，非 Stage 2 E2E 验收。真实 Temporal + Postgres E2E 未运行。
> 日期：2026-11-23
> 范围：`action-temporal` change 的 domain/持久化/Temporal workflow/worker/审批 API 与最小审批 UI。

## 环境

| 项 | 值 |
| --- | --- |
| 主机 | macOS 26 (darwin/amd64) |
| Go | 1.26.3，`/tmp/go/bin/go` |
| 模块缓存 | `GOMODCACHE=/tmp/gomod` |
| Docker | 27.0.3（本证据未启动 Temporal/Postgres 容器） |
| Temporal SDK | `go.temporal.io/sdk v1.51.0`，`go.temporal.io/api v1.64.3` |
| 执行模式 | 单测显式使用 noop 或 `TestWorkflowEnvironment`；未连接真实 Temporal |

## 命令与结果

```bash
# 1. 依赖合并（本 change 提交 go.mod/go.sum 补丁；最终 tidy 由 stage-2-integration 复核）
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go mod tidy
# 结果：exit 0

# 2. 构建
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...
# 结果：exit 0

# 3. 单元测试（全仓库）
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./...
# 结果：全部包 ok；无 FAIL

# 4. actions 竞态检测
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -race ./internal/modules/actions/...
# 结果：actions/actions/api/domain/service/temporal/worker 全部 ok；repo 无测试文件

# 5. vet 与格式
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go vet ./...
# 结果：exit 0
/tmp/go/bin/gofmt -l internal/modules/actions cmd/ontoaxis-actions-worker
# 结果：无输出

# 6. 前端
cd web && pnpm check && pnpm test && pnpm build
# 结果：check 通过；33 tests passed（7 个 approval 测试）；build 成功
```

## 覆盖情况

| 能力 | 覆盖 |
| --- | --- |
| 执行定义校验（steps/retry/timeout/compensation/read_only） | domain 单测：合法、重复 ID、未知 activity、缺补偿、重试/超时越界、50 步上限 |
| 状态机与终态不可变 | 表驱动单测覆盖 D6 转换与非法转换 |
| 迁移/加密列/步骤台账/审批/幂等表 | `20261122200000_action_temporal.sql`；repo 集成测试在本地 PG16 上通过（up/down、CAS、跨租户、幂等） |
| Adapter 端口/noop/引擎失败 | service 单测：启动失败投影 `failed`+`engine_unavailable`、cancel、approval signal 失败 |
| Workflow 成功/重试/补偿/审批/取消 | `testsuite.TestWorkflowEnvironment` 9 个测试，含逆序补偿与 `compensation_failed` 残留 |
| Worker health/ready/metrics | worker HTTP 单测 |
| 审批 REST + Idempotency-Key | handler 单测：列表、允许、缺 key 400、非法 status 400 |
| 审批 UI | Vitest 7 测试：列表、筛选、批准刷新、403/409/503、空态、i18n |

## 未覆盖 / 限制（不得写成已通过）

- 未启动真实 Temporal/Postgres；无 `ONTOAXIS_ACTIONS_INTEGRATION=1` E2E，无 smoke 脚本证据。
- `temporal.NoopEffectApplier` 仍是 worker 的生产装配占位；`object.update` 等真实副作用尚未接到 `objects.Service.UpdateObject`。
- 审批决定授权暂复用 `action_execution:cancel`（授权目录无 `action_execution:approve`）；待 `authorization-openfga-cedar` 增补 action 后切换。
- 未修改 proto；执行字段通过 REST 投影暴露，审批 RPC 以 REST 实现。`make proto-*`/`generate-check` 未运行。
- `actions.execution_mode` 配置在 `internal/modules/actions/temporal`，未并入 `internal/platform/config`（共享文件由 stage-2-integration 合并）。
- `RetryExecution` 仅允许 `failed`/`cancelled`；spec delta 对 `timed_out`/`compensation_failed` 的表述待统一。
- 取消由 service 直接写 `cancelled`；若后续补偿失败，行仍为 `cancelled`，未升级为 `compensation_failed`。
- 无 compose profile、Helm/ArgoCD、CI job、`docs/stages/evidence/stage-2-action-temporal.md`（由 stage-2-integration 汇总）。
- `make license-check` 未运行。

## 变更文件索引

- `internal/modules/actions/domain/definition.go`、`domain/action.go`（执行字段/状态/步骤/审批/幂等类型）
- `internal/modules/actions/migrations/20261122200000_action_temporal.{sql,down.sql}`
- `internal/modules/actions/repo/runtime.go`、`repo/store.go`
- `internal/modules/actions/service/adapter.go`、`service/service.go`
- `internal/modules/actions/temporal/{config,ports,activities,workflow,adapter}.go`
- `internal/modules/actions/worker/{worker,metrics}.go`
- `cmd/ontoaxis-actions-worker/main.go`
- `internal/modules/actions/api/approvals.go`、`api/approvals_test.go`、`api/handler.go`（执行字段）
- `internal/modules/actions/module.go`（Temporal adapter/readiness/close 与多迁移读取）
- `web/src/features/approvals/`、`web/src/App.tsx`、`web/src/i18n/{en,zh}.ts`、`web/src/api/{types,fixtures,mock-data-provider}.ts`、`web/src/components/AppLayout.tsx`

## 变更边界（2026-11-23）

- 本 change 只修改 `internal/modules/actions/`、`cmd/ontoaxis-actions-worker/`、`web/src/features/approvals/` 与相关前端装配，以及 `go.mod/go.sum`、本 change 目录。
- 未修改 `internal/app/`、`cmd/ontoaxis-core/`、`deploy/compose/` 主文件、`proto/`、其他 change 目录；共享装配以 `integration-patch.md` 交付 `stage-2-integration` / `production-delivery`。
- 工作区中其他模块的未提交改动来自此前 `authorization-openfga-cedar` / `connector-runtime` / `pipeline-kestra-dbt`，不是本 change 扩大范围。
- `openspec validate action-temporal --strict`：valid；`openspec validate --all --strict`：20/20 passed。
- `git status` 复核：本 change 未写入 `.env`、token 或真实数据。
