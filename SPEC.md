# SPEC — OntoAxis Core 平台基座（Stage 1 / Wave 0）

> 状态：Stage 1 platform foundation 的实现约定。目标设计与完整架构见 [`ARCHITECTURE.md`](../ARCHITECTURE.md)。

## 1. 模块边界

`ontoaxis-core` 是单一 Go module `github.com/klb3713/ontoaxis` 的单二进制。业务模块固定为 9 个：

`tenancy`、`connectors`、`ingestion`、`datasets`、`pipelines`、`query`、`ontology`、`objects`、`actions`。

`identity`、`authorization`、`health`、`operation` 是 platform 能力，不是业务模块。

每个模块位于 `internal/modules/<module>/`，必须实现 `platform/module.Module`：

```go
type Module interface {
    Name() string
    Dependencies() []string
    Migrate(ctx context.Context, pool *pgxpool.Pool) error
    RegisterRoutes(r chi.Router)
    Ready(ctx context.Context) error
}
```

模块注册顺序由 `Dependencies()` 拓扑排序后决定；固定业务模块顺序为：

`tenancy → connectors → ingestion → datasets → pipelines → ontology → objects → query → actions`。

## 2. 配置

配置从 `ONTOAXIS_*` 环境变量和可选 `ONTOAXIS_CONFIG` YAML 文件读取，环境变量优先。

| 配置项 | 默认值 | 必填 | 说明 |
|---|---|---|---|
| `ONTOAXIS_HTTP_ADDR` | `:8080` | 否 | 业务/健康端口 |
| `ONTOAXIS_METRICS_ADDR` | `:9090` | 否 | Prometheus 指标端口 |
| `ONTOAXIS_DATABASE_URL` | — | 是 | PostgreSQL DSN |
| `ONTOAXIS_LOG_LEVEL` | `info` | 否 | `debug/info/warn/error` |
| `ONTOAXIS_SHUTDOWN_TIMEOUT` | `10s` | 否 | 优雅关停超时 |
| `ONTOAXIS_TRUST_GATEWAY` | `false` | 否 | 生产由网关注入内部 header；本地默认直接校验 |

## 3. 健康、错误与 API

- `GET /healthz`：`{"status":"ok","service":"ontoaxis-core","version":"...","timestamp":"..."}`。
- `GET /readyz`：检查 PostgreSQL 与每个模块的 `Ready()`；任一失败返回 503。
- `GET /metrics`：独立监听器，暴露 `ontoaxis_http_requests_total`、`ontoaxis_http_request_duration_seconds`。
- 未注册路由返回 JSON 404：
  `{"error":{"code":"not_found","message":"route not found","retryable":false,"request_id":"..."}}`。
- 5xx 不暴露内部 SQL/堆栈；所有错误使用 `internal/platform/httperr`。

## 4. 数据库与迁移

- PostgreSQL 单实例；每个模块独立 schema。
- 平台拥有 `jobs` schema（`jobs.outbox`、`jobs.operations`）以及 `internal/platform/db/migrations`。
- 模块迁移放在 `internal/modules/<module>/migrations/`，只允许写自己的 schema；使用 goose 命名 `YYYYMMDDHHMMSS_<module>_<verb>_<noun>.sql`。
- 迁移只追加、不修改已发布迁移；回滚脚本与前进脚本成对保存。
- 应用启动时先执行平台迁移，再按拓扑顺序执行每个模块的 `Migrate()`。

## 5. 生成与验证

- `proto/ontoaxis/v1` 是唯一契约源；生成链见 `proto/buf.gen.yaml`。
- `make proto-lint`、`make proto-breaking`、`make generate`、`make generate-check` 是 CI 门禁。
- `make test` / `go test -race ./...` 必须通过。
- 本地 Compose：

```sh
docker compose -f deploy/compose/compose.yaml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:9090/metrics | head
docker compose -f deploy/compose/compose.yaml down -v
```

## 6. 非目标

- 本基座不实现业务模块、不接 Keycloak/Trino/Kestra/Temporal/Kafka/Meilisearch/SeaweedFS 真实实例。
- 不提供 `oa-edge` 路由和 `oa-web` 页面；这些由后续 change 建立。
- 不用真实客户数据；所有本地验证使用合成 fixture。
