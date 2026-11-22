# Design: Object Graph

## Context

`ontology-definition` 已经拥有 object type/property/link type 元数据；本 change 只负责实例数据。对象 properties 是自由 JSONB，类型校验、必填字段和链接端点约束由 ontology 元数据定义，Stage 1 不做跨模块实时校验。

## Decisions

### D1. 单模块分层

`internal/modules/objects/` 固定为 `module.go`、`domain/`、`repo/`、`service/`、`api/`、`migrations/`。`api/` 只做 HTTP↔domain 映射；`service/` 不暴露 protobuf 类型；`repo/` 只访问 `object_data` schema；跨模块调用通过 `ontology.Service` 接口（集成阶段注入）。

### D2. Schema 与 ID

`object_data.objects`、`object_data.object_links` 均带 `tenant_id`、`created_at/updated_at/deleted_at`。对象与链接 ID 使用 UUIDv7；链接唯一约束为 `(tenant_id, link_type_id, source_id, target_id)`，软删除不参与唯一冲突（`deleted_at IS NULL` 部分唯一索引）。

### D3. JSONB 与索引

`properties` 使用 `jsonb`，创建 `GIN (properties jsonb_path_ops)`；对象按 `(tenant_id, object_type_id, created_at DESC)` 建索引；链接按 `(tenant_id, source_id)`/`(tenant_id, target_id)` 建索引。Stage 1 不实现全文检索，Meilisearch 仅保留 service 边界。

### D4. 租户隔离

服务从 `authn.FromContext` 读取 `TenantContext`，测试/过渡期允许 `X-Tenant-Id`。所有 repo SQL 显式带 `tenant_id`；Stage 1 不启用 RLS（RLS 由平台 `db.WithTenant` 在集成 change 补齐）。

### D5. 错误与 HTTP 契约

使用 `internal/platform/httperr` 统一信封；`ErrNotFound` → 404、`ErrInvalidInput` → 400、重复链接 → 409。对象列表返回 `{items,total}`，按 `created_at DESC` 排序。

## Risks

- 真实 PostgreSQL 唯一约束/软删除行为未在本 change 集成验证。
- `object_type_id` 存在性不在此模块校验；集成阶段需通过 ontology service 或数据库外键策略确认。
- Meilisearch 索引尚未接入，查询只能走 PostgreSQL JSONB。
