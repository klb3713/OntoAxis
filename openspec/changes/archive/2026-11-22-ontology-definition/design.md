# Design

## Context

见 `proposal.md` 的 Why。当前仓库已有 Go module、`platform.Module` 注册接口、`internal/platform/db.ExecStatements`、`internal/platform/httperr` 与 chi HTTP 基座，但没有任何业务模块。`internal/app` 在构造 HTTP server 之前按依赖顺序调用各模块的 `Migrate`，因此模块可以在 `Migrate` 阶段保存共享连接池，并在之后的 `RegisterRoutes` 阶段完成仓储与服务装配。

架构约束：`ontology` 只拥有 `ontology.object_types`、`ontology.properties`、`ontology.link_types`；Project/ProjectResource 由 `tenancy` 拥有；模块分层固定为 `module.go`、`api/`、`domain/`、`service/`、`repo/`、`migrations/`；路由前缀 `/api/v1/ontology`；所有租户表启用 RLS。

## Goals / Non-Goals

**Goals:**

- 用清晰的分层实现对象类型、属性、链接类型与数据集绑定的完整 CRUD/查询。
- 以可替换的 `Store` 接口隔离数据库，使服务层可用 fake store 完整测试。
- 以 `httptest` 验证 HTTP 契约、错误响应与租户拒绝路径，不依赖真实 PostgreSQL。
- 迁移可重复执行，并保留可回滚的 down 语句说明。
- 通过事务级 `app.tenant_id` 设置与 RLS 双层保证租户隔离。

**Non-Goals:**

- 不实现对象实例、真实数据集同步、异步任务系统或事件发布。
- 不实现认证/JWT、平台级租户解析或 API 网关；模块只消费已解析的租户上下文。
- 不修改模块注册接口、`internal/app` 装配或 `cmd/ontoaxis-core/main.go`；这些由集成 change 负责。
- 不实现 gRPC/gateway，本 change 提供 REST JSON 接口。

## Decisions

### 1. 分层与依赖方向

`domain` 定义实体、输入校验、分页与哨兵错误；`service` 定义 `Service` 接口及实现，只依赖 `domain` 与 `Store` 接口；`repo` 实现 PostgreSQL `Store`；`api` 只做 JSON 编解码、租户上下文提取和错误映射；`module.go` 只做生命周期装配。

备选：直接在 handler 中访问 `pgxpool`。放弃原因是无法用 fake store 覆盖业务规则，且违反架构 3.4.3 的分层约束。

### 2. 连接池注入方式

`module.Migrate(ctx, pool)` 执行迁移后把 `pool` 保存在模块实例中；`RegisterRoutes` 是后续生命周期阶段，此时按需构建 `PostgresStore → Service → Handler`。模块构造函数保持 `New()` 无参，与 `_template` 和现有 `app.New` 的调用方式兼容。

备选：修改 `module.Module` 接口或 `app.New` 以传入连接池。放弃原因是本 change 的写入范围不允许修改 `internal/platform` 与 `internal/app`，且会破坏其他 Wave 1 模块的并行开发。

### 3. Store 接口与 fake 测试

服务依赖窄接口 `Store`，仓储以租户 ID 为每个方法的显式参数，SQL 永远带 `tenant_id` 条件。测试用内存 fake store 覆盖成功、重复、缺失、跨租户与非法输入路径；不引入 mock 框架或新依赖。

备选：使用 sqlmock。放弃原因是需新增依赖，且不能验证服务层规则与 HTTP 契约。

### 4. 租户上下文

API 层从请求 context 读取模块定义的租户键；缺少或为空时立即返回 `unauthorized`，不提供默认 Principal、`X-Tenant-Id` 请求头或关闭开关。未来的 authn/tenancy 中间件通过 `api.WithTenantID` 注入解析后的租户标识；测试同样通过该辅助函数构造请求。

备选：读取请求头。放弃原因是客户端可伪造，违反 fail-closed 安全边界。

### 5. 主键与时间

主键由服务层生成 UUIDv7（`google/uuid` 已有依赖），数据库不依赖扩展；`created_at`/`updated_at` 由服务层生成并随插入/更新写入，数据库列保留 `now()` 默认值兜底。对象类型、属性、链接类型均使用 `deleted_at` 软删除。

备选：数据库 `gen_random_uuid()`。放弃原因是需要 pgcrypto 扩展且与架构“应用生成 UUIDv7”不一致。

### 6. 数据模型与迁移

三张表沿用架构 5.1.2 的列，并补充 proto 中的 `description`、`display_name` 与绑定所需列：

- `ontology.object_types`：`id`, `tenant_id`, `name`, `display_name`, `description`, `primary_key_property`, `backing_dataset_id`, `dataset_sync_mode`, `status`, `created_at`, `updated_at`, `deleted_at`；`UNIQUE(tenant_id, name)`；`status IN ('active','draft','deprecated')`。
- `ontology.properties`：`id`, `tenant_id`, `object_type_id`, `name`, `display_name`, `description`, `property_type`, `required`, `unique_constraint`, `config jsonb`, `created_at`, `updated_at`, `deleted_at`；`UNIQUE(tenant_id, object_type_id, name)`。
- `ontology.link_types`：`id`, `tenant_id`, `name`, `display_name`, `description`, `source_type_id`, `target_type_id`, `cardinality`, `created_at`, `updated_at`, `deleted_at`；`UNIQUE(tenant_id, name)`；`cardinality` 受 CHECK 约束。

迁移通过 `db.ExecStatements` 执行 `CREATE SCHEMA IF NOT EXISTS`、`CREATE TABLE IF NOT EXISTS`、`CREATE INDEX IF NOT EXISTS`、RLS 启用与策略重建，保证幂等。每张表启用 `ENABLE ROW LEVEL SECURITY` 与 `FORCE ROW LEVEL SECURITY`，策略使用 `tenant_id = current_setting('app.tenant_id', true)::uuid OR current_setting('app.is_platform_admin', true) = 'true'`。仓储在每次事务内执行 `set_config('app.tenant_id', $1, true)`。

备选：仅靠应用层 `WHERE tenant_id`。放弃原因是架构要求 FORCE RLS，双层防护可避免遗漏。

### 7. HTTP 契约

采用资源风格 REST JSON，字段使用 snake_case：

- 对象类型：`POST/GET /object-types`、`GET/PUT/DELETE /object-types/{id}`
- 属性：`POST /properties`、`GET /object-types/{objectTypeId}/properties`、`PUT/DELETE /properties/{id}`
- 链接类型：`POST/GET /link-types`、`PUT/DELETE /link-types/{id}`
- 数据集绑定：`POST/DELETE /object-types/{objectTypeId}/dataset-binding`、`POST /object-types/{objectTypeId}/dataset-binding/materialize`

对象类型与链接类型列表响应为 `{"items":[],"total":0,"page":1,"per_page":50}`，属性列表响应为 `{"items":[]}`；创建返回 `201`，删除返回 `204`，其余成功返回 `200`。错误统一使用 `internal/platform/httperr` 的 `{"error":{...}}` 结构：校验失败 `400`、缺少租户 `401`、不存在 `404`、重复或状态冲突 `409`。响应中的对象类型包含其属性列表。

备选：完全模拟 proto 的 gRPC-gateway JSON。放弃原因是仓库尚未生成 gateway 映射，手写映射会制造与冻结 proto 的隐性偏差；REST 路由只作为 Stage 1 的 HTTP 契约，proto 仍为长期接口真源。

### 8. 业务规则

- 名称规则：`^[a-z][a-z0-9_]{0,62}$`；显示名缺省回退为名称。
- 对象类型创建后状态为 `active`；状态暂不通过 API 修改。
- 属性 `property_type` 必须非空，具体类型集合由上层校验，本模块不限制枚举。
- 链接类型基数缺省 `many_to_many`，只接受 `one_to_one`、`one_to_many`、`many_to_many`。
- 删除对象类型前检查是否有未删除链接类型引用；有引用返回 `409`，需先删除链接类型。删除对象类型时级联软删除其属性。
- 绑定请求要求 `dataset_id` 为非空 UUID，`sync_mode` 缺省 `manual` 且需符合小写标识符规则。
- 物化只校验绑定存在并返回 UUIDv7 `operation_id`，不调用数据集模块、不写任务表。

备选：把全部业务规则下推为数据库约束。放弃原因是数据库约束无法覆盖跨表读取、软删除过滤和稳定的 `invalid_argument` 错误语义，且 fake store 测试无法覆盖。

### 9. 错误映射

`domain` 定义 `ErrNotFound`、`ErrAlreadyExists`、`ErrInvalidArgument`、`ErrFailedPrecondition`。API 将前者映射为 `not_found`/404、`already_exists`/409，`ErrInvalidArgument` 为 `invalid_argument`/400，`ErrFailedPrecondition` 为 `conflict`/409；未知错误为 `internal`/500。仓储把 `pgx.ErrNoRows` 与 PostgreSQL `23505` 唯一冲突翻译为对应哨兵错误。

备选：在 API 层直接识别 PostgreSQL 错误码。放弃原因是会泄漏存储实现、破坏分层，并让 fake store 测试与真实仓储行为不一致。

## Risks / Trade-offs

- [临时租户上下文接口] `api.WithTenantID` 是平台 authn 尚未落地前的过渡注入点 → 在 design 与 tasks 中登记集成要求：认证中间件必须在校验 JWT 后注入；在此之前所有请求 fail-closed 返回 401，不会匿名放行。
- [RLS 与连接池] `FORCE RLS` 下若仓储遗漏 `set_config` 会查询为空 → 所有仓储方法统一走 `withTenantTx`，并增加仓储层租户过滤的代码审查与集成测试前置条件。
- [软删除与唯一约束] `UNIQUE(tenant_id, name)` 不允许删除后复用同名 → 遵循架构约束；若未来需要复用，改为 `WHERE deleted_at IS NULL` 的部分唯一索引，需新 change。
- [物化仅受理] 当前返回的 `operation_id` 没有真实异步任务可追踪 → 响应消息明确“已受理”；接入 datasets/worker 的 change 必须替换为真实任务创建。
- [HTTP 与 proto 双轨] REST 字段是手写映射，可能与后续 gateway 生成物存在细微差异 → 接口测试固定 JSON 字段；后续引入 gateway 时以 proto 为准做一次契约对齐。
- [软删除级联] 对象类型删除时批量软删除属性未使用数据库触发器 → 服务层在单事务内完成；若并发创建属性，FK 仍指向已删除对象类型，后续查询按 `deleted_at` 隐藏。集成阶段需验证并发路径。

## Migration Plan

1. 部署包含本 change 的进程后，`ontology` 模块按注册顺序在 `tenancy` 之后执行 `Migrate`，创建 schema、表、索引与 RLS 策略；全部语句幂等，可重复执行。
2. 该 change 不修改已有表、不回填数据，无数据迁移。
3. 回滚：停止写入后执行 `migrations.DownStatements()` 中的 `DROP SCHEMA ontology CASCADE`；由于无其他模块引用这些表，回滚不影响平台基座与 tenancy 数据。
4. 真实环境执行前需由集成 change 完成模块注册与 JWT/租户中间件接线；本 change 的测试证据仅为单元与 HTTP 契约测试，不代表生产验收。

## Open Questions

无。租户中间件接线与物化执行属于后续 change 的明确范围，不影响本 change 的规格、方案或任务拆分。
