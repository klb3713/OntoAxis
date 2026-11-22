# Proposal: edge-gateway

## Why

Stage 1 的入口只有 core 的直接 JWT 模式和一个约定性的 `config/edge/routes.yaml`；没有独立部署的 `oa-edge`，也没有任何机制保证 Envoy Gateway、Traefik 与 core 看到同一份路由契约。Stage 2 要求 `oa-edge` 可以独立部署、路由与 Stage 1 完全一致，并具备可验证的 401/403、限流、404/405 和 provider 间一致性，因此需要把路由表升级为可生成、可校验的单一来源。

## What Changes

- 新增 `edge-gateway` 能力（New Capability），定义 `oa-edge` 从路由表到 Envoy Gateway / Traefik 的确定性生成、路由匹配与负向响应、限流、部署一致性要求。
- 新增 `tools/edgegen` CLI 与 `make edge-generate` / `make edge-generate-check`：读取 `config/edge/routes.yaml`，生成 `deploy/edge/generated/envoy-gateway.yaml`、`deploy/edge/generated/traefik-dynamic.yaml`，并写入 `ontoaxis.dev/route-table-sha256` digest 注释。
- 新增 `config/edge/routes.schema.json` 与路由表校验；生成器拒绝重复名称/前缀、未知 auth、无效方法、非法 upstream。
- 新增 Envoy Gateway 部署资源（Gateway、HTTPRoute、SecurityPolicy JWT、BackendTrafficPolicy 限流）与 Compose Traefik 配置（forwardAuth 到 core 内部鉴权端点、RateLimit middleware、由路由表生成 router）。
- 新增 `edge-gateway` 集成测试与负向测试：无 JWT 401、伪造 header 403、未知路由 404、错误方法 405、超限 429、公开 allowlist 仅 health/ready/well-known、`/metrics` 仅集群内、provider 生成物 digest 一致。
- **不修改** `identity-edge` 的 JWT 校验、tenant context 与 header 注入契约；`oa-edge` 只负责在入口执行该契约。
- **不新建** 自研 Go 网关；Envoy Gateway 与 Traefik 分别使用其原生扩展点。

## Capabilities

### New Capabilities

- `edge-gateway`: oa-edge 的 provider 无关路由生成、路由匹配与负向响应、限流、部署一致性。

### Modified Capabilities

无。

## Impact

- 新增：`tools/edgegen/`、`config/edge/` 的 schema/校验、`deploy/edge/` 的 provider 配置与 kind/Compose 部署资产、`scripts/edge-smoke.sh`、`docs/edges/`（或 `docs/operations/edge-gateway.md`）。
- 修改：`Makefile`、`deploy/compose/compose.yaml`、`.github/workflows/ci.yml`、Stage 2 change map/evidence/index。
- 修改 core：新增仅集群内可达的 `/internal/edge-auth` 端点，复用现有 `internal/platform/authn`，不改变业务 API 契约。
- 不影响 Stage 1 core 模块 API；JWT/tenant 行为仍由 `identity-edge` 契约约束。
- 运行时新增独立 `oa-edge` 容器/Deployment 与（可选）kind/k3d 集群依赖；本地 Compose 默认端口待定，不覆盖现有 18080/18082/18083/18081。
