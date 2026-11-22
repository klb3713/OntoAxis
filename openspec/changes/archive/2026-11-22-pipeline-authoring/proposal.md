# Proposal: pipeline-authoring

## Why

Stage 1 的 S1-AC-09 要求 pipelines 模块提供 Pipeline 定义、版本、运行入口与 Kestra 契约适配。当前仓库只有平台基座与并行建设中的 datasets 模块，`pipeline` schema、Pipeline 服务、运行入口、Schedule 与 Kestra adapter 边界都尚不存在；若不先建立最小垂直切片，query、ontology 与后续集成 change 将缺少可注册、可测试的 pipelines 依赖。

本 change 在 `internal/modules/pipelines/` 内交付一个不接真实 Kestra、不依赖真实 Postgres 的可独立测试模块，为 Wave 3 的模块注册与真实执行适配提供稳定边界。

## What Changes

- 新增 `pipelines` capability，覆盖 Pipeline 定义、发布版本、运行入口、运行查询与 Schedule 管理。
- 新增 `internal/modules/pipelines/` 模块：`domain/`、`service/`、`repo/`、`api/`、`kestra/`、`migrations/` 与 `module.go`，实现 `platform.Module`，`Dependencies()` 返回 `datasets`。
- 新增 `pipeline` schema 幂等 DDL：`pipeline.pipelines`、`pipeline.versions`、`pipeline.runs`、`pipeline.schedules`，包含租户 RLS、FORCE ROW LEVEL SECURITY、唯一键与索引；迁移通过 `db.ExecStatements` 执行。
- 新增 `/api/v1/pipelines` JSON 路由族：Pipeline CRUD、发布、触发运行、运行查询，以及 `/api/v1/pipelines/schedules` 的 Schedule CRUD/Pause/Resume。
- 新增 `kestra.Adapter` 接口与 `kestra.MemoryAdapter` 内存骨架：发布时登记 Flow、触发运行时调用 adapter；不接真实 Kestra 实例、不生成真实 Flow YAML。
- 新增 `service.DatasetResolver` 消费端窄接口：DAG 引用 dataset 时校验其存在；未注入 resolver 时 fail-closed，不静默放行。
- 测试使用 `repo.MemoryStore` 与 `httptest`，覆盖成功、无效输入、租户隔离与关键拒绝路径；不依赖真实 Postgres、Kestra 或身份服务。

**Out of Scope**

- 不实现 DAG 引擎、cron 调度器或执行器；Build/Job 执行由 Kestra 承担（ARCHITECTURE 7.2）。
- 不接真实 Kestra 实例、不调用 Kestra API、不生成真实 Flow YAML、不实现 Kestra Flow 双向同步。
- 不实现真实认证中间件与 identity-edge；tenant 解析器按接口注入，默认拒绝所有请求。
- 不修改 `cmd/`、`internal/app/`、`internal/platform/`、`go.mod`、`go.sum`、`deploy/` 或其他模块目录，不把模块注册进主程序，不 commit。

## Capabilities

### New Capabilities

- `pipelines`：Pipeline 定义、发布版本、运行入口、运行查询、Schedule 与 Kestra adapter 契约。

### Modified Capabilities

- 无。

## Impact

- 新增代码：`internal/modules/pipelines/**`（domain、service、repo、api、kestra、migrations、module.go 与测试）。
- 新增规格：`openspec/changes/pipeline-authoring/specs/pipelines/spec.md`。
- 新增 schema：`pipeline.pipelines`、`pipeline.versions`、`pipeline.runs`、`pipeline.schedules`，全部租户表启用 RLS。
- 依赖：模块声明依赖 `datasets`；复用 `internal/platform/module`、`internal/platform/db`、`internal/platform/httperr`、`internal/platform/httpserver`，不新增第三方依赖。
- 下游：`stage-1-core-integration` 可将本模块注册到 `internal/app` 并提供真实认证与 datasets 适配；真实 Kestra 适配与真实 Postgres 迁移验证属于后续集成/环境门。
