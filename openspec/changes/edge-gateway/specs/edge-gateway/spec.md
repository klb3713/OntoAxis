# edge-gateway Specification

## Purpose

定义 `oa-edge` 独立边缘入口的 provider 无关路由生成、匹配顺序、公开/受保护边界、限流与部署一致性，使 Envoy Gateway 与 Traefik 共享同一份路由契约并对未认证、越权、未知路由和超限请求给出一致响应。

## ADDED Requirements

### Requirement: 路由表单一来源与确定性生成

`oa-edge` SHALL 只从一份声明的路由表生成 provider 配置；生成结果 MUST 确定（相同输入产生字节相同的输出）并携带路由表内容摘要；系统 MUST 拒绝重复的路由名称或前缀、未知的 auth 值、非法 HTTP 方法以及非法的 upstream。

#### Scenario: 相同输入生成相同配置

- **WHEN** 对同一路由表连续执行两次生成
- **THEN** 两次产物的字节内容与摘要注释完全一致

#### Scenario: 重复前缀被拒绝

- **WHEN** 路由表包含两个相同 `prefix` 的受保护路由
- **THEN** 生成失败并报告冲突的路由名称与前缀

#### Scenario: 未知 auth 被拒绝

- **WHEN** 路由的 `auth` 不是路由表 schema 允许的值
- **THEN** 生成失败，不产生任何 provider 配置文件

#### Scenario: 摘要可复核

- **WHEN** 路由表内容变化
- **THEN** 生成产物的 digest 注释变化，`edge-generate-check` 能检测到未重新生成

### Requirement: 路由匹配与负向响应

`oa-edge` SHALL 按协议/监听器、公开路由、内部路由、受保护最长前缀、HTTP 方法、身份校验、header 注入、upstream 转发的顺序处理请求；未知路径 SHALL 返回 404，已知路径不支持的方法 SHALL 返回 405，MUST NOT 存在 catch-all 路由。

#### Scenario: 最长前缀优先

- **WHEN** 请求路径同时匹配 `/api/v1/objects` 与更长的受保护前缀
- **THEN** 使用更长前缀对应的路由与模块

#### Scenario: 未知路径

- **WHEN** 请求 `/api/v1/not-a-route`
- **THEN** 返回 404 `unknown_service_route`，且不转发到 core

#### Scenario: 不支持的方法

- **WHEN** 向只允许 `GET` 的公开 health 路由发送 `POST`
- **THEN** 返回 405，且不转发到 core

#### Scenario: 公开路由 allowlist

- **WHEN** 请求 `GET /healthz`、`GET /readyz` 或 `GET /.well-known/*`
- **THEN** 允许匿名访问且不要求 JWT

#### Scenario: metrics 仅集群内

- **WHEN** 集群外请求 `GET /metrics`
- **THEN** 拒绝访问，不暴露指标

### Requirement: 受保护路由的身份边界

`oa-edge` SHALL 在转发受保护路由前执行 `identity-edge` 契约：验证 Bearer JWT 的签名、`iss`、`aud`、`exp`，将 claims 映射为 `TenantContext`，清理客户端身份 header 并注入内部 `x-ontoaxis-*` header；缺失或无效凭据 SHALL 返回 401，租户不匹配或越权请求 SHALL 返回 403。伪造内部 header 的请求 MUST NOT 获得该 header 对应的身份：provider MAY 以 403 拒绝，MAY 在鉴权前剥离该 header 并以 JWT 派生身份继续。`oa-edge` MUST NOT 将未验证的身份字段转发给 core。

#### Scenario: 无令牌访问受保护路由

- **WHEN** 请求 `/api/v1/tenancy/organizations` 且没有 `Authorization`
- **THEN** 返回 401 `unauthenticated`，请求不转发到 core

#### Scenario: 客户端伪造身份 header

- **WHEN** 请求同时携带 `Authorization` 与客户端 `X-Tenant-Id`/`X-Ontoaxis-*` header
- **THEN** 客户端 header 被清理，仅使用 JWT 派生身份

#### Scenario: 租户不匹配

- **WHEN** 已验证主体的租户与请求目标租户不一致且主体不是允许切换的平台管理员
- **THEN** 返回 403 `permission_denied`，请求不转发到 core

#### Scenario: 公开路由不注入身份

- **WHEN** 匿名请求 `GET /healthz`
- **THEN** 不要求 JWT，且不注入 `x-ontoaxis-*` 身份 header

#### Scenario: 鉴权后端不可用时 fail-closed

- **WHEN** 受保护请求到达 edge，但 edge 使用的内部鉴权端点不可达或返回 5xx
- **THEN** 返回非 2xx（Traefik 当前为 500，Envoy extAuth 失败为 403），不将业务请求转发到 core

### Requirement: 入口限流

`oa-edge` SHALL 对受保护路由执行可配置的入口限流，按客户端与租户维度限制请求速率；超限请求 SHALL 返回 429；provider 支持时 SHALL 携带 `Retry-After`（Traefik 当前携带），Envoy 本地限流至少以 429 表示拒绝；公开健康与 well-known 路由 MUST NOT 被限流；限流不得静默放行超限请求。

#### Scenario: 超过客户端限额

- **WHEN** 同一客户端在窗口内超过配置的请求速率
- **THEN** 返回 429；Traefik 响应包含 `Retry-After`，Envoy 本地限流返回 429；超限请求不转发到 core

#### Scenario: 健康路由不受限流

- **WHEN** 健康检查以高于限流阈值的频率访问 `/healthz`
- **THEN** 健康检查仍返回 200

#### Scenario: 租户限额独立

- **WHEN** 租户 A 达到限额而租户 B 未达到
- **THEN** 租户 A 的请求返回 429，租户 B 的请求继续正常处理

### Requirement: Provider 一致性与独立部署

`oa-edge` SHALL 能以 Envoy Gateway（Kubernetes）与 Traefik（Compose）两种 provider 独立部署；两种 provider 对同一路由表 MUST 产生相同的公开/受保护边界、匹配结果和负向响应；部署 MUST 暴露可验证的健康/就绪状态，并支持在没有 core 直连入口时完成全部边缘验收。

#### Scenario: 两种 provider 的公开路由一致

- **WHEN** 分别通过 Envoy Gateway 和 Traefik 请求 `GET /healthz`
- **THEN** 两者都返回 200

#### Scenario: 两种 provider 的未知路由一致

- **WHEN** 分别通过 Envoy Gateway 和 Traefik 请求 `/api/v1/not-a-route`
- **THEN** 两者都返回 404，而不是默认后端页面或 core 404

#### Scenario: 两种 provider 的未认证响应一致

- **WHEN** 分别通过 Envoy Gateway 和 Traefik 请求受保护路由且不带 JWT
- **THEN** 两者都返回 401 `unauthenticated`

#### Scenario: 部署就绪

- **WHEN** `oa-edge` 的 Envoy Gateway 或 Traefik 完成启动
- **THEN** 其 readiness 检查通过，生成配置的 digest 与当前路由表一致
