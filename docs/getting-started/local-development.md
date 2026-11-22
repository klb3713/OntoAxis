# 本地开发

## 前置条件

- Docker 24+ / Docker Compose v2
- Go 1.26+（如本机没有 Go，可使用 Docker `golang:1.26.3`）
- Node.js 22+ 与 pnpm 10+（前端构建与测试）

## 一键启动

```sh
docker compose -f deploy/compose/compose.yaml up -d --build
```

Compose 包含：

| 服务 | 宿主机端口 | 说明 |
| --- | --- | --- |
| web | 18082 | `oa-web`，OIDC 登录入口 |
| ontoaxis-core | 18080 | 单进程 API |
| ontoaxis-core metrics | 19090 | Prometheus 指标 |
| Keycloak | 18083 | realm `ontoaxis` |
| Trino | 18081 | 单节点查询引擎 |
| PostgreSQL | 15433 | 平台存储 |
| oa-edge | 18085（`edge` profile） | Traefik 入口，forwardAuth 到 core |

浏览器打开 <http://127.0.0.1:18082/>，使用 Keycloak 用户 `admin` / `admin123` 登录。

## oa-edge 本地入口（可选）

```sh
make edge-generate
docker compose -f deploy/compose/compose.yaml --profile edge up -d --build
EDGE_TEST_FAIL_CLOSED=1 ./scripts/edge-smoke.sh
```

`edge` profile 启动 `oa-edge`（Traefik，宿主 18085）与一个 `ONTOAXIS_TRUST_GATEWAY=true` 的 core 副本；direct core 仍用于 `oa-web`，两者共享 PostgreSQL。`scripts/edge-smoke.sh` 覆盖 200/401/403/404/405/429 与鉴权后端不可达的 fail-closed 5xx。Envoy 清单在 `deploy/edge/generated/envoy-httproute.yaml`，kind 安装/校验见 `deploy/edge/kind/`。

## 验证

```sh
curl -fsS http://127.0.0.1:18080/healthz
curl -fsS http://127.0.0.1:18080/readyz
curl -fsS http://127.0.0.1:19090/metrics | head
```

`/readyz` 应返回 9 个业务模块 + `database` 共 10 项检查，全部为 `ok`。

## 直接运行 core

```sh
export ONTOAXIS_DATABASE_URL='postgres://ontoaxis:ontoaxis@localhost:15433/ontoaxis?sslmode=disable'
export ONTOAXIS_OIDC_ISSUER='http://localhost:18083/realms/ontoaxis'
export ONTOAXIS_OIDC_AUDIENCE='ontoaxis-api'
export ONTOAXIS_OIDC_JWKS_URL='http://localhost:18083/realms/ontoaxis/protocol/openid-connect/certs'
export ONTOAXIS_TRINO_URL='http://localhost:18081'
go run ./cmd/ontoaxis-core
```

端口由 `ONTOAXIS_HTTP_ADDR`（默认 `:8080`）和 `ONTOAXIS_METRICS_ADDR`（默认 `:9090`）控制。

## 测试与格式

```sh
go build ./...
go test ./...
go test -race ./...
gofmt -w cmd internal
docker run --rm -v "$PWD":/src -w /src golangci/golangci-lint:v2.15.0 golangci-lint run ./...
cd web && pnpm install --frozen-lockfile && pnpm check && pnpm test && pnpm build
```

## 迁移

平台迁移位于 `internal/platform/db/migrations/`，由 goose 在启动时执行。各业务模块迁移放在 `internal/modules/<module>/migrations/`，按模块依赖顺序执行；迁移只向前，不修改已发布文件。

## 本地观测栈

默认 `docker compose up` 只启动 core 与依赖，不启动可观测组件。要启用本地
Collector、Prometheus、Alertmanager、Jaeger 与 Perses：

```sh
export ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
docker compose -f deploy/compose/compose.yaml --profile observability up -d --build
```

| 组件 | 宿主机端口 | 用途 |
| --- | --- | --- |
| OTel Collector | 14317/14318 | OTLP gRPC/HTTP |
| Prometheus | 19091 | 指标与规则评估 |
| Alertmanager | 19093 | 告警路由 |
| Jaeger | 16686 | trace 查询 |
| Perses | 18084 | dashboard |

core 的 `ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT` 为空时 tracing 为 no-op；
设置后使用 OTLP/HTTP 导出。本地采样率为 `1.0`，非本地环境通过
`ONTOAXIS_OTEL_SAMPLE_RATIO` 配置。

验证：

```sh
make observability-rules-test
scripts/observability-smoke.sh
```

`scripts/observability-smoke.sh` 会启动 profile、请求 core、确认 Prometheus
收到 HTTP 指标且 Jaeger 收到 `ontoaxis-core` trace。

## 清理

```sh
docker compose -f deploy/compose/compose.yaml down -v
```

## Stage 2 运行时配置

Stage 2 新增的非密钥配置键已列入 `.env.example`，包括：
- 授权：`ONTOAXIS_AUTHZ_OPENFGA_API_URL/STORE_ID/MODEL_ID/API_TOKEN/POLICY_DIR/AUDIT_SINK`
- 对象搜索：`ONTOAXIS_SEARCH_ENABLED/URL/API_KEY/INDEX_VERSION`
- Actions/Temporal：`ONTOAXIS_ACTIONS_EXECUTION_MODE/TEMPORAL_HOST_PORT/ACTIONS_APPROVAL_TIMEOUT/ACTIONS_WORKFLOW_TIMEOUT/ACTIONS_WORKER_HTTP_ADDR`
- Connector runtime：`ONTOAXIS_CONNECT_URL/TIMEOUT/RETRY_MAX/VERIFY_MAX_ROWS`
- Pipelines/Kestra：`ONTOAXIS_KESTRA_URL/TIMEOUT/API_TOKEN`、`ONTOAXIS_PIPELINE_DBT_CATALOG/DBT_IMAGE`

真实 token/密码/生产连接串不得写入仓库；本地由 compose authz profile 或环境变量注入。默认 `ACTIONS_EXECUTION_MODE=noop` 仅用于本地无 Temporal 环境，生产默认 `temporal` 且缺少 Temporal host 启动失败。
