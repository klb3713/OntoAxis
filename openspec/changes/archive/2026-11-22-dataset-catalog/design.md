# Design: dataset-catalog

## Context

Stage 1 需要一条能被 Query/Pipeline 复用的最小数据目录垂直切片。平台已提供 `module.Module`、`db.ExecStatements`、chi HTTP 服务器与统一错误包，但还没有任何业务模块。本设计只覆盖 `internal/modules/datasets/` 与对应 OpenSpec artifacts，不修改共享平台文件。

约束：

- `datasets` 模块只能依赖 `tenancy`（ARCHITECTURE 3.4.2）。
- 目录分层固定为 `module.go`、`api/`、`domain/`、`service/`、`repo/`、`migrations/`（ARCHITECTURE 3.4.3）。
- 表结构必须符合 ARCHITECTURE 5.1/5.2；数据集、分支、事务、视图、文件元数据都落在 `dataset` schema。
- 迁移必须通过 `db.ExecStatements` 执行幂等 DDL，测试不得依赖真实 Postgres。

## Goals / Non-Goals

**Goals**

- 交付可注册的 `datasets` 模块、可测试的领域/服务/仓储/HTTP 分层，以及可重复执行的 schema DDL。
- 用内存 fake store 与 `httptest` 覆盖成功、无效输入和关键拒绝路径。
- 为后续 Query/Pipeline 提供稳定的服务接口与路由前缀 `/api/v1/datasets`。

**Non-Goals**

- 不实现 Iceberg/Nessie/S3 客户端、数据预览、版本物化或权限策略执行。
- 不注册到 `cmd/`；模块集成由后续 change 或集成任务完成。
- 不提供真实 Postgres、对象存储或身份服务的联调证据。

## Decisions

### D1. 模块边界与依赖注入

`Module` 通过 `New(opts ...Option)` 构造，支持 `WithRepository` 与 `WithTenantResolver` 覆盖默认实现。`Name()` 返回 `datasets`，`Dependencies()` 返回 `tenancy`，`Migrate` 执行 schema DDL 并构建默认 `PostgresStore`，`RegisterRoutes` 挂载 `/api/v1/datasets`。

**替代方案**：在 `module.go` 内直接 `new` 所有依赖。放弃原因是无法在不启动 Postgres 的测试中注入 fake store。

**权衡**：默认构造路径依赖真实 pool；测试路径通过 `WithRepository` 注入内存 store，仍然覆盖 module 的路由注册逻辑。

### D2. 领域模型与状态机

`domain` 包只包含数据结构、枚举校验与事务状态迁移，不导入 pgx、chi 或任何平台包。事务状态迁移在领域方法 `Commit`/`Abort` 中执行：仅 `open` 可转为 `committed`/`aborted`，其余返回 `ErrInvalidState`。

**替代方案**：在 SQL `UPDATE ... WHERE status='open'` 中隐式判断。放弃原因是错误语义会泄漏到 API 层，且 fake store 无法共享同一规则。

**权衡**：领域层与仓储层都会检查状态；仓储层的条件更新用于防止并发竞争，领域层负责稳定的错误语义。

### D3. 迁移策略

`migrations/00001_dataset_init.sql` 内嵌为字符串，Go 侧按分号切分为语句列表后交给 `db.ExecStatements`。所有 `CREATE SCHEMA`/`CREATE TABLE` 使用 `IF NOT EXISTS`；RLS 使用 `DROP POLICY IF EXISTS` + `CREATE POLICY` 保证重复执行收敛。`dataset.dataset_files` 是 5.1 表清单之外新增的 S3 元数据表（`object_key`、`size_bytes`、`row_count`、`etag`、`content_type`）。

**替代方案**：使用 goose 的 `*.sql` + `*.down.sql`。放弃原因是 `module.Module.Migrate` 只收到 pool，平台未提供 per-module goose 实例；任务明确要求 `db.ExecStatements` 幂等 DDL。

**权衡**：模块迁移是 forward-only，没有 down 迁移；开发环境可 `DROP SCHEMA dataset CASCADE` 回退，生产回滚依赖新迁移或恢复演练，必须在集成门 G1 验证。幂等 DDL 不维护迁移台账，也不会修改已存在表的列；后续结构变更必须追加新的幂等语句，且无法回滚到旧结构。

### D4. 仓储与租户隔离

`service.Repository` 是接口，`repo.PostgresStore` 与 `repo.MemoryStore` 都实现它。Postgres 查询全部显式带 `tenant_id`，UUID 参数用 `$n::uuid`，查询结果将 UUID 转为文本，避免 pgx 的 UUID 扫描差异。每个 Postgres 操作都在一个租户事务内执行，并先调用 `SELECT set_config('app.tenant_id', $1, true)`，使 5.1.1 的 RLS 策略在同一事务内生效；事务结束时 `SET LOCAL` 自动清除。`MemoryStore` 用于单元测试，不引入数据库驱动。

**替代方案**：服务层直接使用 pgxpool。放弃原因是无法离线测试，也违反 api/service/domain/repo 分层。

**权衡**：仓储接口方法较多，但可按领域资源分组，避免 service 依赖具体 SQL。`datasets` 与 `dataset_views` 的唯一约束包含已软删除行，名称在租户/数据集内不可复用；`dataset_files` 的唯一索引只覆盖未删除行。该差异是刻意选择：数据集与视图名称属于稳定标识，文件对象 key 允许在删除后重新登记。若后续产品要求复用名称，需要新增迁移改为 partial unique index 并同步 `MemoryStore`。

### D5. HTTP 契约与租户解析

路由挂在 chi 的 `/api/v1/datasets` 下，JSON DTO 对齐 proto 字段命名（snake_case）。`TenantResolver` 接口默认实现只读取内部头 `X-Ontoaxis-Tenant-Id`；缺失或非法时返回 401，不落仓储。跨租户访问统一返回 404。服务错误映射到 `httperr`：`invalid_argument` 400、`unauthorized` 401、`already_exists` 409、`conflict` 409、`not_found` 404、`unavailable` 503、其他 500。

**替代方案**：直接读取 `X-Tenant-Id`。放弃原因是该头可能由外部客户端伪造，破坏 fail-closed 边界；identity-edge 集成时应注入认证后的 resolver。

**权衡**：当前 change 无法独立完成真实认证，只能在单测中模拟可信头；这属于集成风险，不是已实现的安全认证。

### D6. 分页

数据集、事务、视图与文件列表接口接受 `page`（默认 1）与 `per_page`（默认 50，最大 200）；分支列表按数据集整体返回，不分页。服务层返回 `Page{Page, PerPage, Total, TotalPages}`，仓储层执行 `LIMIT/OFFSET` 与独立 `COUNT(*)`。

**替代方案**：cursor 分页。放弃原因是当前数据量小、proto 也未定义 cursor；cursor 留待后续性能优化。

## Risks / Trade-offs

- [真实 Postgres 未验证] `Migrate` 只在测试中做语句结构检查，未在真实 Postgres 执行；G1 必须在本地 Postgres 执行迁移并检查 RLS。缓解：DDL 幂等、显式 `IF NOT EXISTS`，并保留集成任务。
- [认证未落地] 默认租户解析器信任内部头，若网关未剥离外部同名头会产生租户伪造风险。缓解：只接受 `X-Ontoaxis-Tenant-Id`，部署时由 identity-edge 覆盖并以集成测试验证；本 change 不声称完成认证。
- [事务并发] `Commit` 依赖仓储条件更新；fake store 使用互斥锁，Postgres 使用 `WHERE status='open'` 与单事务提交。并发测试只覆盖 fake store。
- [并发迁移] 幂等 DDL 由各实例启动时执行，未持有跨实例 advisory lock；多实例同时启动可能竞争 `CREATE TABLE IF NOT EXISTS`。当前按单实例启动假设；横向扩容前需在集成层增加迁移锁。
- [forward-only 迁移] 没有 down 迁移；生产回滚需要新迁移或恢复流程。缓解：在 G1 前完成迁移演练和备份确认。
- [软删除占用名称] 删除数据集或视图后同名资源无法重建；当前语义是保留名称。缓解：在 API 文档和集成测试中明确该行为；若产品要求复用，追加 partial unique index 迁移。

## Migration Plan

1. 在测试环境对空库执行 `Migrate`，确认 `dataset` schema、5.1 核心表、`dataset_files` 与 RLS 策略存在。
2. 重复执行 `Migrate`，确认无错误且对象数量不变（幂等）。
3. 注册模块前先由 `tenancy` 提供租户上下文；验证缺租户返回 401、跨租户返回 404。
4. 回滚：应用层取消模块注册即可停止路由；schema 回滚需按变更窗口执行 `DROP SCHEMA dataset CASCADE`（仅限非生产）或由后续迁移处理，生产回滚遵循 RUNBOOK 与负责人审批。

## Open Questions

- 真实 identity-edge 的租户注头名称与 resolver 注入点需在集成 change 中确认。
- `dataset_files` 是否需要在后续 change 中补充 `branch_id` 维度与对象存储校验，当前仅保存元数据。
