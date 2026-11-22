# Design: Stage 1 Platform Foundation

## Context

Stage 1 的第一步不是业务模块，而是所有 Wave 1/2/3 change 共同依赖的运行基座：一个可编译、可启动、可迁移、可观测、可自动验证的 Go 模块化单体骨架。当前仓库已有 `ARCHITECTURE.md` v1.0 与 `proto/ontoaxis/v1` 的 21 个 proto 文件（1,272 行，已通过 `buf format`/`buf lint`/`buf build`），但没有 Go module、`go.mod`、`internal/app`、Compose、CI 或任何可执行入口，因此本 change 必须先冻结这些共享接口，避免后续七个并行 Agent 各自造一套。

## Goals / Non-Goals

**Goals:**

- 建立单 Go module、单二进制 `ontoaxis-core` 的最小可运行骨架。
- 冻结平台层边界：配置、日志、错误模型、HTTP 生命周期、数据库连接与迁移、模块注册接口。
- 接入 `proto/ontoaxis/v1` 契约与代码生成入口，后续模块只消费冻结契约。
- 建立本地 Compose、Dockerfile、Makefile 与 GitHub Actions 基线，后续 change 直接复用。
- 让 `/healthz`、`/readyz`、`/metrics`、404 和优雅关停成为可自动验证的平台行为。

**Non-Goals:**

- 不实现业务模块、业务表或业务 API。
- 不接 Keycloak、Trino、Kestra、Temporal、Kafka、Meilisearch、SeaweedFS 等真实外部服务。
- 不实现 `oa-edge` 路由和 `oa-web` 页面。
- 不建立生产部署、HA 或真实数据验收。

## Decisions

### D1. 单 Go module，模块化目录而不是多 module

- **Decision**: 使用 `github.com/klb3713/ontoaxis` 单 module；目录为 `cmd/ontoaxis-core`、`internal/app/*`、`internal/platform/*`、`internal/modules/<module>/*`。
- **Rationale**: 单 module 让所有 Agent 共享 `go.mod`/`go.sum` 与统一版本，避免多 module 的 replace、版本漂移和 CI 复杂度；模块边界靠目录与 `internal` 可见性约束，而不是物理拆仓。
- **Alternatives considered**:
  - 每个业务模块一个 module：并行编译更独立，但共享类型和依赖管理成本高，Stage 1 不需要。
  - 单目录大包：实现最简单，但无法形成可并行、可审查的模块边界。

### D2. 配置采用 koanf + 环境变量，启动时强校验

- **Decision**: 使用 `koanf` 读取可选的 YAML/JSON 配置与 `ONTOAXIS_*` 环境变量；优先级为显式环境变量 > 配置文件 > 默认值。启动时验证必填项，失败立即 `exit 1`。
- **Rationale**: 与现有架构约定一致；本地开发可用 `.env`/Compose，生产可完全用环境变量；避免把敏感值写入仓库。
- **Alternatives considered**:
  - 纯手写 `os.Getenv`：依赖少，但配置项一多就难以验证和文档化。
  - 仅 YAML：本地简单，但容器环境注入口径不统一。

### D3. HTTP 采用 chi + 独立 `/metrics` 监听

- **Decision**: 业务与健康检查共用一个 `chi.Router`，`/metrics` 由独立监听器提供；`/healthz` 返回 `{status, service, version, timestamp}`，`/readyz` 检查 PostgreSQL 与模块 readiness；未注册路由返回 404。
- **Rationale**: 与 `ARCHITECTURE.md` 16.5 一致；独立 metrics 端口避免业务端口暴露指标；chi 足够轻量且与模块路由组合简单。
- **Alternatives considered**:
  - Gin/Echo：功能更全，但团队既有约定与依赖更重。
  - 标准库 `net/http`：依赖最少，但路由分组与模块注册不够清晰。

### D4. 数据库使用 pgxpool + goose，模块各自迁移

- **Decision**: `internal/platform/db` 提供 `pgxpool.Pool` 与 `goose` 迁移执行器；平台只拥有 `jobs` schema（outbox/operations）基线，业务 schema 全部由对应模块的 `internal/modules/<module>/migrations` 拥有，并按 `ARCHITECTURE.md` 3.4.4 顺序执行。
- **Rationale**: 单 PostgreSQL 实例、每模块独立 schema 是架构固定决策；goose 可审计、可回滚，避免 ORM 自动迁移；不设全局业务 `migrations/` 目录。
- **Alternatives considered**:
  - sqlc：后续用于查询生成，但迁移仍需 goose；本 change 只建立迁移框架。
  - Atlas：能力更强，但新依赖与学习成本对 Stage 1 过高。

### D5. 模块注册接口与共享文件所有权

- **Decision**: 定义 `platform.Module` 接口：`Name() string`、`Dependencies() []string`、`Migrate(ctx context.Context, pool *pgxpool.Pool) error`、`RegisterRoutes(r chi.Router)`、`Ready(ctx context.Context) error`。`internal/app` 按 `ARCHITECTURE.md` 3.4.4 的固定顺序构造模块；`cmd/ontoaxis-core` 只负责加载配置、创建 pool 与调用 `internal/app`，不直接引用业务 handler。
- **Rationale**: Wave 1/2 的 Agent 只改自己的模块目录并实现接口，不修改 `main.go`、`internal/app` 和总路由；`stage-1-platform-foundation` 是唯一共享文件 owner。
- **Alternatives considered**:
  - 让每个模块直接改 `main.go`：简单但多个 Agent 必然冲突。
  - 运行时插件/动态加载：超出 Stage 1 范围，且 Go 生态复杂。

### D6. 契约与生成接入既有 `proto/ontoaxis/v1`

- **Decision**: 使用仓库已冻结的 `proto/ontoaxis/v1`（21 个 proto 文件、1,272 行）与 `proto/buf.yaml`/`proto/buf.gen.yaml`，在 `Makefile` 中建立 `generate`、`generate-check`、`proto-lint`、`proto-breaking` 目标；生成 Go/Connect 到 `gen/`、OpenAPI 到 `openapi/`、TypeScript 到 `web/src/generated`；不修改 proto 源文件，也不新建 `proto/ontoaxis/core/v1`。
- **Rationale**: 契约已在进入 Stage 1 前定稿并通过 `buf format`/`buf lint`/`buf build`；后续模块必须共享同一契约生成链，避免各模块自行定义或手写客户端。
- **Alternatives considered**:
  - 等到第一个业务模块再建 proto 生成：会把契约选择拖到 Wave 1，造成返工。
  - 手写 OpenAPI：与架构的 proto-first 约定冲突。

### D7. 本地运行与 CI 基线

- **Decision**: 使用 `deploy/compose/compose.yaml` 启动 `ontoaxis-core` + PostgreSQL；GitHub Actions 执行 `go build ./...`、`go test -race ./...`、`golangci-lint`、`buf lint`、`buf breaking`、前端 `pnpm check/build`（若 `web/package.json` 尚未建立则跳过并显式报告）、镜像构建。
- **Rationale**: 在当前空仓库先把可执行质量门搭起来，后续 11 个 change 不必重复搭建；CI 对所有 Agent 输出统一约束。
- **Alternatives considered**:
  - GitLab CI：当前协作与代码托管按 GitHub 约定，暂不采用。
  - 只做本地 Makefile：无法阻止并行 Agent 引入不可构建的代码。

## Risks / Trade-offs

- [多个 Agent 同时依赖平台骨架] → 本 change 先合入 `main`，后续 Agent 从同一 tag 建 worktree；共享文件只由 foundation owner 修改。
- [配置项与模块约定被后续 change 擅自扩展] → 配置键、模块接口、错误模型写入本 change 的 `SPEC.md`/design，变更需走 change 评审。
- [Compose 依赖外部服务导致本地启动慢] → Stage 1 基座只启动 core + PostgreSQL；后续模块按需增加 profile。
- [迁移顺序与 schema 冲突] → 平台仅有 `jobs` schema 基线，模块迁移按 `ARCHITECTURE.md` 3.4.4 顺序执行；每个模块只允许写自己的 schema。
- [GitHub Actions 在空前端目录上误报失败] → 前端任务先检查 `web/package.json` 是否存在，不存在则跳过并输出明确说明，等 `frontend-core` change 补齐。
- [生成物与冻结契约漂移] → `make generate-check` 执行 `git diff --exit-code`，CI 同时跑 `buf lint`/`buf breaking`。

## Migration Plan

1. 在 `main` 创建 `go.mod`、目录骨架、配置模板、`deploy/compose`、CI。
2. 执行 `make test` / `go build ./...` 验证基座。
3. 合入 `main` 并打 tag `stage-1-foundation`，后续模块从该 tag 建 worktree。
4. 回滚：删除该 tag 对应提交即可恢复空仓库；本 change 不修改任何生产数据或历史迁移。

## Open Questions

- 无阻塞性开放问题。`web/` 页面目录由 `frontend-core` 创建；本 change 只创建 `web/src/generated` 作为生成物输出目录。
