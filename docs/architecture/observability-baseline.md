# 可观测性基线（Stage 2 runtime-observability）

> 状态：Stage 2 Wave 0 设计基线，随 `runtime-observability` change 定稿。<br>
> 适用版本：`ontoaxis-core` v1.x、Stage 2 范围内的 `oa-edge`/worker。<br>
> 关联：[`ARCHITECTURE.md`](../../ARCHITECTURE.md) 第 12 章、[`stage-2-ontoaxis-runtime-brief.md`](../stages/stage-2-ontoaxis-runtime-brief.md) S2-AC-09。

## 1. 目标与边界

本基线定义 Stage 2 的 telemetry contract：日志、指标、trace、SLO、告警、dashboard 与验证 harness。目标是让新增运行时（edge、Temporal worker、Kestra、Kafka Connect、Meilisearch）复用同一套命名、标签和证据格式，而不是各自造一套。

**不做**：商业 APM、云监控、日志检索平台、生产 SLA 签核、多区域 trace 后端、把业务载荷写入 telemetry。

## 2. 信号分工

| 信号 | 实现 | 用途 |
| --- | --- | --- |
| Logs | `log/slog` JSON → stdout | 审计/排障，保留 request id；不替代 metrics/trace |
| Metrics | `prometheus/client_golang` → `/metrics` | RED、SLO、告警、容量与冒烟断言 |
| Traces | OTel SDK → OTLP HTTP → Collector | 跨进程调用链与失败定位 |
| Dashboards | Perses | 展示层，不作为唯一证据 |
| Alerts | Prometheus rule + Alertmanager | 可执行告警，规则进入 CI 单测 |

## 3. Resource 与传播约定

### 3.1 Resource attributes

每个进程必须设置：

| 属性 | 示例 | 说明 |
| --- | --- | --- |
| `service.name` | `ontoaxis-core` / `oa-edge` | 固定服务名 |
| `service.namespace` | `ontoaxis` | 固定命名空间 |
| `service.version` | git describe / build version | 发布版本 |
| `deployment.environment` | `local` / `kind` / `staging` | 部署环境 |
| `service.instance.id` | hostname + pid 或 pod name | 实例标识 |

禁止把 tenant、user、token、数据库 DSN 写入 resource。

### 3.2 传播

- 入站：读取 W3C `traceparent` / `tracestate`，缺失时创建根 span。
- 出站：HTTP/Trino/Kafka 客户端必须注入 `traceparent`。
- 日志：`trace_id` / `span_id` 仅在日志字段中关联，不替代 request id。
- 禁止：把 `Authorization`、Cookie、请求体、响应体、SQL 文本写入 span attributes 或日志。

## 4. 指标目录

### 4.1 命名规则

- 前缀固定 `ontoaxis_`。
- 单位：秒 `_seconds`，字节 `_bytes`，计数 `_total`。
- 直方图必须带 `_bucket` / `_sum` / `_count`。
- 每个指标最多 8 个 label。

### 4.2 Label 白名单

`method`、`route`、`status`、`module`、`operation`、`driver`、`result`、`environment`。

**禁止**：`tenant_id`、`user_id`、`object_id`、`request_id`、`trace_id`、原始 URL、URL query、错误消息原文。

### 4.3 核心指标

| 指标 | 类型 | Labels | 说明 |
| --- | --- | --- | --- |
| `ontoaxis_build_info` | gauge | `version`,`commit` | 构建信息 |
| `ontoaxis_http_requests_total` | counter | `method`,`route`,`status` | HTTP 请求总数 |
| `ontoaxis_http_request_duration_seconds` | histogram | `method`,`route` | HTTP 时延 |
| `ontoaxis_http_requests_in_flight` | gauge | — | 在途请求数 |
| `ontoaxis_module_ready` | gauge | `module` | 模块 readiness，1/0 |
| `ontoaxis_db_pool_connections` | gauge | `state` | pgxpool total/idle/acquired |
| `ontoaxis_db_query_duration_seconds` | histogram | `operation` | 数据库操作时延 |
| `ontoaxis_outbound_request_duration_seconds` | histogram | `dependency`,`operation`,`result` | 出站依赖 |
| `ontoaxis_jobs_total` | counter | `type`,`result` | 后台任务 |
| `ontoaxis_search_index_lag_seconds` | gauge | `index` | Stage 2 后段接入 |

### 4.4 直方图 bucket

固定：

```text
0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
```

### 4.5 Route template

`route` 必须是 chi route pattern：

```text
/api/v1/objects/{id}
/api/v1/ontology/object-types
unmatched
```

禁止把 `/api/v1/objects/01a202...` 或带 query 的 URL 写入 label。

## 5. Trace 约定

### 5.1 Span 层级

```text
HTTP server span
├── authn/middleware span
├── module service span
│   ├── PostgreSQL query span
│   └── outbound HTTP/Trino span
└── worker/messaging span
```

### 5.2 必备 span

| 场景 | span name | 关键 attributes |
| --- | --- | --- |
| 入站 HTTP | `HTTP <method> <route>` | `http.request.method`、`url.path`（template）、`http.response.status_code` |
| PostgreSQL | `postgres <operation>` | `db.system=postgresql`、`db.namespace`、`db.operation` |
| Trino | `trino query` | `db.system=trino`、`db.operation`；不记录 SQL |
| Kafka | `kafka <operation>` | `messaging.system=kafka`、`messaging.destination.name` |
| 模块 service | `<module>.<operation>` | `ontoaxis.module`、`operation` |
| 后台任务 | `job <type>` | `job.type`、`job.result` |

### 5.3 采样

| 环境 | Sampler | 参数 |
| --- | --- | --- |
| local / compose | `parentbased_traceidratio` | `1.0` |
| kind / staging | `parentbased_traceidratio` | `0.1` |
| 测试 | parent-based always-on | `1.0` |

错误/latency 异常由 Collector tail-sampling 补充；应用不写自定义 sampler。

### 5.4 Exporter

```text
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_SERVICE_NAME=ontoaxis-core
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=local
```

未设置 endpoint 时使用 no-op tracer，不阻塞启动。

## 6. SLO / SLI（Stage 2 暂定）

> 以下为技术基线，不是生产 SLA；Stage 3 用真实流量重新签核。

| SLI | 定义 | 目标 | 窗口 |
| --- | --- | --- | --- |
| HTTP availability | 非 4xx 响应 / 全部响应 | 99.9% | 30 天 |
| read p95 | GET/HEAD 的 p95 | < 200ms | 5m/30d |
| write p95 | POST/PUT/PATCH/DELETE 的 p95 | < 400ms | 5m/30d |
| readiness | `/readyz` 2xx / 全部 | 99.95% | 30 天 |

Recording rules：

```text
ontoaxis:slo_http_availability:ratio_30d
ontoaxis:slo_http_availability:ratio_5m
ontoaxis:slo_read_latency:p95_5m
ontoaxis:slo_write_latency:p95_5m
ontoaxis:slo_readyz:ratio_30d
```

错误预算：30 天窗口 0.1% 不可用预算。burn-rate 告警：

- 1h 窗口 14.4× burn（2% 预算）→ page
- 6h 窗口 6× burn（5% 预算）→ ticket

## 7. 告警基线

| 告警 | 条件 | 级别 |
| --- | --- | --- |
| `OntoAxisCoreDown` | `up{job="ontoaxis-core"} == 0` 2m | page |
| `OntoAxisHighErrorRate` | 5xx ratio > 1% 5m | page |
| `OntoAxisLatencyP95High` | read p95 > 500ms 10m | ticket |
| `OntoAxisReadinessFailing` | readyz 失败 2m | page |
| `OntoAxisDBPoolSaturated` | acquired/total > 0.9 5m | ticket |
| `OntoAxisCollectorExportFailing` | exporter failure > 0 5m | ticket |

规则文件：`deploy/observability/prometheus/rules.yaml`；单测：`deploy/observability/prometheus/rules_test.yaml`。

## 8. Dashboard 基线

Perses dashboard 至少包含：

1. **Service overview**：请求 rate、5xx ratio、p95/p99、in-flight、build info。
2. **Readiness**：模块 readiness、DB ping、实例 up。
3. **Database**：pool total/idle/acquired、query p95、慢查询计数。
4. **Dependencies**：Trino/Kafka/Meilisearch 出站耗时与错误。
5. **SLO**：availability、read/write p95、error budget 剩余。

Dashboard 文件：`deploy/observability/perses/dashboards/`。

## 9. 本地观测栈

`deploy/compose/compose.yaml` 提供 `observability` profile：

| 组件 | 端口 | 用途 |
| --- | --- | --- |
| `otel-collector` | 4317/4318 | OTLP gRPC/HTTP |
| `prometheus` | 19091 | 指标存储与 rule 评估 |
| `alertmanager` | 19093 | 告警路由（本地仅日志） |
| `jaeger` | 16686 | trace 查询 UI |
| `perses` | 18084 | dashboard UI |

启动：

```bash
docker compose -f deploy/compose/compose.yaml --profile observability up -d
```

默认 `up` 不启动这些组件；CI 的 `observability-smoke` job 单独启用。

## 10. 验证 harness

| 验证 | 命令/位置 | 证据 |
| --- | --- | --- |
| 指标契约 | `go test ./internal/platform/observability/... ./internal/platform/httpserver/...` | 测试日志 |
| trace 传播 | `go test ./internal/platform/observability/...` | 内存 exporter 断言 |
| 告警规则 | `make observability-rules-test` | `promtool test rules` 输出 |
| 本地 smoke | `scripts/observability-smoke.sh` | metrics/trace 查询输出 |
| CI | `.github/workflows/ci.yml` 的 `observability` job | GitHub Actions run |

## 11. 新增指标/span 的审查清单

新增前必须回答：

- 是否已有指标/span 可以复用？
- label 是否在白名单内？基数上界是多少？
- 是否含 tenant/user/object/request 等动态值？
- 是否含 SQL、请求体、token 或错误原文？
- SLO/告警是否需要同步更新？
- 是否在 `observability-baseline.md` 登记？
- 是否有单元测试或 smoke 证据？

未通过审查的 telemetry 不得合入。

## 12. 实现状态（2026-11-22）

- telemetry：`internal/platform/observability/telemetry.go`，支持空 endpoint no-op 与 OTLP/HTTP 导出。
- HTTP RED/trace：`internal/platform/httpserver/server.go` + `observability/tracing.go`，route label 使用 chi template。
- DB trace：`internal/platform/db/tracing.go`，只记录 operation，不记录 SQL。
- Trino client trace：`internal/modules/query/trino/client.go` 注入 `traceparent`。
- 本地栈：`deploy/compose/compose.yaml` 的 `observability` profile；Prometheus rules 12 条；Perses dashboard JSON。
- 验证：`go test ./internal/platform/...`、`make observability-rules-test`、`scripts/observability-smoke.sh`。
- 已知边界：Perses dashboard 通过 API 导入，不包含自动 provisioning；Kafka/job 侧仅提供 `StartJobSpan` 接缝，消费者接入留给后续 change。
- Perses 本地 dashboard 的 Prometheus datasource 使用 `http://localhost:19091`（宿主机浏览器直连）；远程部署必须在 `deploy/observability/perses/globaldatasources/prometheus.json` 中替换为环境内地址。
