# Design: pipeline-authoring

## Context

Stage 1 的平台基座已经提供 `platform.Module`、`db.ExecStatements`、chi HTTP 服务器与统一错误包；`datasets` 模块与本 change 并行交付。`pipelines` 模块需要在这一边界内交付 Pipeline 定义、发布版本、运行入口与 Schedule 的最小垂直切片，同时为后续真实 Kestra 接入保留稳定接口。

约束：

- `pipelines` 只能依赖 `datasets`（ARCHITECTURE 3.4.2），不得直接读写其他模块的表。
- 目录分层固定为 `module.go`、`api/`、`domain/`、`service/`、`repo/`、`migrations/`（ARCHITECTURE 3.4.3）。
- 表结构必须符合 ARCHITECTURE 5.1，schema 为 `pipeline`；所有租户表启用 RLS 与 FORCE ROW LEVEL SECURITY。
- Kestra 只做接口与内存 adapter 骨架，不接真实实例；不得自行实现 DAG 引擎、cron 调度器或执行器（ARCHITECTURE 7.2）。
- 迁移通过 `db.ExecStatements` 执行幂等 DDL；测试不得依赖真实 Postgres。

## Goals / Non-Goals

**Goals**

- 交付可注册的 `pipelines` 模块、可测试的领域/服务/仓储/HTTP 分层，以及可重复执行的 schema DDL。
- 用内存 fake store 与 `httptest` 覆盖成功、无效输入、租户隔离、版本递增与 adapter 失败等关键路径。
- 为后续集成 change 提供稳定的 `pipelines.Service`、`pipelines.ScheduleService` 与 `/api/v1/pipelines` 路由。

**Non-Goals**

- 不实现真实 Kestra Flow YAML 生成、Kestra API 调用或 Flow 双向同步。
- 不实现 DAG 校验器、cron 调度器、执行状态回写或 Build/Job 管理。
- 不注册到 `cmd/`；模块装配由 `stage-1-core-integration` 负责。
- 不提供真实 Postgres、Kestra 或身份服务的联调证据。

## Decisions

### D1. 模块构造与依赖注入

`Module` 通过 `New(pool *pgxpool.Pool, opts ...Option)` 构造，支持 `WithStore`、`WithKestraAdapter`、`WithDatasetResolver` 与 `WithTenantResolver` 覆盖默认实现。`Name()` 返回 `pipelines`，`Dependencies()` 返回 `datasets`，`Migrate` 执行 schema DDL，`RegisterRoutes` 挂载 API，`Ready` ping 仓储。

**替代方案**：在 `module.go` 内直接 new 所有依赖。放弃原因是无法在不启动 Postgres 的测试中注入 fake store。

**权衡**：默认路径依赖真实 pool；测试路径通过 `WithStore` 注入内存 store，仍覆盖 module 的元数据与路由注册逻辑。

### D2. 领域模型使用字符串 ID，服务层统一 UUID 校验

`domain` 包只包含数据结构、枚举校验、cron 解析与哨兵错误，不导入 pgx、chi 或平台包。ID 在领域与服务层使用 string（与并行交付的 datasets 模块保持一致），服务层用 `uuid.Parse` 校验并统一为小写形式，生成 ID 使用 `uuid.NewV7()`。

**替代方案**：领域层使用 `uuid.UUID`。放弃原因是 fake store 与 HTTP DTO 转换会更啰嗦，且跨模块适配需要额外转换；字符串在仓储边界更简单。

**权衡**：UUID 格式校验由服务层负责，仓储实现必须假定传入 ID 已规范化；Postgres 查询使用 `$n::uuid` 并统一返回文本。

### D3. 版本发布采用快照，状态机保持最小

创建 Pipeline 时 `version=0`、`status=draft`。发布时把当前 `dag` 写入 `pipeline.versions` 并递增 `version`、置 `status=published`。`pipeline.versions` 只插入不更新，保证已发布版本不可变。Pipeline 的 `status` 只有 `draft`、`published`、`archived` 三种；本 change 不提供归档 API，删除时置 `archived` 并软删除。

**替代方案**：每次更新自动产生版本。放弃原因是 proto 的 Update 只修改定义，不要求显式版本管理；显式发布更接近 Kestra Flow 注册语义。

**权衡**：修改已发布 Pipeline 的 `dag` 后，`pipeline.pipelines.version` 仍指向旧版本，直到再次发布；这是“草稿修改”语义，需在后续 UI 中提示。为避免草稿 DAG 被误当作已发布版本执行，更新 `dag` 时若 Pipeline 处于 `published` 状态，服务会把 `status` 回落为 `draft`；此时触发运行返回 409，必须先再次发布生成新版本。

### D4. 仓储接口与内存 fake

`service.Repository` 是接口，`repo.PostgresStore` 与 `repo.MemoryStore` 都实现它。Postgres 查询全部显式带 `tenant_id`，不使用字符串拼 SQL；唯一冲突转换为 `domain.ErrAlreadyExists`，无记录转换为 `domain.ErrNotFound`。`MemoryStore` 使用互斥锁并深拷贝 `dag`，用于单元测试与 httptest，不引入数据库驱动。

**替代方案**：服务层直接使用 pgxpool。放弃原因是无法离线测试，也违反 api/service/domain/repo 分层。

**权衡**：仓储接口方法较多，但按 Pipeline、Run、Schedule 分组后可读；真实 Postgres 的并发行为只能由集成环境验证。

### D5. 迁移使用内嵌 SQL 与 `-- +statement` 标记

`migrations/00001_pipeline_init.sql` 通过 `//go:embed` 内嵌；Go 侧只在 `-- +statement` 分隔行切分，避免 PL/pgSQL 或 SQL 字符串中的分号被误切。所有 `CREATE SCHEMA`/`CREATE TABLE`/`CREATE INDEX` 使用 `IF NOT EXISTS`；RLS 使用 `DROP POLICY IF EXISTS` + `CREATE POLICY` 保证重复执行收敛。迁移 forward-only，不提供 down 迁移。

**替代方案**：goose `*.sql` + `*.down.sql`。放弃原因是 `module.Module.Migrate` 只收到 pool，平台未提供 per-module goose 实例；任务明确要求 `db.ExecStatements` 幂等 DDL。

**权衡**：模块迁移没有版本账本，每次 `Migrate` 重放全部幂等语句；生产环境通过启动顺序与 DDL 权限控制，回滚需后续迁移或恢复演练。

### D6. Kestra adapter 只做接口与内存骨架

`kestra.Adapter` 定义 `RegisterFlow(ctx, Flow) error` 与 `TriggerRun(ctx, RunRequest) (RunResult, error)`。`MemoryAdapter` 用互斥 map 保存 Flow、用切片记录运行请求，不发起网络调用。发布时服务把 Pipeline 的当前版本、名称与定义传给 adapter；触发时传 Pipeline ID 与版本。真实 Flow YAML 转换与 Kestra API 调用留给后续 Stage 2/集成 change。

**替代方案**：直接生成 YAML 并调用 Kestra HTTP API。放弃原因是 Stage 1 明确不接真实实例，且新增 YAML/HTTP 依赖会扩大范围。

**权衡**：`MemoryAdapter` 的 `RunResult` 只用于服务判断成功/失败；`pipeline.runs` 没有外部 execution id 列，因此本 change 不持久化 Kestra execution id，后续迁移可补列。

### D7. DatasetResolver 消费端窄接口与 fail-closed

`service.DatasetResolver` 只暴露 `DatasetExists(ctx, tenantID, datasetID string) (bool, error)`。服务从 `dag` 的 `inputs[].dataset_id` 与顶层 `dataset_ids` 提取引用；发布与触发前校验。未注入 resolver 且存在引用时返回 `domain.ErrUnavailable`，不写版本/运行记录；resolver 返回不存在时返回 `domain.ErrInvalidArgument`。`datasets` 模块接入时通过适配器实现该接口，不直接依赖其内部类型。

**替代方案**：直接 import `datasets.Service`。放弃原因是当前 `datasets` 模块仍并行开发，直接依赖会阻塞编译；消费端接口也避免跨模块类型耦合。

**权衡**：DAG 的 dataset 引用只按约定字段识别，非约定形状不会被校验；后续应在 schema/UI 层统一 Pipeline 定义结构。

### D8. HTTP 契约、租户解析与错误映射

路由挂在 `/api/v1/pipelines` 下，JSON DTO 对齐 proto 字段命名（snake_case）。`TenantResolver` 是接口，默认实现 `RejectAllTenantResolver` 拒绝所有请求（fail-closed）；测试与本地联调使用 `HeaderTenantResolver` 读取 `X-Ontoaxis-Tenant-Id`。服务错误映射到 `httperr`：`invalid_argument` 400、`unauthorized` 401、`already_exists` 409、`conflict` 409、`not_found` 404、`unavailable` 503、其他 500。跨租户访问统一 404。

**替代方案**：直接读取客户端 `X-Tenant-Id`。放弃原因是该头可能被外部伪造；identity-edge 集成时应注入认证后的 resolver。

**权衡**：本 change 无法独立完成真实认证，只能在单测中模拟可信头；集成风险需在 `stage-1-core-integration` 中关闭。

### D9. 分页与列表默认值

所有列表接口接受 `page`（默认 1）与 `per_page`（默认 50，最大 200），返回 `{pipelines|runs|schedules, page:{page,per_page,total,total_pages}}` 结构。Pipeline 列表默认按 `created_at DESC`；运行列表按 `started_at DESC`；Schedule 列表按 `created_at DESC`。仓储层执行 `LIMIT/OFFSET` 与独立 `COUNT(*)`。

**替代方案**：cursor 分页。放弃原因是当前数据量小、proto 只定义 PageRequest/PageResponse；cursor 留待后续性能优化。

## Risks / Trade-offs

- [真实 Postgres 未验证] `Migrate` 只在测试中做语句结构检查，未在真实 Postgres 执行；G1 必须在本地 Postgres 执行迁移并检查 RLS。缓解：DDL 幂等、显式 `IF NOT EXISTS`，并保留集成任务。
- [RLS 会话上下文依赖集成] 四张表均启用并强制 RLS；`PostgresStore` 虽然显式带 `tenant_id` 过滤，但连接未设置 `app.tenant_id` 时策略会返回空集（fail-closed）。平台必须在每个请求/事务内设置 `app.tenant_id`（及必要的 `app.is_platform_admin`）；否则真实 Postgres 路径不可用。缓解：本 change 用 `MemoryStore` 验证服务契约，集成门必须补真实 RLS 会话测试。
- [认证未落地] 默认 resolver 拒绝所有请求；若集成方误用测试用 `HeaderTenantResolver` 且网关未剥离外部同名头，会产生租户伪造风险。缓解：默认 fail-closed，集成 change 必须注入 identity-edge 的认证后 resolver 并补端到端测试。
- [Kestra adapter 与真实执行脱节] 内存 adapter 只记录请求，运行状态不会自动推进到 `succeeded`/`failed`；Stage 1 不声称提供真实执行闭环。缓解：接口稳定，后续适配器与状态回写单独 change。
- [版本并发] 发布时先读后写，两个并发发布可能竞争同一版本号；Postgres 唯一键会拒绝后者并返回 409。缓解：仓储唯一约束兜底，客户端可重试。
- [adapter 外部副作用先于持久化] 发布流程先调用 `RegisterFlow`，再写 `pipeline.versions`；若数据库写入失败，Flow 可能已在执行引擎侧登记。Stage 1 的 `MemoryAdapter` 无外部副作用，真实 Kestra 适配必须在后续 change 设计幂等登记或补偿/outbox 机制。
- [DAG 引用校验形状有限] 只识别 `inputs[].dataset_id` 与 `dataset_ids`；未约定的引用不会触发校验。缓解：在文档与后续 schema 中固定定义结构。
- [forward-only 迁移] 没有 down 迁移；生产回滚需要新迁移或恢复流程。缓解：在 G1 前完成迁移演练和备份确认。

## Migration Plan

1. 在测试环境对空库执行 `Migrate`，确认 `pipeline` schema、4 张表、索引与 RLS 策略存在。
2. 重复执行 `Migrate`，确认无错误且对象数量不变（幂等）。
3. 由 `stage-1-core-integration` 注册模块并注入认证后的 `TenantResolver`、`DatasetResolver` 与 Kestra adapter。
4. 回滚：应用层取消模块注册即可停止路由；schema 回滚需按变更窗口执行 `DROP SCHEMA pipeline CASCADE`（仅限非生产）或由后续迁移处理，生产回滚遵循 RUNBOOK 与负责人审批。

## Open Questions

- 真实 identity-edge 的租户注头名称与 resolver 注入点需在集成 change 中确认。
- Kestra Flow YAML 的命名空间与 Flow ID 约定需在 Stage 2 真实适配时确认。
- `pipeline.runs` 是否需要补充 `external_execution_id` 列以关联 Kestra execution，留待真实适配 change 评估。
