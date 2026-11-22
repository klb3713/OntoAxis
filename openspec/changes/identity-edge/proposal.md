# Proposal: identity-edge

## Why

Stage 1 已批准，平台基座（`stage-1-platform-foundation`）已冻结 Go module、模块注册、错误模型与 HTTP 生命周期，但请求链仍缺少身份边界：客户端请求没有可信的 JWT 校验、租户上下文和边缘路由约束。若各业务模块自行解析 token 或信任客户端传入的租户头，将直接破坏 `ARCHITECTURE.md` 6.3 的 P0 契约，也无法满足 S1-AC-02 的 fail-closed 要求。

本 change 在 Wave 1 先建立统一的 `internal/platform/authn` 与 `config/edge`，让后续所有业务模块只消费同一个 `TenantContext`，不重复实现认证。

## What Changes

- 新增 `internal/platform/authn`：定义唯一 `TenantContext`、`FromContext`/`RequireTenant`、`Authenticator` 接口与 claims 结构（`iss`/`aud`/`sub`/`tenant_id`/`org_ids`/`workspace_ids`/`roles`/`email`/`exp`）。
- 实现 JWT 访问令牌校验：固定 RS256、校验 `iss`/`aud`/`exp`（30 秒 clock skew）/`kid`，拒绝 `alg=none`、HS256 与未知 `kid`；通过可替换的 JWKS/verifier 接口隔离 Keycloak，本 change 不连接真实 Keycloak。
- 实现认证中间件：从 `Authorization: Bearer` 解析并注入 `TenantContext`；支持 `ONTOAXIS_TRUST_GATEWAY=true` 时读取网关注入的 `x-ontoaxis-*` header，并拒绝 Authorization 与内部 header 混用、伪造 `X-Tenant-Id` 等注入路径。
- 统一 401/403 响应为 `ErrorDetail`（`unauthenticated`/`permission_denied`，带 `request_id`）；缺失租户返回 403，未带或无效令牌返回 401；提供测试用 fake verifier 与 fail-closed 负向测试。
- 新增 `config/edge/routes.yaml` YAML 路由表骨架，声明 `/api/v1/tenancy`、`/api/v1/ontology`、`/api/v1/datasets`、`/api/v1/connectors`、`/api/v1/ingestion`、`/api/v1/pipelines`、`/api/v1/objects`、`/api/v1/query`、`/api/v1/actions` 的模块所有权、JWT 要求与公开路由 allowlist，并声明客户端 header 清洗/内部 header 注入清单。

**Out of scope（明确不做）**

- 不修改 `go.mod`/`go.sum`，不引入第三方 JWT 库；本 change 用标准库实现最小 RS256 校验，并保留 verifier/JWKS 接口，后续由基座 owner 评估正式库依赖。
- 不启动或连接真实 Keycloak、不创建 realm/Organizations、不实现 OIDC 登录页。
- 不实现 Envoy Gateway/Traefik 配置生成器与真实部署；`config/edge/routes.yaml` 只作为路由真源骨架。
- 不实现 OpenFGA/Cedar 细粒度授权与业务模块路由。
- 不修改 `cmd/`、`internal/app/`、`internal/platform/httpserver/`、`deploy/` 及其他模块目录；不提交 commit。

## Capabilities

### New Capabilities

- `identity-edge`: 边缘身份认证与租户上下文契约，包括 JWT 校验、claims 映射、认证中间件、鉴权错误响应与边缘路由表约束。

### Modified Capabilities

- 无。当前 `openspec/specs/` 为空，本 change 不修改既有 capability。

> 说明：change 目录名 `identity-edge` 与 capability 路径 `identity-edge` 一致；Stage 归属由 Stage brief/change map 与 `traceability.md` 记录，不写入 capability 名称。

## Impact

- 新增 `internal/platform/authn/`：`TenantContext`、claims、JWT verifier、JWKS 接口与缓存契约、认证中间件、fake verifier 与单元测试。
- 新增 `config/edge/routes.yaml` 与 `config/edge/README.md`；不修改 `go.mod`/`go.sum`。
- 后续 Wave 1 业务模块通过 `authn.Middleware` 获取 `TenantContext`，不得自行解析 JWT 或创建上下文。
- 与 `ARCHITECTURE.md` 6.1/6.3、16.2 及 Stage 1 S1-AC-02 对齐；真实 Keycloak OIDC 登录和网关部署仍为未覆盖项，需在后续集成/环境授权后补证。
