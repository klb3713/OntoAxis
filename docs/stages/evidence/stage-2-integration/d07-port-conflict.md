# D-07 端口冲突修复与同栈配置校验（2026-11-25）

Fast Track（行为不变：仅默认端口与文档登记）。归属：`deploy/connector-runtime/`（connector-runtime change 已归档）、`deploy/compose/`（production-delivery 边界），两处证据均已登记。

## 1. 原问题

| 组件 | profile | 原默认宿主端口 |
| --- | --- | --- |
| Kestra | `pipeline-kestra` | `18086`（`deploy/compose/pipeline-kestra/compose.pipeline.yaml:29`） |
| Kafka Connect | `connector-runtime` | `18086`（`deploy/connector-runtime/compose.connector-runtime.yaml:105`） |

两个 profile 同栈启动 → `port is already allocated`，`stage-2-integration` 5.4「同时/分组启动 Wave 1/2 运行时 profile」无法通过。

同栈复核时发现第二处冲突：Temporal UI 默认 `18088`（`deploy/compose/action-temporal/compose.temporal.yaml:41`）与 search worker 宿主端口 `18088`（`deploy/compose/search/compose.search.yaml:40`）冲突，`connector-runtime`+`search`+`action-temporal` 三 profile 同栈时同样会撞端口。

## 2. 修改

- `deploy/connector-runtime/compose.connector-runtime.yaml:64,105`：Connect 默认 `18086` → `18089`，保留 `ONTOAXIS_CONNECT_HOST_PORT` 覆盖。
- 同步 `deploy/connector-runtime/README.md`、`deploy/connector-runtime/scripts/verify-connector-runtime.sh:14,22`、`deploy/connector-runtime/scripts/cleanup.sh:19,29`、`deploy/connector-runtime/connect/README.md:116,122`。
- `internal/modules/connectors/runtime/runtime_integration_test.go:25,35`：集成测试默认 URL `18086` → `18089`。
- `deploy/compose/search/compose.search.yaml` 与 `deploy/compose/compose.yaml`：search worker 宿主端口参数化为 `127.0.0.1:${ONTOAXIS_SEARCH_WORKER_PORT:-18088}:8081`（默认不变，与 Temporal UI 同栈时覆盖为 `18091`）。
- `docs/architecture/services-and-ports.md` 补登记：Kafka Connect `18089`、Redpanda `19092`、source-postgres `15434`、Meilisearch `17700`、search worker `18088`（`ONTOAXIS_SEARCH_WORKER_PORT`，容器内 metrics `9094`）。

## 3. 验证（配置级）

命令（`KESTRA_BASIC_AUTH_USERNAME/PASSWORD` 为必填插值，使用一次性占位值）：

```
KESTRA_BASIC_AUTH_USERNAME=cc KESTRA_BASIC_AUTH_PASSWORD=cc ONTOAXIS_SEARCH_WORKER_PORT=18091 \
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  -f deploy/connector-runtime/compose.trino-iceberg.yaml \
  -f deploy/compose/pipeline-kestra/compose.pipeline.yaml \
  -f deploy/compose/action-temporal/compose.temporal.yaml \
  -f deploy/compose/search/compose.search.yaml \
  --profile connector-runtime --profile pipeline-kestra --profile search config -q
```

- `config -q`：exit 0。
- 合并后 16 个服务，按 `published` 宿主端口统计**无重复**：postgres `15433`、source-postgres `15434`、meilisearch `17700`、ontoaxis-core `18080`、trino `18081`、web `18082`、keycloak `18083`、kestra `18086`、temporal-ui `18088`、kafka-connect `18089`、search-worker `18091`（覆盖值）、ontoaxis-core metrics `19090`、redpanda `19092`。

## 4. 限制

- 未执行真实 `up -d` 健康矩阵（本机仅有 `getmeili/meilisearch` 镜像，Connect/Kestra/Temporal/Redpanda 镜像需拉取数 GB）。因此本条只提供**配置级**证据：无重复宿主端口、`config -q` 通过。
- 5.4 的「分组 `up -d` + 各组件 `/readyz`」仍需在能拉取全部镜像的环境（CI 或交付演练机）执行后回填。
