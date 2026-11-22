# 可观测性运行手册

本手册对应 `runtime-observability` change，覆盖告警响应、采样调整和本地验证。
指标/SLO 契约以 `docs/architecture/observability-baseline.md` 为准。

## 1. 入口

| 组件 | 地址/命令 |
| --- | --- |
| core 健康 | `curl -fsS http://127.0.0.1:18080/healthz` |
| core 就绪 | `curl -fsS http://127.0.0.1:18080/readyz` |
| core 指标 | `curl -fsS http://127.0.0.1:19090/metrics` |
| Prometheus | <http://127.0.0.1:19091> |
| Alertmanager | <http://127.0.0.1:19093> |
| Jaeger | <http://127.0.0.1:16686> |
| Perses | <http://127.0.0.1:18084> |

本地观测栈：

```sh
export ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
docker compose -f deploy/compose/compose.yaml --profile observability up -d --build
```

## 2. 告警响应

### OntoAxisCoreDown

1. 检查容器/进程：`docker compose -f deploy/compose/compose.yaml ps ontoaxis-core`。
2. 查看日志：`docker compose -f deploy/compose/compose.yaml logs --tail=200 ontoaxis-core`。
3. 检查 `/healthz`、`/readyz`、`/metrics`。
4. 若数据库不可用，先恢复 PostgreSQL；迁移只向前，禁止手工删除迁移历史。
5. 恢复后确认 `up{job="ontoaxis-core"} == 1` 且告警恢复。

### OntoAxisHighErrorRate

1. 在 Prometheus 查询 `ontoaxis_http_requests_total{status=~"5.."}` 按 route 分组。
2. 在 Jaeger 按 service `ontoaxis-core` 查该时间段的错误 span。
3. 检查对应日志中的 `request_id`/`trace_id`，不要记录 token 或请求体。
4. 回滚：使用 `deploy/README.md` 的回滚步骤，先备份数据库再切镜像。

### OntoAxisLatencyP95High

1. 看 route label 定位慢路径。
2. 查 `ontoaxis_db_query_duration_seconds` 与 Trino client span。
3. DB pool 饱和时见下一节；Trino 慢查询需检查 Trino 队列/节点。
4. 不得通过放宽 SLO 阈值消除告警。

### OntoAxisReadinessFailing

1. `GET /readyz` 查看失败的 module。
2. 检查对应模块日志/依赖；数据库 pool 状态可查 `ontoaxis_db_pool_connections`。
3. 模块未就绪时不要绕过 readiness 放流量。

### OntoAxisDBPoolSaturated

1. 查 `ontoaxis_db_pool_connections{state="acquired"}` 与 `state="total"`。
2. 检查长事务/慢查询；不要直接把 pool size 调到超过 Postgres 连接上限。
3. 必要时滚动重启 core；确认迁移锁未长期占用。

### OntoAxisCollectorExportFailing

1. 检查 `docker compose ... logs otel-collector`。
2. 检查 `ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT` 与 collector 4318 端口。
3. 恢复前 tracing 可能丢失；不要伪造 trace 证据。

## 3. 采样与保留

- 本地 `ONTOAXIS_OTEL_SAMPLE_RATIO=1.0`；非本地默认 `0.1`。
- 采样率变更需重启 core；变更后确认 `telemetry tracing enabled` 日志。
- Prometheus 本地 profile 保留 24h；生产保留策略由部署环境配置。
- 不记录 SQL 文本、object id、token、请求体。

## 4. 规则与 smoke

```sh
make observability-rules-test
scripts/observability-smoke.sh
```

smoke 会启动 profile、导入 Perses dashboard、请求 core，并断言
Prometheus 有 `ontoaxis_http_requests_total` 且 Jaeger 有 `ontoaxis-core` trace。

## 5. 变更审查

新增指标/label/span 前，按 `docs/architecture/observability-baseline.md` 第 11 节检查基数、敏感字段与 SLO 影响。
