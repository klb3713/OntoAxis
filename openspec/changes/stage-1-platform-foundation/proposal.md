# Proposal: Stage 1 Platform Foundation

## Why

Stage 1 已通过评审，但当前仓库只有架构、治理文档与已冻结的 `proto/ontoaxis/v1` 契约文件（21 个 proto 文件、1,272 行，已通过 `buf format`/`buf lint`/`buf build`），没有可编译的 Go module、可启动的进程骨架、数据库连接与迁移机制，也没有 CI 和本地 Compose 基线。若直接让多个模块 Agent 并行开发，各自会重复搭建基础设施并争抢共享入口，无法安全地并行推进后续 11 个 change。

本 change 先建立一次性的平台基座，冻结模块注册方式、配置、数据库、健康检查、错误模型与契约生成入口，为 Wave 1 的七个并行模块提供稳定依赖。

## What Changes

- 初始化 `github.com/klb3713/ontoaxis` Go module（Go 1.25），建立 `cmd/ontoaxis-core`、`internal/app`、`internal/platform`、`internal/modules` 目录骨架。
- 实现统一配置加载与校验、结构化日志、请求关联 ID、统一错误响应模型 `{"error":{"code","message","metadata","retryable","request_id"}}`。
- 建立 PostgreSQL `pgxpool` 连接、`goose` 迁移执行器与平台 `jobs` schema 基线；模块迁移按 `ARCHITECTURE.md` 3.4.4 顺序执行，业务表迁移归属各模块。
- 实现 `ontoaxis-core` HTTP 基座：`/healthz`、`/readyz`、`/metrics`、未注册路由 404、优雅关停。
- 建立 `platform.Module` 注册接口（`Name()`、`Dependencies()`、`Migrate()`、`RegisterRoutes()`、`Ready()`）与模块生命周期；`internal/app` 按 `ARCHITECTURE.md` 3.4.4 的固定顺序装配，依赖未就绪时启动失败。
- 接入已冻结的 `proto/ontoaxis/v1`（21 个 proto 文件、1,272 行）与 `proto/buf.yaml`/`proto/buf.gen.yaml`，建立 `make generate`、`make generate-check`、`make proto-lint`、`make proto-breaking` 目标，生成物输出到 `gen/`、`openapi/`、`web/src/generated`；不修改 proto 源文件。
- 增加 Dockerfile、`deploy/compose` Compose 骨架、Makefile 目标与 GitHub Actions CI（build、test、lint、buf、前端 check/build、镜像构建）。
- 增加平台级 smoke test：启动单进程，验证健康端点、404、优雅关停与迁移执行。

**Out of scope**

- 不实现 `tenancy`、`connectors`、`ingestion`、`datasets`、`pipelines`、`query`、`ontology`、`objects`、`actions` 九个业务模块。
- 不接入 Keycloak、Trino、Kestra、Temporal、Kafka、Meilisearch、SeaweedFS 等外部真实实例。
- 不实现 `oa-edge` 与 `oa-web` 页面。
- 不启动真实环境服务，不接触真实客户数据。

## Capabilities

- **New Capabilities**: 无。本 change 是 Stage 支撑/工程基座 change，按 `skip_specs: true` 处理，不引入用户可见业务能力。
- **Modified Capabilities**: 无。

## Impact

- 新增 `go.mod`、更新 `go.sum`、`cmd/ontoaxis-core/main.go`、`internal/app/*`、`internal/platform/*`、`internal/modules/` 目录骨架、`gen/`、`openapi/`、`web/src/generated`（后三者由 `make generate` 生成）、`SPEC.md`、`Makefile`、`Dockerfile`、`deploy/compose/compose.yaml`、`.github/workflows/ci.yml`。
- `proto/ontoaxis/v1` 与 `proto/buf.yaml`/`proto/buf.gen.yaml` 已存在；本 change 只接入生成链，不修改契约文件。
- 后续 Wave 1/2 模块将依赖本 change 冻结的模块注册接口、配置键、错误模型、健康检查和迁移约定。
- 尚无生产数据、外部服务或用户接口，因此无破坏性变更与迁移风险。
