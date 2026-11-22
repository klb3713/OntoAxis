# Design: Tenancy Workspaces

## Context

参见 [`proposal.md`](proposal.md) 的 Why。当前仓库已由 `stage-1-platform-foundation` 提供 Go module、`platform.Module` 接口、`pgxpool` 连接与 `db.ExecStatements` 迁移助手，并已提供 `internal/platform/authn` 的 `TenantContext`/`FromContext` 读取契约；`proto/ontoaxis/v1/tenancy/tenancy.proto` 已冻结 Organization、Workspace、Membership、Project、ProjectResource 的字段与 RPC 形状，但生成代码尚未进入本 change 的写入范围。因此本 change 必须在“不修改共享文件、不新增依赖、不依赖真实 Postgres”的约束下交付一个可独立编译和测试的垂直切片。

## Goals / Non-Goals

**Goals:**

- 在 `internal/modules/tenancy` 内实现 `module.Module` 五方法，`Name()=="tenancy"`，`Dependencies()` 为空。
- 用 `pgxpool.Pool` 实现 5 类实体的持久化，表全部位于 `core` schema。
- 提供 `/api/v1/tenancy` 下的 REST 读写接口，字段命名与冻结 proto 对齐。
- 通过 `Store` 接口隔离持久化，使 service 与 API 测试完全使用 fake store。
- 所有错误经 `internal/platform/httperr` 输出统一错误信封。

**Non-Goals:**

- 不实现 JWT 校验、OpenFGA/Cedar 授权、Keycloak Organization 同步；这些属于 `identity-edge`。
- 不启用 RLS 策略、不实现 `db.WithTenant`；平台尚未提供请求级租户事务封装。
- 不生成 Connect/protobuf handler，不修改 `proto/`、`gen/`、`openapi/`。
- 不覆盖真实数据库集成、Compose 启动和跨模块 E2E；由 `stage-1-core-integration` 统一验证。

## Decisions

### D1. 单包分层文件布局

- **Decision**: 在 `internal/modules/tenancy/` 使用 `module.go`、`service.go`、`repo.go`、`api.go`、`migrations/*.sql` 的单包实现；`repo` 类型不导出，`api` 只依赖 `Service` 接口，`service` 不 import HTTP 或 pgx 类型。
- **Rationale**: 本 change 的写入范围明确限定这些文件；单包可避免为最小切片引入多子包和循环依赖，同时保持 `api → service → repo` 的单向调用边界，行为上符合 `ARCHITECTURE.md` 3.4.3 的边界规则。
- **Alternatives considered**:
  - 按 3.4.3 建 `api/domain/service/repo` 子包：目标结构更完整，但本 change 的交付清单与测试入口未要求，且会增加并行 change 的路径协调成本；后续可在不改变行为的前提下拆分。
  - handler 直接调用 repo：实现更快，但违反架构边界且无法用 fake store 做 API 测试。

### D2. 原生 pgx SQL，不引入 sqlc 生成

- **Decision**: `PostgresStore` 使用 `pgxpool.Pool` 执行参数化 SQL；所有查询显式带 `tenant_id` 条件与 `deleted_at IS NULL` 过滤。
- **Rationale**: 平台基座未提供 sqlc 生成链，本 change 又被禁止修改 `go.mod`/`go.sum`；手写 SQL 足以覆盖最小 CRUD，且便于审查租户过滤。
- **Alternatives considered**:
  - sqlc：类型安全更好，但需要新增工具链与生成文件，超出本 change 写入范围。
  - ORM：与 `ARCHITECTURE.md` 5.1 “不引入 ORM”冲突。

### D3. REST/JSON 适配层，字段对齐冻结 proto

- **Decision**: `api.go` 用 `chi` + `net/http` 暴露 REST 接口；JSON 字段使用 `snake_case`，与 `proto/ontoaxis/v1/tenancy/tenancy.proto` 的字段名对齐；列表返回 `{"items": [...], "page": {page, per_page, total, total_pages}}`，分页参数采用 proto `PageRequest` 的 `page`/`per_page`。
- **Rationale**: 本 change 不拥有 `gen/` 生成物和 Connect 依赖；REST 适配层可独立测试，并由后续集成 change 替换为生成的 Connect handler，而 service 契约不变。
- **Alternatives considered**:
  - 直接实现 Connect handler：需要 `gen/` 生成代码与依赖，且 `gen/` 不在写入范围，当前会阻塞编译。
  - 自定义响应形状：会增加前端/集成适配成本；对齐 proto 字段可减少后续切换成本。

### D4. 租户解析顺序：上下文优先，`X-Tenant-Id` 兜底

- **Decision**: `api.go` 优先读取 `internal/platform/authn.FromContext` 提供的 `TenantContext`；为单元测试与尚未安装认证中间件的装配场景，额外提供 `WithTenantID(ctx, uuid)`/`TenantIDFromContext(ctx)` 注入点；没有上下文租户时才读取 `X-Tenant-Id`。上下文与请求头同时存在且不一致返回 403，均缺失返回 401，请求头 UUID 非法返回 400。
- **Rationale**: `identity-edge` 会把认证后的 `TenantContext` 注入 context；直接复用 `authn.FromContext` 与 6.3.2 对齐，避免第二套身份模型。`WithTenantID` 只接受进程内代码注入，不是 HTTP 可伪造入口；header 兜底仅用于认证中间件尚未装配的本地/集成前阶段。
- **Alternatives considered**:
  - 只读 `X-Tenant-Id`：实现简单，但与 6.3.2 的 `authn.FromContext` 不兼容，未来还要改 handler，且无法表达请求头与可信身份冲突。
  - 只读 authn context：更严格，但 httptest 必须构造完整认证中间件；本 change 保留进程内测试注入点，同时生产路径优先 authn context。
  - 自定义独立租户上下文：会与平台 `authn.TenantContext` 重复，长期存在双真源风险；因此仅保留测试辅助函数并在生产解析时优先 authn。

### D5. 幂等迁移与显式租户过滤

- **Decision**: `migrations/20261122172700_tenancy_init_tables.sql` 使用 `CREATE SCHEMA IF NOT EXISTS core`、`CREATE TABLE IF NOT EXISTS core.*`、`CREATE INDEX IF NOT EXISTS`；`Module.Migrate` 读取嵌入的 Up 段并用 `db.ExecStatements` 逐条执行；同时提供 `.down.sql` 作为人工回滚脚本。repo 的每条 SQL 都显式带 `tenant_id`。
- **Rationale**: 平台基座约定模块迁移通过 `Module.Migrate(ctx, pool)` 执行且不依赖数据库 URL；幂等 DDL 可在重复启动和并行开发中安全执行。显式租户过滤保证在 RLS 尚未启用时仍不越租户。
- **Alternatives considered**:
  - 使用 goose 文件与 `db.MigrateFS`：需要 DSN，而 `Module.Migrate` 只拿到 pool；平台已提供 `ExecStatements`，按约定使用。
  - 依赖数据库 RLS：平台 `db.WithTenant` 尚未实现，启用 RLS 会导致未设置 `app.tenant_id` 的查询全部失败或返回空。

### D6. fake store 驱动的测试策略

- **Decision**: 定义 `Store` 接口；测试实现线程安全的内存 `fakeStore`，覆盖 service 校验、租户隔离与 API 状态码/错误信封；`Module.Ready` 在 pool 为 nil 时返回错误。
- **Rationale**: 满足“不依赖真实 Postgres”的验收约束，测试可在任意 CI 环境快速运行；同时通过 `httptest` 验证真实 chi 路由与 JSON 编解码。
- **Alternatives considered**:
  - testcontainers/真实 PostgreSQL：更接近生产，但本 change 明确不要求，且本地 Docker 不可假设可用。

### D7. 领域错误到 HTTP 错误码的映射

- **Decision**: `service.go` 定义 `ErrNotFound`、`ErrConflict`、`ErrInvalidArgument` 哨兵；`api.go` 使用 `errors.Is` 映射为 `httperr.NotFound`、`httperr.Conflict`、`httperr.BadRequest`，其余返回 `httperr.Internal()`。
- **Rationale**: 避免 handler 依赖数据库错误文本；保持错误码稳定且不泄漏 SQL。
- **Alternatives considered**:
  - 在 repo 直接写 HTTP 错误：破坏分层且无法复用 service。

## Risks / Trade-offs

- [与 `ARCHITECTURE.md` 3.4.3 的子包结构存在差异] → 本 change 保持包内单向调用边界；后续拆分 `api/domain/service/repo` 子包不改变行为，可由集成或重构 change 完成。
- [REST 适配层与最终 Connect 契约存在切换成本] → 字段名与分页形状对齐冻结 proto；集成 change 替换 handler 时只改协议适配，不改 `Service`/`Store`。
- [RLS 未启用，租户隔离依赖应用层过滤] → 所有 repo 查询强制 `tenant_id` 参数，并在 service 层校验租户；上线前由平台 change 补 RLS 与 `db.WithTenant`，本风险在 traceability 中保留。
- [真实 Postgres 未验证，SQL 可能有方言/约束问题] → 迁移只使用 PostgreSQL 标准 DDL 与显式类型；`go vet`/单元测试覆盖逻辑；真实迁移验证由 G1 集成门补跑。
- [并发创建可能触发唯一约束] → repo 将唯一冲突识别为 `ErrConflict`；service 返回 409，不返回 500。

## Migration Plan

1. 模块启动时 `Module.Migrate` 执行 `CREATE SCHEMA IF NOT EXISTS core` 与 5 张表、索引的幂等 DDL。
2. 本 change 只新增表与索引，不修改既有表；回滚执行 `migrations/20261122172700_tenancy_init_tables.down.sql` 删除新表（新环境无数据损失）。
3. 由 `stage-1-core-integration` 在模块注册时调用 `tenancy.New(pool)`，并把 `/api/v1/tenancy` 纳入网关路由表。
4. 若需回退：移除模块注册并执行 down 脚本；不影响平台 `jobs` schema 或其他模块。

## Open Questions

- 无阻塞开放问题。RLS 策略、JWT 租户声明映射与 Connect handler 切换已明确列为后续集成依赖，不影响本 change 的 specs 与任务拆分。
