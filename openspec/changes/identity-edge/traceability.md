# Traceability

| Stage AC | Requirement / Scenario | Planned evidence | Status |
|---|---|---|---|
| S1-AC-02 | JWT 访问令牌校验（RS256、`iss`/`aud`/`exp`/`kid`、拒绝 alg=none/HS256、30 秒 skew、未知 kid 刷新） | `go test ./internal/platform/authn/...` 中 `TestJWKSVerifier*`（含 `TestJWKSVerifierValidToken`、`TestJWKSVerifierRejectsUnsupportedAlgorithms`、`TestJWKSVerifierClockSkew`、`TestJWKSVerifierUnknownKidRefreshesOnce`）；`internal/platform/authn/jwt_test.go` | 已验证（单元） |
| S1-AC-02 | Claims 到租户上下文映射与缺失/非法 `tenant_id` 返回 403 | `TestClaimsToTenantContext*`、`TestAuthenticateMissingTenant`、`TestAuthenticateInvalidTenant`；`claims_test.go`、`authenticator_test.go` | 已验证（单元） |
| S1-AC-02 | 客户端租户选择与 `platform-admin` 在 `org_ids` 内切换 | `TestMiddlewareTenantHeader*`、`TestTenantHeaderSelectionWithNilOrgIDs`；`authenticator_test.go` | 已验证（单元） |
| S1-AC-02 | 网关信任模式、Authorization/内部 header 混用拒绝、header 清洗防护、缺失租户 fail-closed | `TestMiddlewareGateway*`、`TestMiddlewareDirectRejectsInternalHeaders`、`TestMiddlewareDirectRejectsClientIdentityHeaders`；`authenticator_test.go` | 已验证（单元） |
| S1-AC-02 | 统一 401/403 `ErrorDetail`、`unauthenticated`/`permission_denied`、`request_id` 一致性、不泄漏用户存在性 | `TestWriteAuthError*`、`TestMiddlewareRequestIDFromTraceparent`、`TestMiddlewareDirectMissingToken`；`errors_test.go`、`authenticator_test.go` | 已验证（单元） |
| ARCH 6.3.2 | `TenantContext` 只能由 `authn.Middleware` 写入；`FromContext` 缺失返回 false；`RequireTenant` 返回 `unauthenticated` | `TestFromContextMissing`、`TestRequireTenantMissing`；`context_test.go` | 已验证（单元） |
| ARCH 16.2 / S1-AC-12（局部） | `config/edge/routes.yaml` 覆盖九个 `/api/v1/*` 前缀、公开路由 allowlist、`/metrics` 仅集群内、header 清洗/注入清单 | `TestEdgeRouteTable*` 解析 YAML 输出；`config/edge/routes.yaml`、`routes_test.go` | 已验证（单元） |

## 说明

- 本 change 的 Stage 归属为 `identity-edge`（change map 标注验收 S1-AC-02）；不把 Stage AC 编号写入 capability 名称。
- S1-AC-02 还包含“Keycloak OIDC 登录可用”和真实 Keycloak/JWKS 集成；本 change 明确只实现可替换的 JWT/claims/中间件契约与 fake verifier，**不接真实 Keycloak**。因此 S1-AC-02 整体不会被本 change 单独标记为完成，需后续环境授权和集成测试补齐。
- 所有 `已验证（单元）` 仅代表 `go test ./internal/platform/authn/...` 的单元契约证据，不代表真实认证、网关部署或 Stage 验收。真实 Keycloak/JWKS、`deploy/` 网关资源与 `/metrics` 网络策略仍未实现。
- 证据命令：`export PATH=/Users/klb3713/.local/share/go-toolchain/go/bin:$PATH && gofmt -l internal/platform/authn && go vet ./internal/platform/authn/... && go test ./internal/platform/authn/...`（2026-11-22，58 个测试通过；`gofmt -l` 无输出，`go vet` 无输出）。
