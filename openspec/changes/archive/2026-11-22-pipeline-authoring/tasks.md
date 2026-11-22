# Tasks: pipeline-authoring

> 每个任务完成后才能勾选；证据必须可复核。不要在本 change 内 commit 或宣布 Stage 完成。

## 1. 规格与设计

- [x] 1.1 完成 proposal 与 `pipelines` delta spec，覆盖 Pipeline 定义、发布版本、运行入口、运行查询、Schedule、Kestra 边界、dataset 依赖、租户隔离与列表错误契约。验证：`openspec validate pipeline-authoring --strict` 通过；逐条检查 requirement 均含 WHEN/THEN scenario。
- [x] 1.2 完成 design.md、tasks.md 与 traceability.md，记录决策、替代方案、迁移回滚和风险。验证：文件存在，traceability 的 Stage AC 映射无空项，requirement 名称与 spec 一致。

## 2. 领域模型与迁移

- [x] 2.1 在 `internal/modules/pipelines/domain/` 实现 Pipeline、PipelineVersion、PipelineRun、Schedule 类型、枚举校验、cron 解析与哨兵错误。验证：`go test ./internal/modules/pipelines/domain/...` 通过，覆盖非法类型、非法 JSON 定义、非法 cron/时区与下一次运行时间计算。
- [x] 2.2 在 `internal/modules/pipelines/migrations/` 实现 `pipeline` schema 幂等 DDL，包含 4 张表、索引、租户 RLS 与 FORCE ROW LEVEL SECURITY。验证：`go test ./internal/modules/pipelines/migrations/...` 通过；语句列表包含 `CREATE SCHEMA IF NOT EXISTS pipeline`、每张表的 `CREATE TABLE IF NOT EXISTS` 与 `FORCE ROW LEVEL SECURITY`。

## 3. 仓储与服务

- [x] 3.1 实现 `service.Repository` 接口与 `repo.PostgresStore`，所有查询显式带 `tenant_id`，发布版本在同一事务内插入版本并更新 Pipeline。验证：`go build ./internal/modules/pipelines/...` 通过；代码审查确认用户输入均通过绑定参数传入，动态 WHERE 只拼接固定占位符片段。
- [x] 3.2 实现 `repo.MemoryStore` 作为 fake store，覆盖 Pipeline、版本、运行、Schedule、分页与租户隔离。验证：`go test ./internal/modules/pipelines/repo/...` 通过。
- [x] 3.3 实现 Pipeline 服务：创建/读取/列表/更新/软删除、发布版本、触发运行与运行查询；校验名称、类型、DAG、分页与发布状态。验证：`go test ./internal/modules/pipelines/service/...` 通过，覆盖成功、重名、非法类型、草稿触发拒绝、版本递增与跨租户。
- [x] 3.4 实现 Schedule 服务：CRUD、暂停/恢复、cron/timezone 校验、`next_run_at` 计算与唯一性。验证：`go test ./internal/modules/pipelines/service/...` 通过，覆盖非法 cron、重复 cron、暂停/恢复。

## 4. Kestra 与 dataset 边界

- [x] 4.1 实现 `internal/modules/pipelines/kestra/` 的 `Adapter` 接口与 `MemoryAdapter` 骨架，记录 Flow 登记与运行请求，不发起网络调用。验证：`go test ./internal/modules/pipelines/kestra/...` 通过。
- [x] 4.2 实现 `service.DatasetResolver` 消费端接口，在发布与触发时校验 DAG 引用；未注入时 fail-closed。验证：`go test ./internal/modules/pipelines/service/...` 通过，覆盖 resolver 未注入、dataset 不存在与存在三种路径。

## 5. HTTP API 与模块注册

- [x] 5.1 实现 `/api/v1/pipelines` JSON API、DTO、分页、错误映射与 fail-closed `TenantResolver`，默认只接受显式注入的 resolver，测试用 `HeaderTenantResolver` 读取 `X-Ontoaxis-Tenant-Id`。验证：`go test ./internal/modules/pipelines/api/...` 通过，覆盖 201/202/204、400、401、404、409、503 与未知路由。
- [x] 5.2 实现 `module.Module`：`Name()=pipelines`、`Dependencies()=[datasets]`、`Migrate` 通过 `db.ExecStatements` 执行 DDL、`RegisterRoutes` 挂载 API、`Ready` ping 仓储。验证：`go test ./internal/modules/pipelines` 通过，覆盖模块元数据与路由注册。

## 6. 测试与质量

- [x] 6.1 运行 `gofmt -l`、`go vet ./internal/modules/pipelines/...` 与 `go test ./internal/modules/pipelines/...`，记录命令与结果。验证：命令退出码为 0，gofmt 无输出。
- [x] 6.2 复核测试覆盖成功、无效输入、关键拒绝路径与跨租户边界，补齐缺失断言。验证：测试列表与 traceability 场景一一对应；`go test -cover ./internal/modules/pipelines/...` 记录覆盖率。

## 7. 文档与交付

- [x] 7.1 将实际测试证据写入 traceability.md，标注 `verified` 或 `partial`，并记录真实 Postgres/Kestra/认证未验证的偏差。验证：traceability 的每个 Stage AC 行都有证据位置与状态。
- [x] 7.2 运行最终校验命令并整理文件清单、测试结果与剩余风险；不 commit、不宣布 Stage 完成。验证：`openspec validate pipeline-authoring --strict` 与 `go test ./internal/modules/pipelines/...` 均通过，报告中包含风险与集成门待办。

## Evidence log

验证时间：2026-11-22（本地工作区）。以下命令均为实际执行结果：

- `openspec validate pipeline-authoring --strict` → `Change 'pipeline-authoring' is valid`，exit 0；`openspec status` 4/4 artifacts complete。
- `gofmt -l internal/modules/pipelines` → 无输出。
- `go vet ./internal/modules/pipelines/...` → 无输出，exit 0。
- `go build ./...` → exit 0。
- `go test ./internal/modules/pipelines/...` → **43 passed in 7 packages**；`go test -coverprofile` 总覆盖率 66.2%，各包：pipelines 87.5%、api 73.2%、domain 91.4%、kestra 90.9%、migrations 88.0%、repo 31.4%、service 79.4%。repo 覆盖率低是因为 `PostgresStore` 没有真实 Postgres 测试。
- `go test ./...` → **293 passed in 77 packages**（包含其他并行 change 的包；本模块 7 个包全部通过）。
- `openspec validate --all --strict` → 10 passed / 2 failed，失败为其他 change 的 `query-surface` 与 `stage-1-core-integration`（均报无 delta）；`pipeline-authoring` 本身通过，未修改他人 artifacts。
- 未执行：真实 Postgres 迁移、真实 Kestra 调用、真实 identity-edge 认证联调、`cmd/`/`internal/app/` 模块注册。这些偏差见 `traceability.md` 第 2 节。

## Workflow follow-up

- 在实现与验证全部完成后，提交 change 评审并请求项目负责人确认。
- 评审通过后运行 `openspec validate --all --strict` 与 `openspec status --change pipeline-authoring`，再执行归档；归档前不得宣称 Stage 1 完成。
- `stage-1-core-integration` 负责把本模块注册进 `internal/app`，并注入真实 identity-edge 的 `TenantResolver`、datasets 的 `DatasetResolver` 与 Stage 2 Kestra adapter；真实 Postgres 迁移执行属于集成门 G1。
