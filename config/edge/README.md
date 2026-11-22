# config/edge

`routes.yaml` 是 `oa-edge` 的 YAML 路由表骨架，也是九个业务模块路径前缀的唯一声明来源。当前 change（`identity-edge`）只交付路由表与校验测试，不生成或部署 Envoy Gateway/Traefik 资源。

## 契约

- 所有 `/api/v1/**` 路由默认 `auth: jwt`，网关必须先完成 JWT 校验，再注入 `x-ontoaxis-*` 内部 header。
- 公开路由仅允许 `GET /healthz`、`GET /readyz`、`GET /.well-known/*`。
- `/metrics` 只能从集群内访问，不暴露到公网监听器。
- 网关必须删除客户端传入的 `X-Tenant-Id`、`X-User-Id`、`X-Org-Ids`、`X-Workspace-Ids`、`X-Roles`，不得直接透传。
- 内部 header 只能由网关在验证 JWT 后注入；`oa-core` 在 `ONTOAXIS_TRUST_GATEWAY=true` 时读取，在直连模式下拒绝任何内部 header。
- 新增业务前缀时，同步更新本文件与 `internal/platform/authn/routes_test.go` 的断言；不要让网关配置成为无测试的隐式真源。

## 校验

```bash
export PATH=/Users/klb3713/.local/share/go-toolchain/go/bin:$PATH
go test ./internal/platform/authn/... -run 'TestEdgeRouteTable' -v
```

## 后续

- Envoy Gateway/Traefik 具体资源、TLS、限流、`/metrics` 网络策略和发布流程由 `deploy/` 的 owner 在后续 change 中实现。
- 本文件不声明真实 Keycloak realm、JWKS URL 或生产域名；这些属于环境配置，不得写入仓库秘密。
