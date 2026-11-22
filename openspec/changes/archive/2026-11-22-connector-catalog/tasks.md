# Tasks

## 1. OpenSpec 规划产物

- [x] 1.1 完成 `proposal.md` 与 `specs/connectors/spec.md`、`specs/ingestion/spec.md`，验证：三个文件存在，且 `proposal.md` 的 Capabilities 与两个 spec 目录一一对应。
- [x] 1.2 完成 `design.md`、`tasks.md`、`traceability.md`，验证：design 覆盖分层、迁移、租户与适配边界，traceability 覆盖 Stage 1 对应验收项且初始状态为 unverified。

## 2. Connectors 模块

- [x] 2.1 实现 `internal/modules/connectors/domain`：Connector 类型/状态枚举、值校验、敏感配置递归检查与领域错误；验证：`go test ./internal/modules/connectors/domain` 覆盖成功、非法类型、非法状态、嵌套敏感键。
- [x] 2.2 实现 `internal/modules/connectors/service`：`Service` 公开接口、`Store` 端口、CRUD/Test/StartSync/InferSchema 用例与 UUIDv7 生成；验证：fake store 单测覆盖成功、重名、跨租户、适配器缺失。
- [x] 2.3 实现 `internal/modules/connectors/repo`：pgx 查询、租户过滤、软删除过滤、唯一冲突映射；验证：`go build ./internal/modules/connectors/...` 编译通过，SQL 均带 `tenant_id` 条件。
- [x] 2.4 实现 `internal/modules/connectors/migrations`：`connector` schema、`connector.connectors` 表、唯一与查询索引、RLS 策略、回滚语句；验证：迁移测试断言 DDL 包含 `CREATE SCHEMA IF NOT EXISTS`、`CREATE TABLE IF NOT EXISTS`、`ENABLE ROW LEVEL SECURITY` 与 `DownStatements`。
- [x] 2.5 实现 `internal/modules/connectors/api`：`/api/v1/connectors` 下 CRUD、test、sync、infer-schema 路由与错误映射；验证：`httptest` 覆盖创建、列表、404、缺租户 400、适配器不可用 503。
- [x] 2.6 实现 `internal/modules/connectors/module.go`：`Name()="connectors"`、`Dependencies()=["tenancy"]`、`Migrate`/`RegisterRoutes`/`Ready`；验证：模块测试断言接口实现、依赖声明与路由注册不返回 404。

## 3. Ingestion 模块

- [x] 3.1 实现 `internal/modules/ingestion/domain`：SyncJob/SyncRun 类型、状态枚举与状态机（含终态不可变）；验证：`go test ./internal/modules/ingestion/domain` 覆盖合法迁移与非法迁移。
- [x] 3.2 实现 `internal/modules/ingestion/service`：`Service` 公开接口、`Store`/`ConnectorReader` 端口、Create/Get/List/Cancel/Watch/MarkRunning/Complete/StartSync 用例；验证：fake store 单测覆盖创建、连接器不存在/暂停、取消、终态冲突、监听结束。
- [x] 3.3 实现 `internal/modules/ingestion/repo`：`sync_jobs`、`sync_runs` 的 pgx 查询与任务+初始运行事务写入；验证：`go build ./internal/modules/ingestion/...` 编译通过，SQL 均带 `tenant_id` 条件。
- [x] 3.4 实现 `internal/modules/ingestion/migrations`：`ingestion` schema、`sync_jobs`/`sync_runs` 表、索引、RLS 与回滚语句；验证：迁移测试断言 DDL 与回滚语句存在且可重复执行。
- [x] 3.5 实现 `internal/modules/ingestion/api`：`/api/v1/ingestion` 下 jobs 创建/查询/列表/取消/Watch（SSE）/runs 路由与错误映射；验证：`httptest` 覆盖创建、列表过滤、404、缺租户 400、终态 Watch 立即结束。
- [x] 3.6 实现 `internal/modules/ingestion/module.go`：`Name()="ingestion"`、`Dependencies()=["connectors"]`、`Migrate`/`RegisterRoutes`/`Ready`；验证：模块测试断言接口实现、依赖声明与路由注册不返回 404。

## 4. 质量门与交付说明

- [x] 4.1 运行 `gofmt -l`、`go vet ./internal/modules/connectors/... ./internal/modules/ingestion/...`，确认无格式与静态检查问题。
- [x] 4.2 运行 `go test ./internal/modules/connectors/... ./internal/modules/ingestion/...` 与 `go build ./...`，保存命令与结果到 traceability 证据。
- [x] 4.3 更新 `tasks.md` 勾选与 `traceability.md` 证据状态，运行 `openspec validate connector-catalog --strict` 并记录结果；确认未修改本 change 之外的共享文件。

## Workflow follow-up

- 本 change 完成后提交评审，由集成 change 负责在 `internal/app` 注册 `connectors`、`ingestion` 模块并完成真实 PostgreSQL 迁移验证。
- 评审通过后再执行归档与 Stage 1 退出评审；本 change 完成不等于 Stage 1 完成。
