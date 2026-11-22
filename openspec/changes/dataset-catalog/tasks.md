# Tasks: dataset-catalog

> 每个任务完成后才能勾选；证据必须可复核。不要在本 change 内 commit 或宣布 Stage 完成。

## 1. 规格与设计

- [x] 1.1 完成 proposal 与 `dataset-catalog` delta spec，覆盖数据集、分支、事务、视图、文件元数据、租户隔离与列表错误契约。验证：`openspec validate dataset-catalog --strict` 通过；逐条检查 scenario 均含 WHEN/THEN。
- [x] 1.2 完成 design.md、tasks.md 与 traceability.md，记录决策、替代方案、迁移回滚和风险。验证：文件存在且 traceability 的 Stage AC 映射无空项；与 proposal/spec 的 requirement 名一致。

## 2. 领域模型与 schema 迁移

- [x] 2.1 在 `internal/modules/datasets/domain/` 实现 Dataset、Branch、Transaction、View、File 类型、格式枚举、事务状态机与哨兵错误。验证：`go test ./internal/modules/datasets/domain/...` 通过，覆盖非法格式与非法状态迁移。
- [x] 2.2 在 `internal/modules/datasets/migrations/` 实现 `dataset` schema 幂等 DDL，包含 5.1 核心表、`dataset_files` 元数据表、索引与租户 RLS。验证：`go test ./internal/modules/datasets/migrations/...` 通过；语句列表包含 `CREATE SCHEMA IF NOT EXISTS dataset`、每张表的 `CREATE TABLE IF NOT EXISTS` 与 `FORCE ROW LEVEL SECURITY`。

## 3. 仓储与服务

- [x] 3.1 实现 `service.Repository` 接口与 `repo.PostgresStore`，所有查询显式带 `tenant_id`，事务提交在同一条数据库事务内更新分支 head。验证：`go build ./internal/modules/datasets/...` 通过；代码审查确认无拼字符串 SQL。
- [x] 3.2 实现 `repo.MemoryStore` 作为 fake store，覆盖数据集、分支、事务、视图、文件元数据与分页。验证：`go test ./internal/modules/datasets/repo/...` 通过。
- [x] 3.3 实现数据集与分支服务：创建数据集时生成 UUIDv7、RID 并创建 `main` 分支，校验租户、名称、格式与分页。验证：`go test ./internal/modules/datasets/service/...` 通过，覆盖成功、重名、非法格式、跨租户。
- [x] 3.4 实现事务、视图与文件元数据服务：事务状态机、视图唯一名、文件元数据校验与删除。验证：`go test ./internal/modules/datasets/service/...` 通过，覆盖重复提交被拒、非法文件元数据。

## 4. HTTP API 与模块注册

- [x] 4.1 实现 `/api/v1/datasets` JSON API、DTO、分页、错误映射与 fail-closed `TenantResolver`，默认只接受 `X-Ontoaxis-Tenant-Id`。验证：`go test ./internal/modules/datasets/api/...` 通过，覆盖 201/204、400、401、404、409 与未知路由。
- [x] 4.2 实现 `module.Module`：`Name()=datasets`、`Dependencies()=[tenancy]`、`Migrate` 通过 `db.ExecStatements` 执行 DDL、`RegisterRoutes` 挂载 API、`Ready` ping 仓储。验证：`go test ./internal/modules/datasets` 通过，覆盖元数据与路由注册。

## 5. 测试与质量

- [x] 5.1 运行 `gofmt -l`、`go vet ./internal/modules/datasets/...` 与 `go test ./internal/modules/datasets/...`，记录命令与结果。验证：命令退出码为 0，gofmt 无输出。
- [x] 5.2 复核测试覆盖成功、无效输入、关键拒绝路径与跨租户边界，补齐缺失断言。验证：测试列表与 traceability 场景一一对应；`go test -cover ./internal/modules/datasets/...` 记录覆盖率。

## 6. 文档与交付

- [x] 6.1 将实际测试证据写入 traceability.md，标注 `verified` 或 `partial`，并记录真实 Postgres/认证未验证的偏差。验证：traceability 的每个 Stage AC 行都有证据位置与状态。
- [x] 6.2 运行最终校验命令并整理文件清单、测试结果与剩余风险；不 commit、不宣布 Stage 完成。验证：`openspec validate dataset-catalog --strict` 与 `go test ./internal/modules/datasets/...` 均通过，报告中包含风险与集成门 G1 待办。
