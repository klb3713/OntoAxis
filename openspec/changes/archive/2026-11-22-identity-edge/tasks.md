# Tasks

## 1. 上下文与错误契约

- [x] 1.1 实现 `internal/platform/authn/context.go`、`claims.go`、`doc.go`：`TenantContext` 精确字段、`FromContext`/`RequireTenant`、`Claims` 结构、已知角色集合、UUID 与角色解析；编写测试覆盖缺失上下文 fail-closed 与角色过滤。
- [x] 1.2 实现 `internal/platform/authn/errors.go`：`ErrorDetail` 统一信封、401 `unauthenticated` / 403 `permission_denied` 写入器、`X-Request-Id` 响应头与 body `request_id` 一致；编写响应形状测试。

## 2. JWT 校验

- [x] 2.1 实现 `internal/platform/authn/keyset.go`、`fake.go`：`KeySet`/`KeySource` 接口、`StaticKeySet`/`StaticKeySource`、`FakeVerifier`；测试 fake verifier 可注入合法/非法 claims。
- [x] 2.2 实现 `internal/platform/authn/jwt.go`：compact JWS 解析、RS256 验签、`iss`/`aud`/`exp`/`kid` 校验、30 秒 clock skew、未知 `kid` 强制刷新一次；测试覆盖 `alg=none`、HS256、篡改签名、错误 iss/aud、过期超限、未知 kid、key 轮换。
- [x] 2.3 实现 claims → `TenantContext` 映射（`sub`/`tenant_id`/`org_ids`/`workspace_ids`/`roles`/`email`）：缺失或非法 `tenant_id` 返回 403；测试覆盖完整映射、缺失/非法租户与未知角色。

## 3. 认证中间件

- [x] 3.1 实现 `internal/platform/authn/authenticator.go` 与 `middleware.go` 直连模式：从 `Authorization: Bearer` 解析、无/无效 token 返回 401、`X-Tenant-Id` 一致性与 `platform-admin` 跨租户选择；编写表驱动测试。
- [x] 3.2 实现网关信任模式：仅读 `x-ontoaxis-*` 内部 header、缺失/非法 UUID 拒绝、Authorization 与内部 header 混用拒绝、直连模式内部 header 拒绝、`x-ontoaxis-roles` 过滤/去重/排序；编写负向测试。
- [x] 3.3 实现 `request_id` 派生（`observability.RequestID` → `X-Request-Id` → W3C `traceparent`）与 401/403 响应头/响应体一致性；编写测试。

## 4. 边缘路由表

- [x] 4.1 创建 `config/edge/routes.yaml` 与 `config/edge/README.md`：九个业务前缀、模块所有权、公开路由 allowlist、`/metrics` 仅集群内、客户端 header 清洗与内部 header 注入清单。
- [x] 4.2 实现 `internal/platform/authn/routes_test.go`：解析 YAML 并断言九个前缀映射、无重复、公开路由仅三条、`/metrics` `internalOnly: true`；`go test ./internal/platform/authn/...` 通过。

## 5. 质量门与证据

- [x] 5.1 运行 `gofmt -l internal/platform/authn`（必须空输出）、`go vet ./internal/platform/authn/...`、`go test ./internal/platform/authn/...`，记录命令与输出。
- [x] 5.2 运行 `go test ./...` 与 `openspec validate --all --strict`，记录结果；核对 tasks/traceability 与实际证据一致，更新 `traceability.md` 状态。
- [x] 5.3 复核 `git status --short` 与 `git diff --stat`：只允许 `openspec/changes/identity-edge/`、`internal/platform/authn/`、`config/edge/` 的变更；不 commit；将文件清单、测试结果、真实 Keycloak/JWKS 依赖风险写入交付说明。

## Workflow follow-up

- 实现与验证完成后提交 change 评审并请求项目负责人确认。
- 评审通过后才可运行归档与 Stage 级验证；本 change 完成不等于 Stage 1 完成，不得自动归档或宣布 Stage 退出。

## 验证记录（2026-11-22）

- `gofmt -l internal/platform/authn`：无输出。
- `go vet ./internal/platform/authn/...`：通过，无输出。
- `go test -count=1 ./internal/platform/authn/...`：`58 passed in 1 packages`。
- `openspec validate --all --strict`：`change/identity-edge` 通过（4/4 artifacts complete）；全仓另有 5 个与本次无关的 change 报 `No deltas found`（action-orchestration、frontend-core、object-graph、query-surface、stage-1-core-integration），未修改。
- `go test ./...`（最终复核）：228 passed，2 failed；失败点为 `internal/modules/pipelines` 与 `internal/modules/pipelines/repo` 构建失败（`internal/modules/pipelines/repo/postgres.go:330:17: undefined: json`）。该失败超出本 change 写入范围，未修复，作为集成风险上报；工作树含其他并行 change 的变更，失败包可能随并行工作变化。
- 范围复核：本次新增/修改仅位于 `openspec/changes/identity-edge/`、`internal/platform/authn/`、`config/edge/`；本次未主动修改 `go.mod`/`go.sum`、`cmd/`、`internal/app/`、`internal/platform/httpserver/`、`deploy/`；未 commit。工作树中的 `go.mod`/`go.sum` 与其他目录变更来自并行 change，不能用全局 `git status` 归因。
- 依赖风险：未新增第三方 JWT 库；当前 RS256 校验为 `crypto/rsa` + `crypto/sha256` 的最小实现，真实 JWKS HTTP 客户端/缓存、真实 Keycloak 登录与网关资源仍待后续授权与 change。
