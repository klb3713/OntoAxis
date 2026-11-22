# config/edge

`routes.yaml` 是 `oa-edge` 的唯一路由声明来源，也是九个业务模块路径前缀的唯一真源。它描述 Envoy Gateway（K8s）与 Traefik（Compose）共同消费的路由契约；`tools/edgegen` 从该文件生成 `deploy/edge/generated/` 下的 provider 产物。

## 文件

| 文件 | 作用 |
| --- | --- |
| `routes.yaml` | 唯一路由表：监听器、公开/内部路由、JWT 策略、限流、上游、9 个业务前缀；不包含环境域名、TLS 证书或密钥 |
| `routes.schema.json` | 路由表的 JSON Schema（草案 2020-12），供生成器与 CI 校验 |
| `README.md` | 匹配顺序、公开/受保护边界、限流、生成规则和变更流程 |

## 版本

- `apiVersion: ontoaxis.edge/v1`、`kind: EdgeRouteTable`，`metadata.contractVersion` 随不兼容变更递增。
- 当前契约版本：`1`；变更需同时更新 schema、Go 契约测试和本文档。

## 匹配顺序

网关必须按以下顺序处理请求，任何步骤失败都不得继续匹配：

1. **协议与监听器**：`listener.protocol` 决定是否要求 TLS；未启用的 TLS 不得接受 HTTPS；`/metrics` 只允许在集群/私网监听器匹配。
2. **Header 清理**：在转发前删除 `clientHeadersToStrip` 中的客户端 header；这些 header 永远不能来自外部。
3. **公开路由**：按 `publicRoutes` 的方法 + 精确 path/前缀匹配（`/.well-known/*` 为前缀）。命中后不校验 JWT，也不注入任何 `x-ontoaxis-*` 身份 header。
4. **内部路由**：匹配 `internalRoutes`；仅当请求来自集群/私网监听器时允许，否则返回 `404`（不暴露内部路由存在性）。
5. **受保护路由**：按 `routes[].prefix` **最长前缀优先**匹配；长度相同时按 `priority` 降序、再按 `name` 升序；同一前缀重复视为配置错误，启动/生成时拒绝。
   - 超过 `rateLimit.defaults` 返回 `429` + `Retry-After`；`publicRoutes` 按 `rateLimit.exemptPublicRoutes` 豁免。
   - `rateLimit.byTenant` 是单实例 best-effort 租户配额，不作为跨副本精确限额。
6. **JWT 校验**：受保护路由先校验签名、`iss`/`aud`/`exp`/`kid` 与 30 秒时钟偏移；失败返回 `401` 并带 `WWW-Authenticate: Bearer`。缺少必要 claim 视作无效 token。
7. **方法匹配**：请求方法不在该路由的 `methods` 中返回 `405`，不得转发。
8. **注入内部 header**：仅在 JWT 成功后注入 `internalHeadersToInject`；不得透传客户端提供的同名 header。
9. **转发**：把原始 path 与 query 转发到 `upstream`；不在网关做业务路径重写。未匹配任何路由返回 `404`，禁止 catch-all 转发。
10. **审计/日志**：记录 method、匹配到的 route name、status、request id；不记录 Authorization、token、原始对象属性或查询字符串。

## 公开与受保护路由

- **公开**：仅 `GET /healthz`、`GET /readyz`、`GET /.well-known/*`。公开路由不注入身份 header，也不允许访问业务模块。
- **内部**：仅 `GET /metrics`，标记 `exposure: cluster-only`；不得出现在公网监听器或外部 `HTTPRoute`/Ingress 中。
- **受保护**：所有 `/api/v1/**` 业务前缀均为 `auth: jwt`。当前 9 个前缀为 tenancy、ontology、datasets、connectors、ingestion、pipelines、objects、query、actions。
- **默认拒绝**：未匹配的路径、方法、协议一律拒绝；禁止默认转发到 core。

## JWT 与身份边界

- JWT 使用 `Authorization: Bearer <token>`；`auth.issuerEnv`/`audienceEnv`/`jwksURLEnv` 只引用环境变量名，不提交 issuer URL、JWKS URL、域名或密钥。
- `oa-edge` 校验 JWT 后注入 `x-ontoaxis-tenant-id`、`x-ontoaxis-user-id`、`x-ontoaxis-org-ids`、`x-ontoaxis-workspace-ids`、`x-ontoaxis-roles`、`x-ontoaxis-request-id`。
- `ontoaxis-core` 在 `ONTOAXIS_TRUST_GATEWAY=true` 时读取这些 header；直连模式下必须拒绝任何内部 header。
- 客户端传入的 `X-Tenant-Id`、`X-User-Id`、`X-Org-Ids`、`X-Workspace-Ids`、`X-Roles` 必须先删除；不得靠 core 侧“忽略”来兜底。

## 生成规则

`routes.yaml` 不是运行时动态配置，而是生成器的输入。生成器为 `tools/edgegen`，`make edge-generate` 生成、`make edge-generate-check` 只校验不写入。生成器必须满足：

1. **单一来源**：Envoy Gateway 的 `HTTPRoute` 和 Traefik 的动态配置都由同一份 `routes.yaml` 生成；禁止在 `deploy/` 手写第二份路由清单。
2. **确定性**：输出顺序固定为 `publicRoutes` 声明顺序 → 受保护路由按 prefix 长度降序 → 同长度按 name 升序；相同输入必须产生字节级相同输出。
3. **来源可追溯**：每个生成文件写入 `generation.digestAnnotation`（值为 `routes.yaml` 的 SHA-256）和 provider 标识；生成器不得修改路由顺序或隐式添加 catch-all。
4. **环境无关**：生成结果不包含域名、TLS 证书、集群名称或密钥；这些只来自 Helm values/Compose `.env`。`upstream.service`/`port` 是集群内服务标识，不是公网地址。
5. **生成检查**：`generation.checks` 中的命令必须在 CI 运行；生成物与源不一致时失败（`make edge-generate-check`）。
6. **变更流程**：新增前缀/方法时同时更新 `routes.yaml`、`routes.schema.json`、`internal/platform/authn/routes_test.go`、`ARCHITECTURE.md` 15.3；不得只改网关或只改 core。

生成目标：

| Provider | 目标文件 | 资源 |
| --- | --- | --- |
| `envoy-gateway` | `deploy/edge/generated/envoy-httproute.yaml` | `Gateway`、`HTTPRoute`、`SecurityPolicy`、`BackendTrafficPolicy`、`HTTPRouteFilter` |
| `traefik` | `deploy/edge/generated/traefik-dynamic.yaml` | Traefik dynamic `http` routers/services/middlewares |

## 校验

```bash
go test ./internal/platform/authn/... -run 'TestEdgeRouteTable' -v
```

`routes.schema.json` 是生成器和 CI 的机器可读契约；Go 测试负责验证语义约束（前缀唯一、顺序、公开集合、header 列表）。JSON Schema 的 `additionalProperties: false` 用于阻止未声明字段悄悄进入契约。

## Provider 差异

- **Envoy Gateway**：使用 `SecurityPolicy.extAuth.http` 调用 core 的 `/internal/edge-auth`，由 core 统一做 JWT 校验并返回 `x-ontoaxis-*`；不启用原生 JWT claimToHeaders，避免数组 claim 映射限制。
- **Traefik**：使用 `forwardAuth` 指向同一 core 端点，`authResponseHeaders` 注入身份头；不依赖第三方 JWT 插件。
- 两种 provider 都必须剥离客户端身份头、保证鉴权端点不可达时 fail-closed（503），并用同一份 `routes.yaml` 生成。

## 后续

- TLS、网络策略、跨副本租户限流与生产发布流程由后续 Stage 2/Stage 3 change 实现。
- 本目录不声明真实 Keycloak realm、JWKS URL 或生产域名；环境配置不得写入仓库秘密。
