# Proposal: Tenancy Workspaces

## Why

Stage 1 已批准，平台基座（`stage-1-platform-foundation`）已经提供可编译的 Go module、模块注册接口、数据库连接与迁移入口；但组织、工作区、成员关系、项目与项目资源的租户级数据模型和 API 仍缺失。`tenancy` 是 `ARCHITECTURE.md` 3.4.2 依赖图的根节点，`ontology`、`datasets`、`connectors` 等后续模块都依赖它提供租户与项目边界；若不先交付该能力，Wave 1 的其他模块无法确定项目归属与租户隔离口径。

## What Changes

- 新增 `tenancy` 业务模块，实现 Organization、Workspace、Membership、Project、ProjectResource 的最小 CRUD 与资源绑定能力。
- 在 `core` schema 新增 `core.organizations`、`core.workspaces`、`core.memberships`、`core.projects`、`core.project_resources` 表，所有租户级表带 `tenant_id`，迁移使用幂等 DDL。
- 在 `/api/v1/tenancy` 前缀下提供 REST API：organizations、workspaces、memberships、projects 的 GET/POST/PATCH/DELETE，以及 `projects/{id}/resources` 的 GET/POST/DELETE。
- 统一从可信租户上下文或 `X-Tenant-Id` 解析租户；缺失、非法或两者冲突时按 `internal/platform/httperr` 错误模型拒绝。
- 以 fake store + `httptest` 覆盖成功、无效输入与租户隔离拒绝路径，不依赖真实 PostgreSQL。

**Out of scope**

- 不实现 JWT 校验、Keycloak 同步或 OpenFGA/Cedar 授权（由 `identity-edge` change 负责）；本 change 只从 `internal/platform/authn` 已提供的可信 `TenantContext` 或测试用上下文读取点取租户，不重复实现认证中间件。
- 不实现前端页面、其他业务模块的 project 引用与跨模块调用。
- 不启用 PostgreSQL Row-Level Security 策略与租户连接会话变量（当前平台尚未提供 `db.WithTenant`，留待平台/集成 change 统一实现）；本 change 的 repo 查询显式带 `tenant_id` 条件。
- 不提供真实 PostgreSQL 集成测试、生产数据验收、归档或 Stage 退出。

## Capabilities

### New Capabilities

- `tenancy`: 组织、工作区、成员关系、项目与项目资源绑定的租户级行为契约，包括租户隔离与错误响应要求。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/modules/tenancy/`：`module.go`、`service.go`、`repo.go`、`api.go`、`migrations/*.sql` 与测试。
- 新增 `/api/v1/tenancy` 路由；由 `stage-1-core-integration` 在总装时把模块注册进 `cmd/ontoaxis-core`/`internal/app`。
- 数据库新增 `core` schema 与 5 张租户级表；不修改平台既有 `jobs` schema。
- 不新增第三方依赖：复用 `github.com/go-chi/chi/v5`、`github.com/google/uuid`、`github.com/jackc/pgx/v5`（均已在 `go.mod`）。
- 不修改 `go.mod`/`go.sum`、`cmd/`、`internal/app/`、`internal/platform/`、`deploy/` 或其他模块目录。
