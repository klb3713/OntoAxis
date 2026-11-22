# Proposal: Object Graph

## Why

Stage 1 已批准，`ontology-definition` 提供 object type/property/link type 元数据，但对象实例与链接的持久化、分页与过滤能力尚缺失。`object-graph` 是 `query-surface` 与 `action-orchestration` 的上游依赖，也是前端 Object Explorer 的数据来源。该 change 以 PostgreSQL JSONB 提供对象/链接的最小可用垂直切片，并为后续 Meilisearch 索引边界保留 service 接口。

## What Changes

- 新增 `objects` 业务模块，实现对象（Object）与链接（Link）的创建、读取、更新、删除、分页列表与基础过滤。
- 在 `object_data` schema 新增 `object_data.objects`、`object_data.object_links` 表；对象 properties 使用 JSONB，链接带源/目标对象 ID 与唯一约束，均使用软删除。
- 在 `/api/v1/objects` 提供 REST API：对象 CRUD、`?object_type_id=` 过滤、`/{id}/links` 与链接 CRUD。
- 统一从可信租户上下文或 `X-Tenant-Id` 解析租户；缺失或非法 tenant 时拒绝请求。
- 以 fake store + `httptest` 覆盖成功、无效输入与租户缺失路径；真实 PostgreSQL 验证留给 `stage-1-core-integration`。

**Out of scope**

- 不实现 Meilisearch 真实索引、对象集合/视图、批量导入与对象类型校验（类型元数据由 `ontology` 模块负责）。
- 不实现 JWT 校验、OpenFGA/Cedar 授权或前端页面。
- 不修改 `go.mod`、`cmd/`、`internal/app/`、`internal/platform/`、`deploy/` 或其他模块目录。
- 不提供生产数据验收、归档或 Stage 退出。

## Capabilities

### New Capabilities

- `objects`: 对象实例与链接的 JSONB 持久化、分页/过滤、租户隔离与错误响应契约。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/modules/objects/`：`module.go`、`domain/object.go`、`repo/store.go`、`service/service.go`、`api/handler.go`、`migrations/20261122180000_objects_init.sql` 与测试。
- 新增 `/api/v1/objects` 路由；由 `stage-1-core-integration` 在总装时注册模块。
- 数据库新增 `object_data` schema 与 2 张表；不修改平台 `jobs` schema。
- 复用既有依赖 `github.com/go-chi/chi/v5`、`github.com/google/uuid`、`github.com/jackc/pgx/v5`，不新增第三方依赖。
