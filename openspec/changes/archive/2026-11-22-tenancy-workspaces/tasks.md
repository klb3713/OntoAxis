# Tasks: Tenancy Workspaces

## 1. OpenSpec 规划

- [x] 1.1 阅读 `ARCHITECTURE.md` 3.4/5.1/6.3/16.2/17、Stage 1 brief、change map 与 `openspec instructions`，确认 tenancy 模块边界与 artifact 要求。验证：设计决策与 change map 的模块归属一致（`core` schema、`/api/v1/tenancy`、无模块依赖）。
- [x] 1.2 完成 proposal、delta spec、design 并在实现前通过结构校验。验证：`openspec validate tenancy-workspaces --strict` 无错误。

## 2. 领域模型与持久化

- [x] 2.1 在 `internal/modules/tenancy/service.go` 定义领域类型（Organization、Workspace、Membership、Project、ProjectResource）、输入类型、分页与 `ErrNotFound`/`ErrConflict`/`ErrInvalidArgument` 哨兵。验证：`go build ./internal/modules/tenancy/...` 通过。
- [x] 2.2 在 `internal/modules/tenancy/service.go` 定义 `Store` 接口，在 `repo.go` 实现 `PostgresStore`，所有 SQL 参数化并显式带 `tenant_id` 与软删除过滤。验证：`go vet ./internal/modules/tenancy/...` 通过；SQL 文本检查无字符串拼接用户输入。
- [x] 2.3 编写 `migrations/20261122172700_tenancy_init_tables.sql`（Up）与 `.down.sql`，覆盖 `core` schema 与 5 张表、索引、唯一约束、CHECK。验证：DDL 使用 `CREATE SCHEMA IF NOT EXISTS` 与 `CREATE TABLE IF NOT EXISTS`，重复执行不报错（由 `Module.Migrate` 单元测试覆盖语句数）。

## 3. Service 层

- [x] 3.1 实现 Organization/Workspace/Membership/Project/ProjectResource 的创建、读取、更新、列表、删除/解绑业务方法；ID 使用 UUIDv7，创建/更新时间由 service 填充。验证：fake store 单元测试覆盖每个方法的成功路径。
- [x] 3.2 实现输入校验与租户边界校验：名称/slug 非空、主体类型与资源类型枚举、成员状态枚举、跨租户组织/工作区/项目引用拒绝。验证：表驱动单元测试覆盖非法枚举与跨租户引用。
- [x] 3.3 将 store 返回的唯一约束/外键冲突转换为 `ErrConflict`，不存在转换为 `ErrNotFound`。验证：fake store 注入错误后 service 返回对应哨兵。

## 4. HTTP API 与模块装配

- [x] 4.1 在 `module.go` 实现 `module.Module`：`Name()=="tenancy"`、`Dependencies()==nil`、`Migrate` 通过 `db.ExecStatements` 执行嵌入 DDL、`Ready` 检查 pool 并 Ping、`RegisterRoutes` 挂载 `/api/v1/tenancy`。验证：单元测试断言 Name/Dependencies 与迁移语句数；`Ready(nil pool)` 返回错误。
- [x] 4.2 在 `api.go` 实现租户解析：context 优先、`X-Tenant-Id` 兜底，缺失 401、非法 400、冲突 403；实现 `errors.Is` 到 `httperr` 的映射与统一错误信封。验证：`httptest` 覆盖 401/400/403 与 404/409/500 响应体字段。
- [x] 4.3 实现 organizations、workspaces、memberships、projects 的 GET/POST/PATCH/DELETE 与 `projects/{id}/resources` 的 GET/POST/DELETE 路由，JSON 字段与冻结 proto 对齐。验证：`httptest` 按路由表逐项断言状态码与响应 JSON。
- [x] 4.4 实现 `page`/`per_page` 分页与 `{items,page}` 响应；限制 `per_page` 上限。验证：httptest 断言分页元数据与非法分页参数返回 400。

## 5. 测试与验证

- [x] 5.1 编写 `service_test.go` 与 `repo_test.go`（不依赖真实 Postgres；repo 层测试仅验证构造与 SQL 常量/迁移嵌入）。验证：`go test ./internal/modules/tenancy/...` 通过。
- [x] 5.2 编写 `api_test.go` 使用 fake store + `httptest`，覆盖每类资源成功、无效输入、404/409 和租户隔离（租户 A 数据对租户 B 不可见）。验证：测试输出显示对应子测试 PASS。
- [x] 5.3 对所有 Go 文件执行 `gofmt -l`（输出为空）并运行 `go vet ./internal/modules/tenancy/...`。验证：命令退出码为 0。
- [x] 5.4 回填 `traceability.md` 的证据路径与实测状态（由 unverified 改为 verified/pending，并附命令）。验证：文档中的证据路径真实存在，命令输出与记录一致。
- [x] 5.5 运行 `openspec status --change tenancy-workspaces` 与 `openspec validate --all --strict`，记录结果到最终报告。验证：命令退出码为 0 或如实记录阻塞项。

## Workflow follow-up

- `stage-1-core-integration` 负责在 `cmd/ontoaxis-core`/`internal/app` 注册 `tenancy.New(pool)` 并把 `/api/v1/tenancy` 纳入网关路由。
- `identity-edge` 负责 JWT/Keycloak 租户声明注入 `TenantContext`；本 change 的 context 读取点需与其 `authn.FromContext` 对齐。
- 平台后续 change 负责 `db.WithTenant` 与 RLS 策略；本 change 的 repo 显式租户过滤在 RLS 启用后可保留为第二道防线。
- 集成阶段需对真实 PostgreSQL 执行本 change 的迁移并补至少一个 E2E fixture（本 change 不声称完成真实数据库验证）。
