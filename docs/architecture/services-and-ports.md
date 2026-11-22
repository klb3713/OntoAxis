# 服务与端口

| 服务/组件 | 端口 | 暴露范围 |
| --- | --- | --- |
| oa-web | 18082 | 本地入口 |
| oa-edge（Traefik） | 18085 | `edge` profile |
| ontoaxis-core | 18080 | direct API |
| ontoaxis-core metrics | 19090 | Prometheus |
| ontoaxis-core-edge | 8080（容器内） | 仅 edge 网络 |
| Keycloak | 18083 | OIDC |
| Trino | 18081 | SQL |
| PostgreSQL | 15433 | 数据库 |

## 业务前缀

`/api/v1/{tenancy,ontology,datasets,connectors,ingestion,pipelines,objects,query,actions}`。所有业务前缀受 JWT 保护；`/healthz`、`/readyz`、`/.well-known/*` 公开；`/metrics` 仅核心指标监听器暴露，不经 `oa-edge` 公网入口。

## 路由真源

`config/edge/routes.yaml` 是唯一路由声明；`tools/edgegen` 生成 `deploy/edge/generated/envoy-httproute.yaml` 与 `traefik-dynamic.yaml`。`scripts/edge-smoke.sh` 验证 401/403/404/405/429 与 fail-closed 5xx。
