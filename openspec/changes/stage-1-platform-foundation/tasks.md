# Tasks

## 1. Go module 与目录骨架

- [ ] 1.1 初始化 `go.mod`（module `github.com/klb3713/ontoaxis`，Go 1.25），创建 `cmd/ontoaxis-core`、`internal/app`、`internal/platform`、`internal/modules`、`web/src/generated` 目录；运行 `go list ./...`，确认 module 路径正确且无 import 错误。
- [ ] 1.2 增加 `.gitignore`、`.editorconfig`、`.env.example`、`README` 的本地启动占位说明；运行 `git status --short`，确认忽略 `.env`、`bin/`、`dist/`、`node_modules/`。

## 2. 配置、日志与错误模型

- [ ] 2.1 实现 `internal/platform/config`（koanf + `ONTOAXIS_*` 环境变量），定义 `HTTP_ADDR`、`METRICS_ADDR`、`DATABASE_URL`、`LOG_LEVEL`、`SHUTDOWN_TIMEOUT`；编写表驱动测试覆盖默认值、环境变量覆盖、缺失必填项返回错误。
- [ ] 2.2 实现 `internal/platform/observability`：slog JSON 日志、request_id 注入、日志字段脱敏；编写测试验证请求日志包含 `request_id` 且不输出密码/token 字段。
- [ ] 2.3 实现 `internal/platform/httperr` 统一错误模型 `{"error":{"code","message","metadata","retryable","request_id"}}` 与稳定错误码；编写 handler 单测验证 4xx/5xx 响应形状，且不泄漏 SQL/内部错误。
- [ ] 2.4 记录配置与错误码到 `docs/`（平台运行手册草稿）；运行文档中给出的启动命令，确认命令可执行。

## 3. 数据库与迁移

- [ ] 3.1 实现 `internal/platform/db`：pgxpool 连接、连接超时、最大连接数与健康 ping；编写测试用 testcontainers 或本地 Postgres 验证连接成功、错误 DSN 返回明确错误。
- [ ] 3.2 实现 goose 迁移执行器与平台 `jobs` schema 基线（`jobs.outbox`、`jobs.operations`）；迁移文件放在平台 `eventing` 能力拥有的独立迁移目录，不建立业务共享 `migrations/` 目录；运行迁移，确认重复执行幂等且回滚命令可用。
- [ ] 3.3 实现模块迁移发现与执行规则（按 `ARCHITECTURE.md` 3.4.4 固定顺序、平台迁移先行、每个模块只写自己的 schema）；编写测试验证两个 fake 模块按确定顺序执行，重复注册返回错误。
- [ ] 3.4 在 `docs/architecture/` 或运行手册记录迁移约定（只追加、不修改已发布迁移、回滚/回填要求、模块迁移顺序）；执行文档中的迁移命令验证示例。

## 4. HTTP 基座与模块注册

- [ ] 4.1 实现 `cmd/ontoaxis-core/main.go`、`internal/app` 与 `internal/platform/httpserver`：chi 路由、请求 ID、panic recovery、优雅关停、404 JSON；编写 `main_test` 或 httptest 验证 `/healthz` 形状、`/readyz` 在 DB 未就绪时 503、未知路由 404。
- [ ] 4.2 实现 `internal/platform/module` 接口与注册表：`Name() string`、`Dependencies() []string`、`Migrate(ctx, *pgxpool.Pool) error`、`RegisterRoutes(chi.Router)`、`Ready(ctx) error`；编写测试验证重复模块名报错、缺失依赖报错、注册顺序稳定。
- [ ] 4.3 实现 `/metrics` 独立监听器与基础 Prometheus 指标（进程、HTTP 请求计数/延迟）；启动后 curl `/metrics`，确认包含 `ontoaxis_http_requests_total` 与 `ontoaxis_http_request_duration_seconds`。
- [ ] 4.4 建立模块目录模板 `internal/modules/_template`（仅接口实现示例）；运行 `go test ./internal/modules/_template/...`，确认模板可编译且被文档引用。

## 5. API 契约与生成

- [ ] 5.1 校验既有 `proto/ontoaxis/v1`（21 个 proto 文件、1,272 行）与 `proto/buf.yaml`/`proto/buf.gen.yaml`；运行 `buf format --diff --exit-code`、`buf lint`、`buf build`，确认全部通过且不修改 proto 源文件。
- [ ] 5.2 增加根 `Makefile` 的 `generate`/`generate-check`/`proto-lint`/`proto-breaking` 目标，按 `proto/buf.gen.yaml` 输出 Go 代码到 `gen/`、OpenAPI 到 `openapi/`、TypeScript 到 `web/src/generated`；运行 `make generate` 与 `make generate-check`，确认生成文件存在且无漂移。
- [ ] 5.3 编写 `SPEC.md` 初版：模块边界、模块注册接口（含 `Dependencies()`）、健康/错误契约、配置键、迁移与 CI 约定；运行 Markdown 检查与 `grep` 校验每个章节存在。

## 6. Docker、Compose 与 CI

- [ ] 6.1 编写多阶段 `Dockerfile`（Go 1.25 构建、非 root 运行、`/healthz` healthcheck）；运行 `docker build` 验证镜像可构建且容器内 `/healthz` 返回 200。
- [ ] 6.2 编写 `deploy/compose/compose.yaml`：`ontoaxis-core` + PostgreSQL，带 healthcheck、初始化目录和命名 volume；运行 `docker compose -f deploy/compose/compose.yaml config` 与 `docker compose -f deploy/compose/compose.yaml up -d`，确认两个服务 healthy 且 `/healthz` 可达。
- [ ] 6.3 编写 `.github/workflows/ci.yml`：`go build ./...`、`go test -race ./...`、`golangci-lint`、`buf lint`、`buf breaking`、`make generate-check`、前端 `pnpm check/build`（`web/package.json` 不存在时显式跳过）、许可证扫描与 Docker build；用 `actionlint` 或 YAML 解析验证语法。

## 7. 集成验证与文档

- [ ] 7.1 编写 `scripts/smoke-foundation.sh`：启动 Compose、等待 healthy、curl `/healthz`、`/readyz`、`/metrics`、请求未知路由断言 404；运行脚本，输出全部断言通过。
- [ ] 7.2 更新 `README.md` 与 `docs/getting-started/local-development.md` 的基座启动、测试、停止、清理命令；按文档逐条执行，确认无缺失步骤。
- [ ] 7.3 运行最终质量门：`go build ./...`、`go test -race ./...`、`golangci-lint run`、`buf lint`、`buf breaking`、`make generate-check`、`docker compose -f deploy/compose/compose.yaml config`；将命令与结果记录到 change 的验证报告。

## Workflow follow-up

- 在实现与验证全部完成后，提交 change 评审并请求项目负责人确认。
- 评审通过后运行 `openspec validate --all --strict` 与 `openspec status --change stage-1-platform-foundation`，再执行归档；归档前不得宣称 Stage 1 完成。
