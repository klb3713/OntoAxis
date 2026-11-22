# Tasks

> 实现、本地验证与负责人评审已完成；7.2 待提交后以 GitHub Actions 远端 run 勾选。

## 1. Telemetry 基线与配置

- [x] 1.1 在 `internal/platform/observability` 增加 telemetry config（service name/version/env、OTLP endpoint、sampler、export 开关），并验证未设置 endpoint 时启动与请求不失败（`go test ./internal/platform/observability/...`）。
- [x] 1.2 增加 OTel resource 与 W3C TraceContext propagator 初始化，验证 `traceparent` 入站沿用、无 header 时创建根 span（内存 exporter 单测）。
- [x] 1.3 更新 `.env.example`、Compose 与 `docs/getting-started/local-development.md` 的 OTel 配置说明，并验证文档中的启动命令可执行。

## 2. HTTP RED 指标

- [x] 2.1 在 `internal/platform/observability/metrics` 实现统一 registry、`ontoaxis_http_requests_total`、`ontoaxis_http_request_duration_seconds`、`ontoaxis_http_requests_in_flight`，验证指标名、类型、label 与固定 histogram bucket（testutil）。
- [x] 2.2 在 `internal/platform/httpserver` 用 chi route pattern 生成 `route` label，未匹配路由固定为 `unmatched`，验证直方图与 counter 不含原始 UUID/query（单测）。
- [x] 2.3 增加 `ontoaxis_build_info` 与 `ontoaxis_module_ready`，验证 `/metrics` 输出包含对应系列且 readiness 变化时 gauge 更新。

## 3. Trace 接入

- [x] 3.1 给 HTTP server 与中间件增加 server span，验证 span name 使用 route template、`http.response.status_code` 正确、401/403/404 不丢失。
- [x] 3.2 给 `internal/platform/db` 增加 PostgreSQL query span，验证同一请求的 server span 与 DB span parent/child 正确，且不包含 SQL 文本（内存 exporter 测试）。
- [x] 3.3 为出站 HTTP/Trino 增加 client span 与 `traceparent` 注入，验证下游 stub 收到同一 trace ID。
- [x] 3.4 为后台任务/Kafka 消费增加 span 接缝，验证至少一个 `job <type>` span 可被导出（本地 no-op 与内存 exporter 两种模式）。

## 4. 本地观测栈

- [x] 4.1 新增 `deploy/observability/` 的 OTel Collector 配置（OTLP receiver、batch、Prometheus exporter、Jaeger exporter、tail-sampling 占位），验证 `otelcol validate` 或 `docker run` 配置校验通过。
- [x] 4.2 在 `deploy/compose/compose.yaml` 增加 `observability` profile（Collector、Prometheus、Alertmanager、Jaeger、Perses）及端口 4317/4318/19091/19093/16686/18084，验证默认 `up` 不启动、`--profile observability` 可启动。
- [x] 4.3 增加 Prometheus scrape config 与 Jaeger/Perses 初始配置，验证打开 profile 后能查询 `ontoaxis_http_requests_total` 与一条 trace。

## 5. SLO 与告警

- [x] 5.1 增加 availability/read p95/write p95/readiness 的 recording rules，验证 `promtool check rules` 通过。
- [x] 5.2 增加 `OntoAxisCoreDown`、`OntoAxisHighErrorRate`、`OntoAxisLatencyP95High`、`OntoAxisReadinessFailing`、`OntoAxisDBPoolSaturated`、`OntoAxisCollectorExportFailing` 告警规则，验证 `promtool check rules` 与 `promtool test rules` 通过。
- [x] 5.3 增加 `deploy/observability/prometheus/rules_test.yaml` 的触发/不触发用例（含无流量不误报），验证 `make observability-rules-test` EXIT=0。

## 6. Dashboard 与运行手册

- [x] 6.1 增加 Perses service overview/readiness/database/dependencies/SLO dashboard 定义，验证 Perses 能导入并渲染（本地 profile 截图/查询结果）。
- [x] 6.2 新增 `docs/operations/observability-runbook.md`，覆盖高错误率、延迟、实例 down、DB pool、Collector 失败与采样调整，验证文档中的命令可执行。
- [x] 6.3 更新本 change 的 `traceability.md`，把 S2-AC-09 的 requirement/scenario 映射到计划证据。

## 7. 验证 harness

- [x] 7.1 增加 `scripts/observability-smoke.sh`：启动 profile、打一个请求、查询 Prometheus/Jaeger、输出指标与 trace 结果；失败时非零退出。
- [ ] 7.2 在 CI 增加 `observability` job（或扩展 build-test job）运行规则测试与 smoke，验证 GitHub Actions 通过。
- [x] 7.3 运行完整回归：`go build ./...`、`go test ./...`、`go test -race ./...`、`golangci-lint`、`make generate-check`、`make proto-breaking`，记录日志到 `docs/stages/evidence/stage-2-runtime-observability/`。

## 8. 文档与评审

- [x] 8.1 更新 `docs/architecture/observability-baseline.md` 的实现状态与验证命令，验证 `git diff --check` 通过。
- [x] 8.2 更新 README 与 `docs/getting-started/local-development.md` 的 observability profile 说明，验证相对链接可达。
- [x] 8.3 生成 change 评审材料（proposal/spec/design/tasks/traceability + 验证日志摘要），提交负责人评审。

## Workflow follow-up

- 评审通过后按 tasks 依赖顺序实施；不得跳过未通过的验证项。
- 所有任务有证据后再运行 `openspec validate --all --strict`，准备归档。
- 归档后更新 `openspec/specs/observability/spec.md`，并同步 Stage 2 报告。
