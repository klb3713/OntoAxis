# 运行时拓扑

## 入口

- **oa-web**：React 控制台，容器 8080，宿主 18082；OIDC 登录 Keycloak。
- **oa-edge**：可选独立入口，K8s 使用 Envoy Gateway，Compose 使用 Traefik（`edge` profile，宿主 18085）。`oa-edge` 从 `config/edge/routes.yaml` 生成，负责路由、JWT forward-auth、身份头注入与本地限流。
- **ontoaxis-core**：单进程 Go API，容器 8080，宿主 18080；direct 模式服务 `oa-web`。
- **ontoaxis-core-edge**：仅 `edge` profile 启动的 core 副本，`ONTOAXIS_TRUST_GATEWAY=true`，只接受 `oa-edge` 注入的 `x-ontoaxis-*` 身份头。

## 依赖面

| 组件 | 作用 | 本地端口 |
| --- | --- | --- |
| PostgreSQL | 平台唯一关系库，模块各自 schema | 15433 |
| Keycloak | OIDC / JWT issuer | 18083 |
| Trino | SQL 查询执行 | 18081 |
| OTel Collector / Prometheus / Jaeger / Perses | 观测 profile | 14317/19091/16686/18084 |

请求链：`client → oa-edge (Traefik/Envoy) → ontoaxis-core-edge → module api → service → repo`。`oa-edge` 的 forward-auth 调用 core `/internal/edge-auth`，成功后把 `x-ontoaxis-*` 注入上游；direct core 路径不经过该端点。
