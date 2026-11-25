# Design

## Context

Stage 1 的 `internal/platform/observability` 只有 JSON slog 与 request id；`internal/platform/httpserver` 在自有 Prometheus registry 上注册两个 HTTP 指标，`/metrics` 独立监听；`internal/platform/db` 与模块尚未统一接入 trace。Stage 2 将引入 `oa-edge`、OpenFGA/Cedar、Temporal、Kestra、Kafka Connect、Meilisearch，跨进程链路和失败面会快速增加。

本 change 的完整行为契约见 [`specs/observability/spec.md`](specs/observability/spec.md)；可落地的基线细节见 [`docs/architecture/observability-baseline.md`](../../../../docs/architecture/observability-baseline.md)。

## Goals / Non-Goals

**Goals:**

- 冻结 OTel resource、propagation、sampler 与 OTLP 配置约定，使 `ontoaxis-core`、`oa-edge`、worker 使用同一 trace 语义。
- 冻结 `ontoaxis_*` 指标目录、label 白名单、route template 和 histogram bucket，防止基数失控。
- 提供本地可复现的 Collector/Prometheus/Alertmanager/Jaeger/Perses profile。
- 给出 Stage 2 临时 SLO、burn-rate 告警、dashboard 与规则测试基线。
- 建立可归档的验证 harness：metrics、trace propagation、rule unit test、smoke。

**Non-Goals:**

- 不替换业务模块的日志语义；日志继续使用 `log/slog` JSON 输出。
- 不引入商业 APM、云监控、日志搜索平台或多区域 trace 后端。
- 不在 metrics 中暴露 tenant/user/object 等高基数字段；trace 中也不记录业务载荷。
- 不实现 Stage 2 的 `oa-edge`、授权、Temporal、Kestra、Kafka Connect、Meilisearch 业务 change。
- 不把临时 SLO 阈值作为生产 SLA 或业务验收结论。

## Decisions

### D1：OTel SDK + OTLP HTTP，而不是各组件私有 tracing

选择 `go.opentelemetry.io/otel` + `otlptracehttp`，统一导出到本地 OTel Collector；应用不直接依赖 Jaeger/Tempo。原因是 Stage 2 会新增边缘、worker 与多个外部系统，Collector 作为唯一出口可以在不重启业务进程的情况下切换后端或采样策略。

替代方案：直接使用 Jaeger client 或 vendor SDK。放弃，因为绑定后端、跨语言不一致且迁移成本高。

### D2：metrics 保持 Prometheus pull，不把 OTel metrics 作为主路径

HTTP/DB/业务指标继续通过 `prometheus/client_golang` 暴露 `/metrics`，由 Prometheus 拉取；OTel 只负责 trace，避免 Stage 2 同时迁移两套 metrics SDK。指标注册集中在 `internal/platform/observability/metrics`，禁止各模块自建 registry。

替代方案：OTel metrics + Prometheus exporter。放弃，因为当前指标简单，双栈会增加迁移面且收益不足。

### D3：指标前缀、label 与 route template 作为硬契约

- 指标前缀固定 `ontoaxis_`；单位后缀使用 `_seconds`/`_bytes`/`_total`。
- label 白名单：`method`、`route`、`status`、`module`、`operation`、`driver`、`result`、`environment`。
- `route` 必须是 chi route pattern 或 `unmatched`；禁止原始 UUID/query。
- 直方图固定 bucket：`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10` 秒。
- 每个指标最多 8 个 label；新增 label 必须更新 `observability-baseline.md` 与 review。

替代方案：允许业务自行定义 label。放弃，Stage 2 的 Trino/Kafka/Temporal 会迅速产生不可控基数。

### D4：trace 采样可配置，本地默认 100%，非本地默认 10%

配置项：

- `OTEL_EXPORTER_OTLP_ENDPOINT`（空则 no-op）
- `OTEL_EXPORTER_OTLP_PROTOCOL`（默认 `http/protobuf`）
- `OTEL_TRACES_SAMPLER`（默认 `parentbased_traceidratio`）
- `OTEL_TRACES_SAMPLER_ARG`（本地 `compose` 设 `1.0`，其他环境默认 `0.1`）

原因：本地 E2E 需要完整 trace；非本地 10% 足够排障且成本可控。错误 span 的完整采集由 Stage 2 的 Collector tail-sampling 配置补强，不在应用内做自定义采样器。

替代方案：全部 100% 或全部关闭。放弃，前者成本不可控，后者无法验证链路。

### D5：本地观测栈使用 opt-in Compose profile

新增 `deploy/observability/`，包含：

- `otel/opentelemetry-collector-contrib`
- `prom/prometheus`
- `prom/alertmanager`
- `jaegertracing/all-in-one`
- `perses/perses`

通过 `deploy/compose/compose.yaml` 的 `observability` profile 接入，不改变默认 `up` 的依赖。端口规划：Collector OTLP HTTP `4318`、Prometheus `19091`、Alertmanager `19093`、Jaeger UI `16686`、Perses `18084`。

替代方案：把 observability 组件加入默认栈。放弃，增加本地启动时间和内存占用，且大多数开发任务不需要。

### D6：SLO 先作为 Stage 2 临时基线，不冒充生产 SLA

Stage 2 暂定：

| SLI | 目标 | 窗口 |
| --- | --- | --- |
| HTTP availability（非 4xx 成功 / 全部） | 99.9% | 30 天 |
| read API p95 | < 200ms | 5 分钟/30 天 |
| write API p95 | < 400ms | 5 分钟/30 天 |
| readiness 成功率 | 99.95% | 30 天 |

阈值写入 recording/alert rules，并在文档中标记“Stage 2 暂定，Stage 3 重新签核”。原因是当前没有生产流量和容量数据，不能把拍脑袋阈值包装成正式 SLO。

替代方案：Stage 2 不定义 SLO。放弃，因为无法验证告警和 dashboard，也无法为后续生产准备提供基线。

### D7：验证 harness 优先于 dashboard 截图

新增：

- metrics 单元测试：`prometheus/client_golang/prometheus/testutil` 断言指标名、label、counter/histogram。
- trace 传播测试：内存 exporter 断言 parent/child 和 `traceparent`；不依赖 Jaeger 运行。
- rule 测试：`promtool test rules deploy/observability/prometheus/rules_test.yaml`。
- smoke：`scripts/observability-smoke.sh` 启动 profile、打请求、查询 Prometheus/Jaeger 并输出可归档结果。

原因：dashboard 是展示层，不能作为“遥测正确”的唯一证据；测试 harness 才能进入 CI。

### D8：敏感字段与基数边界

- metrics 不接收 `tenant_id`、`user_id`、`object_id`、`request_id`、URL query。
- trace attributes 只允许低基数/已批准字段；禁止 SQL 文本、请求体、响应体、token、cookie。
- 日志继续由 request id 关联；不把 trace ID 当身份凭据。
- 新增 span attribute 必须通过 review，并在 baseline 文档中登记。

## Risks / Trade-offs

- [基数失控] → label 白名单 + route template + CI 检查新增指标；review 拒绝未登记 label。
- [Collector 成为单点] → Collector 仅本地/预发；应用在 exporter 失败时降级为 no-op，不阻塞请求。
- [采样过低漏掉错误] → Stage 2 使用 Collector tail-sampling 保留 error/latency 异常；应用侧只做基础 sampler。
- [SLO 阈值无依据] → 全部标记暂定；Stage 2 退出前由负责人确认，Stage 3 用真实基线重签。
- [测试依赖外部栈] → 单元测试不依赖 Docker；只有 smoke 依赖 observability profile，且 CI job 可单独运行。
- [Prometheus/Jaeger 版本漂移] → compose 中固定镜像 tag；升级需独立 PR 和基线文档更新。

## Migration Plan

1. 先合入本 change 的 telemetry contract、SDK bootstrap 与本地 profile，不改业务 API。
2. 在 `ontoaxis-core` 接入 HTTP/DB/模块 span 与 RED 指标；保留现有 `/metrics` 兼容量。
3. 在 `deploy/compose` 中启用 opt-in profile，跑 smoke 并归档 metrics/trace 证据。
4. 后续 `edge-gateway`、`authorization` 等 change 直接复用约定；`production-delivery` 再把同一配置映射到 Helm。
5. 回滚策略：关闭 `OTEL_EXPORTER_OTLP_ENDPOINT`、移除 `observability` profile 即回到 Stage 1 行为；不涉及数据库迁移。

## Open Questions

无。采样率、SLO 暂定值、本地组件与端口已在本设计冻结；生产后端和正式 SLO 属于 Stage 3。
