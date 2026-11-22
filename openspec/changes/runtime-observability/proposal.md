# Proposal: Runtime Observability

## Why

Stage 1 已具备 `/healthz`、`/readyz`、`/metrics`、结构化日志和 request id，但这些能力只够本地排查：没有 OTel trace、没有统一的指标命名与标签约束、没有 SLO/告警/仪表盘，也没有可复用的遥测测试 harness。Stage 2 的 `oa-edge`、OpenFGA/Cedar、Temporal、Kestra、Kafka Connect 和 Meilisearch 都会引入新的运行时与失败模式；如果先各自接入日志/指标，会造成命名漂移、基数失控和无法横向比较的证据。

本 change 先冻结 Stage 2 的 telemetry contract，为 Wave 0 后续 change 和所有 Wave 1/2 运行时提供同一套 trace、metrics、SLO、告警与测试基线。

## What Changes

- 定义并实现 OTel resource attributes、W3C trace context 传播、trace sampler 与 OTLP HTTP exporter 配置。
- 定义 `ontoaxis_*` Prometheus 指标目录、标签白名单、route template 归一化、直方图 bucket 与基数约束。
- 为 HTTP server、模块 service、PostgreSQL、出站 HTTP/Trino、Kafka/后台任务建立 span 与指标接入点；默认不记录 SQL 文本、请求体、token 或个人数据。
- 提供本地 `observability` Compose profile：OTel Collector、Prometheus、Alertmanager、Jaeger、Perses；默认 `make test` 不依赖该 profile。
- 定义 Stage 2 临时 SLO/SLI、告警规则、dashboard 与错误预算 burn-rate 基线。
- 增加遥测测试 harness：指标采集断言、trace 传播测试、Prometheus rule 单测、可复现 smoke 脚本。
- 写入 `docs/architecture/observability-baseline.md` 并同步 README/local-development 的运行说明。

**Out of scope**

- 不实现 Stage 2 的业务 change（`oa-edge`、授权、Temporal、Kestra、Kafka Connect、Meilisearch）。
- 不接生产 APM、商业 SaaS 或真实云监控；本地/预发验证使用 OSS 组件。
- 不把 tenant_id、user_id、object_id 等高基数字段写入 metrics labels。
- 不做容量规划、全量日志平台或多区域 tracing 后端。
- 不将临时 SLO 阈值当作业务验收结论；阈值需在 Stage 2 退出评审前确认。

## Capabilities

- **New Capabilities**: `observability` — 运行时可观测性契约，覆盖 telemetry context、指标、trace、SLO、告警与验证 harness。
- **Modified Capabilities**: 无。

## Impact

- 新增 `internal/platform/observability` 的 OTel 与 metrics 组件、`internal/platform/httpserver` 的 RED 指标/span 中间件、`internal/platform/db` 与出站客户端的 span 接入。
- 新增 `deploy/observability/`（Compose profile、Collector/Prometheus/Alertmanager/Jaeger/Perses 配置、rules、dashboards）与 `scripts/observability-smoke.sh`。
- 新增 `docs/architecture/observability-baseline.md`；更新 `docs/getting-started/local-development.md` 与 README 的启动/验证说明。
- 依赖新增 OTel Go SDK 与 exporter；不改变现有公开 HTTP API 或数据库 schema。
- 后续 Wave 1/2 change 直接复用本 change 的命名、标签、span 与证据约定。
