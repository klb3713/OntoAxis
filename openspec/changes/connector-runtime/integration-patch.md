# Integration patch: connector-runtime

> 本文件只描述 `stage-2-integration` 需要合并的装配变更；本 change 不直接修改 `internal/app`、`cmd/`、`internal/platform/config`、`deploy/compose` 或 `go.mod`。

## 1. 配置

新增环境变量（均可在未配置时安全禁用）：

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `ONTOAXIS_CONNECT_URL` | 否（空=禁用） | Kafka Connect REST 根地址，例如 `http://kafka-connect:8083` |
| `ONTOAXIS_CONNECT_TIMEOUT` | 否 | 单次 REST 请求超时，默认 10s |
| `ONTOAXIS_CONNECT_RETRY_MAX` | 否 | 首次之外的重试次数，0–10，默认 5 |
| `ONTOAXIS_CONNECT_VERIFY_MAX_ROWS` | 否 | 行数验证上限，默认 1,000,000 |
| `ONTOAXIS_ICEBERG_CATALOG_URI` | Connect 启用时必填 | Iceberg JDBC catalog URI |
| `ONTOAXIS_ICEBERG_WAREHOUSE` | Connect 启用时必填 | warehouse 根路径 |
| `ONTOAXIS_ICEBERG_NAMESPACE` | 否 | 默认 `oa` |
| `ONTOAXIS_ICEBERG_CATALOG_USER` | 否 | 非敏感 JDBC 用户 |
| `ONTOAXIS_SECRET_<NORMALIZED_REF>` | StartSync 时必填 | JSON 对象 `{"username":"...","password":"...","iceberg_catalog_password":"..."}`，不得写入仓库 |

`ONTOAXIS_CONNECT_URL` 为空时 `runtime.NewFromEnv()` 返回 `(nil,nil)`，模块启动但 `StartSync` 在预检阶段返回不可用且不创建作业（D9）。

## 2. 模块装配

```go
connectRuntime, err := connectorsruntime.NewFromEnv()
if err != nil { return nil, err }
connectorsMod.SetSyncRuntime(connectRuntime) // nil 也要调用，保证 D9 fail-closed

jobRuntime := ingestionruntime.NewConnectorJobRuntime(connectRuntime) // nil control 首次调用即失败
ingestionRuntime, err := ingestionMod.NewRuntime(jobRuntime, verifier, ingestionruntime.Options{
    Logger:  logger,
    Metrics: ingestionRuntimeMetrics,
})
if err != nil { return nil, err }
if err := ingestionRuntime.Start(ctx); err != nil { return nil, err }
```

- `connectors` 模块：`SetSyncRuntime` 把 `runtime.Runtime` 适配为 `service.SyncPreflight`；预检在作业创建前执行，不通过时不产生 job。
- `ingestion` 模块：`NewRuntime` 使用 Postgres store、connectors reader、`ConnectorJobRuntime`、`SnapshotVerifier` 构造 dispatcher/reconciler；应用负责 `Start`/`Stop`。
- 取消传播：把同一个 `ConnectorJobRuntime` 传给 `ingestion.NewService(..., service.WithRuntimeStopper(jobRuntime))`；否则 running 作业取消失败而不是假成功。
- `SnapshotVerifier` 由 integration 提供：读取作业对应 Dataset 的存储位置/表名并查询 Iceberg（Trino），结合派发时记录在 `sync_runs.stats.expected_rows` 的源表行数；两次稳定确认后才允许 succeeded。未注入 verifier 时作业只保持 running，不会假成功。

## 3. 关闭顺序

1. `ingestionRuntime.Stop()` 并等待 dispatcher/reconciler 退出；
2. 关闭 HTTP server；
3. 关闭数据库池。

不得在运行中的 dispatcher 仍持有数据库连接时先关闭池。

## 4. 门禁与限制

- 本 patch 只提供 API/片段；`go.mod` 的 `go mod tidy` 由 integration 执行。
- `deploy/connector-runtime/` 由本 change 提供，`deploy/compose` 的 profile 合并与生产栈由 `production-delivery` 负责。
- 未配置 Connect 或缺少 SnapshotVerifier 时，`StartSync`/reconcile 的 fail-closed 语义必须保留，不得用假成功或默认放行兜底。
