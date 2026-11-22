# Design: edge-gateway

## Context

`oa-edge` 是 Stage 2 的独立入口层。Stage 1 的路由表已经定义了 9 个业务前缀、公开 allowlist、内部 metrics 和 JWT/header 契约，但没有生成器、没有 provider 配置，也没有限流和负向测试。本 change 把路由表变成可执行的单一来源，并分别渲染 Envoy Gateway 与 Traefik 配置。

## Goals

- 一份 `config/edge/routes.yaml` 驱动两种 provider，禁止手工维护第二份路由。
- 路由匹配、401/403、404/405、429 和公开 allowlist 可自动化验证。
- 不新建自研 Go 网关；复用 provider 原生能力。
- 不改变 core 业务契约；`identity-edge` 的 JWT/tenant 行为仍由 core/authn 和 edge 共同遵守。

## Non-Goals

- 不引入 AI 网关、GraphQL 网关或自研反向代理。
- 不在本 change 实施 OpenFGA/Cedar 细粒度授权（属于 `authorization-openfga-cedar`）。
- 不实现生产集群外部 LoadBalancer、TLS 证书、WAF 或 CDN。
- 不把 Envoy Gateway 和 Traefik 做成完全相同的配置文件；只保证行为一致。

## Decisions

### D1: 路由表为唯一来源，生成器独立于运行时

- `config/edge/routes.yaml` 是唯一路由清单；`config/edge/routes.schema.json` 是结构契约。
- 新增 `tools/edgegen` Go CLI：读取 YAML → 校验 schema 与语义不变量 → 生成 Envoy/Traefik 产物。
- 生成器不进入 core 运行时；不增加 core 依赖。生成器复用 `gopkg.in/yaml.v3` 和标准库，避免为单次生成引入大型依赖。
- 生成时输出 `ontoaxis.dev/route-table-sha256` 注释；`make edge-generate-check` 在干净工作区比较生成物与该 digest。
- 生成物提交到 `deploy/edge/generated/`，便于 review 和离线部署；`make edge-generate` 更新。

### D2: 匹配顺序固定为可解释的优先级，而非 provider 默认顺序

顺序：协议/监听器 → 公开路由精确/前缀 allowlist → 内部路由 → 受保护最长前缀 → 鉴权（extAuth/forwardAuth）→ 清理客户端身份头并注入内部头 → 方法限制 → upstream 转发。无 catch-all 路由。客户端身份头的清理位置按 provider 实现：Traefik 在 forwardAuth 后 strip；Envoy 由 extAuth 拒绝伪造或剥离后以 JWT 派生身份继续。

理由：Envoy 与 Traefik 默认路由和 path 匹配语义不同；固定顺序后，生成器为每个 provider 显式设置优先级/rule order，避免依赖隐式行为。

### D3: Envoy Gateway 使用原生 Gateway API + SecurityPolicy

- `GatewayClass` 由集群管理员提供；本 change 提供 `Gateway`、`HTTPRoute`、`SecurityPolicy`、`BackendTrafficPolicy`。
- `HTTPRoute` 按 public、internal、受保护前缀生成，使用 `PathPrefix`；public 只匹配精确的 `/healthz`、`/readyz` 与 `/.well-known/*`。
- `SecurityPolicy.extAuth.http` 指向 core 的 `/internal/edge-auth`，复用 `internal/platform/authn` 校验 JWT 并返回 `x-ontoaxis-*` 响应头；`headersToBackend` 注入内部身份头。客户端身份 header 不参与授权；伪造值不会获得对应身份。`Authorization` 在 extAuth 后从上游请求中移除。
- `BackendTrafficPolicy.rateLimit.local` 使用 local rate limit（按客户端 IP），超限 429；Envoy 不保证 `Retry-After`，由 Traefik 提供该头。租户维度为本地 best-effort，不承诺跨副本全局精确配额。
- 不依赖 Envoy Gateway 的商业扩展。

### D4: Traefik 使用 forwardAuth 到 core 内部鉴权端点

- 不使用第三方 JWT 插件。Traefik 静态配置只负责 HTTP entrypoint、生成配置和 middleware；JWT 校验通过 `forwardAuth` 调用 core 的内部 `/internal/edge-auth`。
- `/internal/edge-auth` 复用 `internal/platform/authn`：校验 Bearer JWT、映射 `TenantContext`，成功返回 200 并回写 `x-ontoaxis-tenant-id`、`x-ontoaxis-user-id`、`x-ontoaxis-org-ids`、`x-ontoaxis-workspace-ids`、`x-ontoaxis-roles`、`x-ontoaxis-request-id`；失败返回统一的 401/403 `ErrorDetail`。
- Traefik `forwardAuth` 设置 `authResponseHeaders` 将上述内部头注入上游；业务路由不暴露 `/internal/edge-auth`，该路径仅 edge→core 集群内可达。
- `forwardAuth` 不可达或 core 返回 5xx 时，Traefik 必须 fail-closed 返回 5xx（当前实现为 500），不得放行请求。
- 动态配置：每个 route 生成 router + service + middleware chain；`forwardAuth` 负责身份，`headers` middleware 负责清理客户端身份头，`ratelimit` 负责本地限流。
- 理由：避免第三方插件的维护/供应链风险，并复用已测试的 `authn` 契约，保持 Envoy/Traefik 可观察行为一致。代价是 Compose 模式下 edge 鉴权依赖 core 可达。

### D5: 限流配置

- 路由表新增 `spec.rateLimit`：
  - `defaults.requestsPerSecond`、`defaults.burst`
  - `byTenant.requestsPerMinute`（本地 best-effort）
  - `exemptPublicRoutes: true`
- 生成器把默认值写入 provider 配置；Envoy 用 `BackendTrafficPolicy.rateLimit.local`，Traefik 用 `RateLimit` middleware。
- 超限响应固定为 429；Traefik 带 `Retry-After: 1`，Envoy local rate limit 仅返回 429；健康/well-known 不受限流。
- 本 change 只承诺单实例本地限流；跨副本精确租户限流留给 `production-delivery`/后续 Stage，文档与监控必须明确该限制。

### D6: 部署与验收

- Compose：新增 `oa-edge` 服务（traefik v3），端口 `18085`，core 仅在内部网络可达；保留 core 的 18080 直连用于调试。
- kind/k3d：`deploy/edge/kind/` 提供安装 Envoy Gateway 与 CRD 的脚本；不把 GatewayClass controller 打包进 Helm chart。
- 负向测试：`scripts/edge-smoke.sh` 对两种 provider 跑同一组用例：public 200、受保护 401、伪造 header 403、未知 404、错误方法 405、超限 429、metrics 403/404。
- 证据：`docs/stages/evidence/stage-2-edge-gateway.md` + 日志 + 截图（如适用）。

## Risks / Trade-offs

- **edge 鉴权依赖 core**：core auth 端点不可用时 fail-closed 非 2xx（Traefik 500；Envoy extAuth 默认 403）；本地/kind 可接受，生产 edge 自治另议。
- **Envoy/Traefik 限流语义差异**：本地限流是单实例近似；多副本生产需外部 Redis/Valkey 限流服务，本 change 不承诺跨副本精确限流。
- **路由表变更需要重新生成**：`edge-generate-check` 会阻断 CI；接受该成本以换取单一来源。
- **不实现 TLS/LoadBalancer**：Stage 2 验收限定 kind/k3d 与 Compose，生产入口另由 `production-delivery` 处理。

## Migration Plan

1. 生成器与校验：新增 `tools/edgegen`、schema 校验、`make edge-generate[-check]`。
2. Envoy 渲染与 kind 部署：先验证 Gateway API/HTTPRoute/SecurityPolicy 可直接应用。
3. Traefik 渲染与 Compose：验证 forwardAuth、header 注入与限流。
4. 负向测试脚本：两种 provider 跑同一组用例。
5. 文档与 evidence：更新 config/edge/README、ARCHITECTURE 15.3、Stage 2 change map/evidence。

## Open Questions

- 是否在后续 Stage 引入 edge 自治鉴权（插件或独立 auth sidecar），使 Compose edge 不依赖 core；本 change 明确不做。
- 租户级限流是否需要跨副本精确计数；若是，则需 Valkey 限流后端（可能属于 production-delivery）。
