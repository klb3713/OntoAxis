# 6.1 集成测试与 6.4 可观测性证据（2026-11-23）

## 环境

- 本地 compose：`deploy/compose/compose.yaml` + `authz/compose.authz.yaml` + `connector-runtime/compose.connector-runtime.yaml`，`--profile connector-runtime --profile observability`。
- Kafka Connect：Debezium 3.6.1 / worker Kafka 4.3.0；Iceberg 1.12.0；Trino 484（只用于目录对照）。
- 观测栈：otel-collector 0.120.0、Jaeger 1.62.0、Prometheus v3.1.0、Alertmanager 0.27.0、Perses v0.50.0。
- 合成数据：`source-postgres` 的 `public.customers`（5 行）。
- 说明：`ontoaxis-core` 镜像构建仍受 macOS keychain 限制，本轮用交叉编译二进制 `docker cp` 进容器验证；正式门禁仍应以镜像构建结果为准。

## 6.1 集成测试

命令：

```bash
ONTOAXIS_CONNECT_INTEGRATION=1 \
ONTOAXIS_CONNECT_IT_URL=http://127.0.0.1:18086 \
go test -tags=integration ./internal/modules/connectors/... ./internal/modules/ingestion/...
```

结果：全部包 `ok`，其中 `internal/modules/connectors/runtime` 耗时 68.7s（含 3 个真实实例用例）。

新增 `internal/modules/connectors/runtime/runtime_integration_test.go`（`//go:build integration`，未设 `ONTOAXIS_CONNECT_INTEGRATION=1` 时 skip）：

| 用例 | 断言 | 结果 |
| --- | --- | --- |
| `TestIntegrationSubmitStatusStop` | 真实提交后 source/sink 均 RUNNING；重复提交返回同一 runtime ref（幂等）；Stop 后资源被移除 | PASS（约 5s） |
| `TestIntegrationBadCredentialsDoNotLeak` | 错误密码时提交或状态失败，且错误文本不含密码原文 | PASS（约 31s） |
| `TestIntegrationConnectUnavailable` | Connect 不可达时返回 `ErrConnectUnavailable`，错误不含凭据 | PASS（约 30s） |

限制与说明：

- 测试会清理本租户前缀（`oa_<tenanthash8>_`）下的**非活跃**复制槽；源端 `max_replication_slots=10`，反复运行不清理会耗尽导致 source task 失败（本轮实测过一次）。
- `internal/modules/ingestion` 目前没有 `-tags=integration` 用例，命令在该包执行的是常规测试；派发/对账的真实链路由 `scripts/connector-runtime-e2e.sh` 覆盖（作业终态、取消、负向矩阵）。

## 6.4 可观测性证据

### 接线缺口与修复

`connectorsruntime.NewMetrics` 之前没有接入进程注册表，`/metrics` 中完全没有 `ontoaxis_connector_*` 序列。修复：

- `AssemblyFactory` / `BuildRuntimeAssembly` 增加 `*observability.Metrics` 参数，core 启动时把应用注册表传下来；
- `connectorsruntime.NewFromEnv` 增加 `WithMetrics` 与 `WithTracer`，指标注册到同一 registry。

### Prometheus 抓取样例

查询（Prometheus 19091）：

```
ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation="upsert",result="ok"} 4
ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation="status",result="ok"} 102
ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation="stop",result="ok"} 2
ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation="delete",result="ok"} 2
ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation="ping",result="ok"} 3
ontoaxis_connector_submit_seconds_count{operation="submit",result="ok"} 2
ontoaxis_connector_submit_seconds_count{operation="preflight",result="error"} 1
```

原始采样：`evidence/2026-11-23-connector-metrics.txt`。

### Trace 样例

Jaeger（16686）按 `service=ontoaxis-core` 取一条包含提交链路的 trace，span 属性只含 `dependency/operation/result/span.kind`，无 tenant、连接器名或凭据：

```
kafka connect upsert        {dependency: kafka_connect, operation: upsert, result: ok, span.kind: client}
kafka connect upsert        {dependency: kafka_connect, operation: upsert, result: ok, span.kind: client}
connectors.runtime.submit   {dependency: kafka_connect, operation: submit, result: ok, span.kind: internal}
```

原始 JSON：`evidence/2026-11-23-jaeger-trace-sample.json`。

### Dashboard / 告警片段

`evidence/2026-11-23-telemetry-snippet.yaml` 提供面板查询（请求速率、错误率、提交 p95）与两条告警规则（错误率 >10%、提交 p95 >30s），**未直接修改 `deploy/observability/`**，按任务要求移交 `production-delivery` 合并。

## 未完成

- 告警规则尚未在本地 firing 验证（只验证了指标存在与查询可用）。
- `verified_rows` 仍为 sink 侧 total-records；源/汇行数等式与 Trino 读取仍挂在 D-03。
