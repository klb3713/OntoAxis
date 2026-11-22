# Design: identity-edge

## Context

`stage-1-platform-foundation` 已建立单 Go module、模块注册、`httperr` 错误模型与 HTTP 生命周期，但 `internal/platform/authn` 尚不存在，任何业务模块都还没有可信的身份来源。`ARCHITECTURE.md` 6.3 把 JWT 校验、claims 映射和 `TenantContext` 定为 P0 契约；16.2 要求 `oa-edge` 用 YAML 路由表声明路径所有权并在边缘强制 JWT。Stage 1 brief 的 S1-AC-02 要求租户上下文从第一天起 fail-closed。

本设计只交付身份边界与被动的路由表骨架，不连接真实 Keycloak，不实现业务模块，也不修改已冻结的 `go.mod`/`go.sum`。

## Goals / Non-Goals

**Goals**

- 建立包内唯一的 `TenantContext` 与访问函数，业务代码只能读取、不能伪造。
- 以可替换的 `Verifier`/`KeySet`/`KeySource` 隔离 JWT 实现，默认实现满足 RS256、`iss`/`aud`/`exp`/`kid` 校验。
- 提供 `EdgeAuthenticator`：直连模式验 JWT，网关信任模式读内部 header，两种模式都拒绝混用与伪造。
- 所有 401/403 响应为稳定 `ErrorDetail`，且 `request_id` 与 `X-Request-Id` 一致。
- `config/edge/routes.yaml` 作为九个业务前缀的唯一路由真源，并由单元测试校验完整性。

**Non-Goals**

- 不实现真实 OIDC 登录、realm/Organizations 配置、token 签发。
- 不引入第三方 JWT 库或修改 `go.mod`/`go.sum`；不实现完整 JWKS HTTP 客户端（仅接口与可注入 fake）。
- 不实现 Envoy Gateway/Traefik 资源生成、部署与 `/metrics` 网络策略落地。
- 不实现 OpenFGA/Cedar 细粒度授权与业务 handler。

## Decisions

### D1：JWT 校验采用标准库最小实现

**决策**：`internal/platform/authn/jwt.go` 用 `crypto/rsa` + `crypto/sha256` 实现 compact JWS（三段 base64url）的 RS256 验签，并使用 encoding/json 解析 header/claims。**理由**：`go.mod` 禁止修改，引入 `golang-jwt` 或 `lestrrat-go/jwx` 会破坏已冻结的依赖基线；标准库足以实现本 change 的验证路径。**替代方案**：等待基座 owner 批准新依赖（被范围排除）；自行 vendoring（会引入未审计代码，拒绝）。**后果**：缺少现成的 claim 校验工具函数，需要自行处理 base64url、JSON number 与错误分类；后续接入真实 JWKS 时建议由平台 owner 评估正式库并替换 `Verifier` 实现，接口不变。

### D2：`Verifier` / `KeySet` / `KeySource` 三层接口

```go
type Claims struct {
    Issuer       string
    Audience     []string
    Subject      string
    TenantID     string
    OrgIDs       []string
    WorkspaceIDs []string
    Roles        []string
    Email        string
    Name         string
    ExpiresAt    time.Time
    IssuedAt     time.Time
    AuthTime     time.Time
}

type Verifier interface {
    Verify(ctx context.Context, rawToken string) (Claims, error)
}

type KeySet interface {
    LookupKey(ctx context.Context, kid string) (crypto.PublicKey, bool)
}

type KeySource interface {
    Keys(ctx context.Context, forceRefresh bool) (KeySet, error)
}
```

`JWKSVerifier` 持有 `Issuer`、`Audience`、`KeySource`、可注入 `Now`；`StaticKeySet`/`StaticKeySource` 与 `FakeVerifier` 用于测试。**理由**：把签名获取与 claim 校验解耦，使本 change 无需网络即可完整测试；真实 JWKS 客户端后续实现 `KeySource` 即可。**替代方案**：直接让 Authenticator 解析 token（耦合、难测，拒绝）。

### D3：`TenantContext` 只由中间件写入

```go
type TenantContext struct {
    TenantID     uuid.UUID
    UserID       uuid.UUID
    OrgIDs       []uuid.UUID
    WorkspaceIDs []uuid.UUID
    Roles        []string
    RequestID    string
}

func FromContext(ctx context.Context) (TenantContext, bool)
func RequireTenant(ctx context.Context) (TenantContext, error)
```

context key 为私有类型，写入函数不导出；`FromContext` 缺失返回 `(zero, false)`，`RequireTenant` 返回包装 `ErrUnauthenticated` 的错误。**理由**：符合 6.3.2 “TenantContext 只能由 authn.Middleware 写入”；业务模块无法构造可信身份。**替代方案**：导出 `WithTenant`（便于测试但破坏不变量，拒绝）。

### D4：两种认证模式与 header 防护

`EdgeAuthenticator` 配置：

```go
type Config struct {
    TrustGateway bool
    Issuer       string
    Audience     string
}

type EdgeAuthenticator struct {
    cfg      Config
    verifier Verifier
}
```

- **直连模式（`TrustGateway=false`）**：`Middleware` 从 `Authorization: Bearer <token>` 取 token，调用 `Authenticate` → `Verifier.Verify` → `claimsToTenantContext`；无 Authorization 返回 401。请求若出现任一 `x-ontoaxis-*` 内部 header 一律拒绝（防止客户端伪造）。
- **信任网关模式（`TrustGateway=true`）**：`Middleware` 只读 `x-ontoaxis-tenant-id`、`x-ontoaxis-user-id`、`x-ontoaxis-org-ids`、`x-ontoaxis-workspace-ids`、`x-ontoaxis-roles`、`x-ontoaxis-request-id`；忽略并拒绝 `Authorization`（混用即拒绝）。缺少或非法租户/用户 UUID 返回 403。`x-ontoaxis-roles` 按逗号拆分、去重、过滤未知角色并排序。
- **公共方法 `Authenticate(ctx, rawToken)`**：只实现直连 JWT 路径，供非 HTTP 调用方（如 future gRPC/worker）复用；网关模式由 `Middleware` 解析 header，不调用 `Authenticate`。文档中明确该约定。
- **request_id**：优先 `observability.RequestID(ctx)`，其次请求头 `X-Request-Id`，再次 W3C `traceparent` 的 trace-id，最后留空；错误响应同时写 `X-Request-Id` 响应头（若已有则保留），保证 body/header 一致。

**理由**：6.3.3 明确网关先清洗再注入；直连模式也拒绝内部 header，防止绕过网关伪造身份。**替代方案**：仅靠网络策略隔离（不能防御误配置，拒绝）。

### D5：claims 校验与映射规则

`JWKSVerifier.Verify` 的校验顺序：结构解析 → `alg` 必须 `RS256`（拒绝 `none`/`HS256`）→ `kid` 非空 → 取 key（未知 `kid` 强制刷新一次）→ RSA PKCS#1 v1.5 验签 → `iss` 精确匹配 → `aud` 包含配置值 → `exp` 不早于 `now-30s`（30 秒 clock skew）→ `iat` 若存在不得晚于 `now+30s`。claims 映射：

| claim | 目标 | 规则 |
| --- | --- | --- |
| `sub` | `UserID` | 必填 UUID |
| `tenant_id` | `TenantID` | 必填 UUID；缺失/非法 → 403 `permission_denied` |
| `org_ids` | `OrgIDs` | 可选 string[]，每项 UUID；非法项丢弃并告警？→ 严格拒绝整枚令牌，返回 401 |
| `workspace_ids` | `WorkspaceIDs` | 可选 string[]，每项 UUID；非法 → 401 |
| `roles` | `Roles` | 必填；过滤到已知集合，去重排序 |
| `email` | 仅校验 | 必填非空，不进入 TenantContext |
| `request_id` | `RequestID` | 来自上下文，不来自 token |

`aud` 兼容字符串或字符串数组。`roles` 缺失视为空集合（不阻断），但 `tenant_id` 缺失必须 403。考虑到错误信封要求 401 用于“无效 token”，claims 解析/签名/iss/aud/exp 失败统一 401；仅“令牌有效但租户缺失/非法”为 403。**理由**：与 6.3.4/6.3.5 的错误码语义一致；避免把格式错误误报为权限问题。

### D6：错误响应本地实现，不修改 httperr

`internal/platform/httperr` 不在写入范围，其 `statusFor` 不认识 `unauthenticated`/`permission_denied`。因此 `authn/errors.go` 定义：

```go
type ErrorDetail struct {
    Error ErrorBody `json:"error"`
}
type ErrorBody struct {
    Code      string            `json:"code"`
    Message   string            `json:"message"`
    Metadata  map[string]string `json:"metadata"`
    Retryable bool              `json:"retryable"`
    RequestID string            `json:"request_id"`
}
```

`writeAuthError(w, r, status, code, message)` 设置 `Content-Type`、`X-Request-Id`、状态码并写 JSON。401 使用 `unauthenticated`/状态 401；403 使用 `permission_denied`/状态 403。`metadata` 始终为对象（空 map），`retryable` 固定 false。**理由**：在最小写入范围内满足精确 code；不复制业务错误逻辑到 httperr。**后续**：平台 owner 可把两个 code 加入 `httperr` 后删除本地 writer。

### D7：路由表为声明式 YAML + 测试校验

`config/edge/routes.yaml` 结构：

```yaml
apiVersion: ontoaxis.edge/v1
kind: EdgeRouteTable
gateway:
  provider: envoy-gateway
  listener: oa-edge
  publicRoutes:
    - method: GET
      path: /healthz
    - method: GET
      path: /readyz
    - method: GET
      path: /.well-known/*
  metrics:
    path: /metrics
    internalOnly: true
  clientHeadersToStrip:
    - X-Tenant-Id
    - X-User-Id
    - X-Org-Ids
    - X-Workspace-Ids
    - X-Roles
  internalHeadersToInject:
    - x-ontoaxis-tenant-id
    - x-ontoaxis-user-id
    - x-ontoaxis-org-ids
    - x-ontoaxis-workspace-ids
    - x-ontoaxis-roles
    - x-ontoaxis-request-id
routes:
  - prefix: /api/v1/tenancy
    module: tenancy
    auth: jwt
  ...
```

九个前缀各一条，`auth: jwt`。`internal/platform/authn/routes_test.go` 读取 `../../../config/edge/routes.yaml`（相对包目录）解析并断言：九条路由存在且 module 正确、无重复前缀、公开路由仅三条、`/metrics` internalOnly。**理由**：不修改 `cmd/`/`internal/app/` 的前提下，让路由表有可执行校验，避免“文件存在但内容漂移”。**替代方案**：手写配置结构体（YAML 真源重复，拒绝）；用 Go 常量生成 YAML（本 change 不引入生成器，拒绝）。

### D8：包布局

```
internal/platform/authn/
  context.go        # TenantContext / FromContext / RequireTenant
  claims.go         # Claims 结构、角色集合、UUID 解析
  jwt.go            # compact JWS 解析、RS256 验签、JWKSVerifier
  keyset.go         # KeySet / KeySource / StaticKeySet / StaticKeySource
  fake.go           # FakeVerifier（测试与非生产装配用）
  authenticator.go  # Authenticator 接口、Config、EdgeAuthenticator
  middleware.go     # Middleware、header 解析与防护
  errors.go         # ErrorDetail、writeAuthError
  doc.go            # 包文档：唯一写入契约
  *_test.go
config/edge/
  routes.yaml
  README.md
```

**理由**：按关注点拆分，测试可单独覆盖 verifier 与 middleware；`fake.go` 是非 `_test.go` 文件，供其他模块测试装配使用。

## Risks / Trade-offs

- **自行实现 JWT 校验的风险**：base64url、JSON 数字等边界处理不如成熟库；缓解：仅接受紧凑三段式、限制 header/claims 大小、覆盖负向测试；正式环境前由安全评审确认是否替换为审计过的库。
- **JWKS 无真实 HTTP 客户端**：本 change 只能证明接口与 fake 路径；真实 Keycloak 集成证据缺失，必须在后续环境授权后补集成测试与 `make verify` 证据。不得把本 change 的测试写成真实认证验收。
- **路由表无运行时消费者**：`config/edge/routes.yaml` 目前是骨架，网关部署配置（Envoy Gateway/Traefik）尚未生成；需在 `deploy/` 授权的 change 中落地。
- **错误码与 httperr 的短暂重复**：本地 `writeAuthError` 与 `httperr.Error` 信封一致但 code 集合不同；后续合并可减少重复，但当前不能修改 httperr。
- **`X-Tenant-Id` 的切换语义**：platform-admin 只能在 `org_ids` 内切换；若未来引入组织层级需重新评估，当前严格按 6.3.2。

## Migration Plan

1. 本 change 只新增包与配置，不修改现有路由/中间件装配；因此对现有 `cmd/` 与业务模块零影响。
2. 后续模块接入时，由 `internal/app` 在受保护路由上挂 `authn.Middleware`，并通过 `authn.FromContext`/`RequireTenant` 读取身份；本 change 不修改这些文件。
3. 引入真实 JWKS 客户端时，实现 `KeySource` 并替换 `JWKSVerifier` 的 `KeySource`，接口保持不变；同时评估是否把 JWT 库纳入 go.mod。
4. 若 `httperr` 后续增加 `unauthenticated`/`permission_denied` 常量，将 `authn/errors.go` 的 writer 切换为 `httperr.Write`，删除重复定义。

## Open Questions

- 真实 Keycloak 的 JWKS URL、realm 与 `aud` 值需在环境授权后确认；本 change 以 `https://auth.ontoaxis.local/realms/ontoaxis` 与 `ontoaxis-api` 为配置默认值，不视为已部署事实。
- `/metrics` 的“集群内”判定由网关/网络策略负责，具体实现待 `deploy/` 范围确认。

## Testing Strategy

- `jwt_test.go`：有效 RS256 令牌；`alg=none`、HS256、篡改签名、错误 `iss`/`aud`、过期超过 30 秒、未知 `kid` 触发一次刷新、key 轮换；claims 解析含字符串/数组 audience。
- `context_test.go`：`FromContext` 缺失；`RequireTenant` 缺失返回 `ErrUnauthenticated`；注入后读取。
- `authenticator_test.go`：直连模式缺失/非法 token → 401；有效 token → 上下文；`X-Tenant-Id` 一致/不一致/非法；platform-admin 允许/拒绝；网关模式注入、混用拒绝、缺失/非法 header 拒绝、角色排序。
- `errors_test.go`：401/403 形状、`content-type`、`X-Request-Id` 与 body 一致。
- `routes_test.go`：九个前缀、公开 allowlist、`/metrics` internalOnly。
- 运行：`export PATH=/Users/klb3713/.local/share/go-toolchain/go/bin:$PATH && go test ./internal/platform/authn/...`，并 `gofmt -l internal/platform/authn` 为空。
