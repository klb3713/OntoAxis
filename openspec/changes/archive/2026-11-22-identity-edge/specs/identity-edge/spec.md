# Spec Delta: identity-edge

## Purpose

定义 OntoAxis 边缘入口的身份认证与租户上下文契约，使所有业务请求在进入模块前完成 JWT 校验、租户绑定与 fail-closed 拒绝。

## ADDED Requirements

### Requirement: JWT 访问令牌校验

系统 SHALL 对业务请求的 Bearer JWT 校验 RS256 签名、`iss`、`aud`（必须包含 `ontoaxis-api`）、`exp`（允许 30 秒 clock skew）与 `kid`；SHALL 拒绝 `alg=none`、HS256 和未知 `kid` 的令牌。未携带或校验失败的令牌 SHALL 返回 401 `unauthenticated`。

#### Scenario: 有效令牌通过校验

- **WHEN** 请求携带由受信 JWKS 中对应 `kid` 的 RS256 私钥签发且 `iss`/`aud`/`exp` 正确的令牌
- **THEN** 校验通过，请求继续进入下一处理层

#### Scenario: 未携带令牌

- **WHEN** 请求未启用网关信任模式且没有 `Authorization` 头
- **THEN** 返回 401，`ErrorDetail.code` 为 `unauthenticated`

#### Scenario: 拒绝非 RS256 算法

- **WHEN** JWT header 的 `alg` 为 `none` 或 `HS256`
- **THEN** 返回 401，且不读取公钥、不解析 claims 为可信身份

#### Scenario: 过期与 30 秒时钟偏移

- **WHEN** `exp` 早于当前时间不超过 30 秒
- **THEN** 令牌仍被接受
- **WHEN** `exp` 早于当前时间超过 30 秒
- **THEN** 返回 401

#### Scenario: 未知 kid 强制刷新 JWKS

- **WHEN** 令牌 `kid` 不在 JWKS 缓存中
- **THEN** 系统强制刷新 JWKS 一次；刷新后仍无该 `kid` 时返回 401

### Requirement: Claims 到租户上下文映射

系统 SHALL 将已验证 claims 映射为唯一 `TenantContext`：`sub` → `UserID`、`tenant_id` → `TenantID`、`org_ids` → `OrgIDs`、`workspace_ids` → `WorkspaceIDs`、`roles` → `Roles`；`tenant_id` 缺失或非法时 SHALL 返回 403 `permission_denied`。角色 SHALL 过滤为已知集合并按字典序排序。

#### Scenario: 完整 claims 映射

- **WHEN** 令牌包含有效 UUID 的 `sub`、`tenant_id`、`org_ids`、`workspace_ids` 与已知 `roles`
- **THEN** `TenantContext` 各字段与 claims 一致

#### Scenario: 缺失 tenant_id

- **WHEN** 有效令牌没有 `tenant_id` 或值为空
- **THEN** 返回 403，`ErrorDetail.code` 为 `permission_denied`

#### Scenario: 非法 tenant_id

- **WHEN** `tenant_id` 不是合法 UUID
- **THEN** 返回 403，不构造可用租户上下文

#### Scenario: 未知角色按最小权限处理

- **WHEN** `roles` 中包含未定义角色
- **THEN** 未知角色被丢弃，`TenantContext.Roles` 仅保留已知角色

### Requirement: 客户端租户选择

系统 SHALL 校验可选 `X-Tenant-Id`：与 JWT `tenant_id` 一致时通过；不一致时仅当主体含 `platform-admin` 且目标租户在 `org_ids` 中才允许切换，否则 SHALL 返回 403 `permission_denied`；`X-Tenant-Id` MUST NOT 直接覆盖 JWT 租户值。

#### Scenario: 一致的租户头

- **WHEN** `X-Tenant-Id` 与 JWT `tenant_id` 相同
- **THEN** 请求使用该租户上下文并继续处理

#### Scenario: 普通成员伪造租户头

- **WHEN** 非 `platform-admin` 主体的 `X-Tenant-Id` 与 JWT `tenant_id` 不一致
- **THEN** 返回 403 `permission_denied`

#### Scenario: platform-admin 选择 org_ids 内租户

- **WHEN** `platform-admin` 的 `X-Tenant-Id` 指向 `org_ids` 中的某个租户
- **THEN** `TenantContext.TenantID` 为该目标租户

#### Scenario: platform-admin 选择范围外租户

- **WHEN** `platform-admin` 的 `X-Tenant-Id` 不在 `org_ids` 中
- **THEN** 返回 403 `permission_denied`

### Requirement: 信任网关模式与 header 注入防护

系统 SHALL 在 `ONTOAXIS_TRUST_GATEWAY=true` 时仅从 `x-ontoaxis-tenant-id`、`x-ontoaxis-user-id`、`x-ontoaxis-org-ids`、`x-ontoaxis-workspace-ids`、`x-ontoaxis-roles`、`x-ontoaxis-request-id` 构造 `TenantContext`；SHALL 拒绝同时携带 `Authorization` 与任一内部 header 的请求，以及直连模式下携带内部 header 的请求；缺失租户 header SHALL 返回 403。

#### Scenario: 网关模式注入

- **WHEN** 请求只携带合法的内部 header 且不含 `Authorization`
- **THEN** `TenantContext` 字段与 header 一致，请求继续处理

#### Scenario: 混用凭证被拒绝

- **WHEN** 请求同时携带 `Authorization` 与任一 `x-ontoaxis-*` header
- **THEN** 请求被拒绝且不构造 `TenantContext`

#### Scenario: 缺失租户 header

- **WHEN** 网关模式请求缺少 `x-ontoaxis-tenant-id`
- **THEN** 返回 403 `permission_denied`

#### Scenario: 非法 header UUID

- **WHEN** `x-ontoaxis-tenant-id` 或 `x-ontoaxis-user-id` 不是合法 UUID
- **THEN** 请求被拒绝且不构造 `TenantContext`

#### Scenario: 内部 header 角色排序

- **WHEN** `x-ontoaxis-roles` 是逗号分隔的角色集合
- **THEN** `TenantContext.Roles` 为过滤未知角色、去重并排序后的已知角色

### Requirement: 统一鉴权错误响应

系统的 401/403 响应 SHALL 使用 `ErrorDetail` 信封 `{"error":{"code","message","metadata","retryable","request_id"}}`；401 的 `code` SHALL 为 `unauthenticated`，403 的 `code` SHALL 为 `permission_denied`；`request_id` SHALL 与响应头 `X-Request-Id` 一致，错误消息 MUST NOT 泄漏用户是否存在。

#### Scenario: 401 响应形状

- **WHEN** 请求没有有效凭证
- **THEN** 返回 401 JSON `ErrorDetail`，`code` 为 `unauthenticated`

#### Scenario: 403 响应形状

- **WHEN** 有效主体缺少租户
- **THEN** 返回 403 JSON `ErrorDetail`，`code` 为 `permission_denied`

#### Scenario: request_id 一致性

- **WHEN** 请求携带 `X-Request-Id`
- **THEN** 响应体中的 `request_id` 与响应头 `X-Request-Id` 相同

#### Scenario: 不泄漏用户存在性

- **WHEN** 令牌无效
- **THEN** 返回统一的认证失败消息，不区分用户是否存在

### Requirement: 租户上下文访问契约

`TenantContext` SHALL 只由 `authn.Middleware` 写入请求上下文；`FromContext` 在缺失时返回 false，`RequireTenant` 在缺失时 SHALL 返回 `unauthenticated` 错误。业务代码 MUST NOT 自行创建或修改 `TenantContext`。

#### Scenario: 已注入上下文可读取

- **WHEN** 请求通过 `authn.Middleware`
- **THEN** handler 调用 `FromContext` 返回 true，且上下文值与注入值一致

#### Scenario: 缺失上下文 fail-closed

- **WHEN** handler 在没有中间件的请求中调用 `RequireTenant`
- **THEN** 返回 `unauthenticated` 错误

#### Scenario: 无上下文 FromContext

- **WHEN** context 未注入 `TenantContext`
- **THEN** `FromContext` 返回 false

### Requirement: 边缘路由表

`oa-edge` SHALL 使用 YAML 路由表声明路由，覆盖 `/api/v1/tenancy`、`/api/v1/ontology`、`/api/v1/datasets`、`/api/v1/connectors`、`/api/v1/ingestion`、`/api/v1/pipelines`、`/api/v1/objects`、`/api/v1/query`、`/api/v1/actions` 九个前缀并绑定模块所有权；公开路由 SHALL 只允许 `GET /healthz`、`GET /readyz`、`GET /.well-known/*`，`/metrics` 仅限集群内，其余 `/api/v1/**` MUST 要求 JWT。

#### Scenario: 九个业务前缀完整

- **WHEN** 读取边缘路由表
- **THEN** 九个前缀分别映射到 `tenancy`、`ontology`、`datasets`、`connectors`、`ingestion`、`pipelines`、`objects`、`query`、`actions` 模块

#### Scenario: 公开路由 allowlist

- **WHEN** 请求 `GET /healthz`、`GET /readyz` 或 `GET /.well-known/*`
- **THEN** 允许匿名访问
- **WHEN** 请求 `/metrics` 且来源不在集群内
- **THEN** 拒绝访问

#### Scenario: 业务路由强制 JWT

- **WHEN** 请求任意 `/api/v1/**` 且没有有效 JWT
- **THEN** 网关拒绝请求并返回 401 `unauthenticated`

#### Scenario: 客户端 header 清洗

- **WHEN** 客户端传入 `X-Tenant-Id`、`X-User-Id`、`X-Org-Ids`、`X-Workspace-Ids` 或 `X-Roles`
- **THEN** 网关先删除这些 header，再从已验证 JWT 注入 `x-ontoaxis-*` 内部 header
