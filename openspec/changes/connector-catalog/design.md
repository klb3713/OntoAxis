# Design: Connector 目录与同步任务

## Context

参见 `proposal.md` 的动机。当前平台基座（`stage-1-platform-foundation`）已提供 `platform.Module` 接口、`pgxpool`、`db.ExecStatements`、chi 路由、统一错误信封与冻结的 `proto/ontoaxis/v1` 契约。`connectors` 与 `ingestion` 是 Wave 1 的两个独立业务模块，必须在自己的目录内完成实现，不修改 `internal/app`、`cmd/`、平台路由、`go.mod`/`go.sum`。

运行期依赖方向固定为 `connectors → tenancy`、`ingestion → connectors`，禁止反向依赖。真实 Meltano/Kafka Connect 执行器在 Stage 1 不接入，只能通过适配端口保留替换点。

## Goals / Non-Goals

**Goals:**

- 交付 Connector CRUD、配置安全校验、状态与租户隔离的完整服务端行为。
- 交付 SyncJob 创建、查询、列表、取消、监听与 SyncRun 状态投影。
- 以 `connector` / `ingestion` 两个独立 schema 落地可重复执行的 DDL，含索引与 RLS。
- 用端口/适配器隔离 Meltano、Kafka Connect 等外部执行器，缺省时 fail-closed。
- 用 fake store 与 `httptest` 覆盖成功、无效输入与关键拒绝路径，不依赖真实 PostgreSQL。

**Non-Goals:**

- 不实现真实同步执行、CDC、Schema 映射、密钥托管或 Dataset 落盘。
- 不实现 `tenancy`、`datasets` 等其他模块，也不装配到进程入口。
- 不提供生产级适配器实现，不进行真实数据验收。
- 不新增第三方依赖，不修改冻结 proto 契约。

## Decisions

### D1. 模块边界与依赖装配

- **Decision**: `connectors` 模块声明 `Dependencies() = ["tenancy"]`，`ingestion` 声明 `["connectors"]`。`ingestion` 通过一个窄接口 `ConnectorReader`（方法签名与 `connectors.Service.GetConnector` 一致）查询并校验连接器；`connectors` 不 import `ingestion`，只定义 `SyncStarter` 端口供外部注入。
- **Rationale**: 保持依赖箭头单向（`ingestion → connectors`），同时让 `ConnectorService.StartSync` 不反向依赖 ingestion。集成阶段可把 `ingestion.Service` 作为 `SyncStarter` 注入 connectors。
- **Alternatives considered**:
  - connectors 直接 import ingestion：形成反向依赖与循环，违反 `ARCHITECTURE.md` 3.4.2。
  - 把 StartSync 完全交给 HTTP 层编排：破坏 `connectors.Service` 的公开契约。
  - 让 ingestion 直接读 `connector` 表：违反跨模块禁止直读表规则。

### D2. 分层与存储端口

- **Decision**: 每个模块按 `domain/`、`service/`、`repo/`、`api/`、`migrations/`、`module.go` 分层。`service` 定义 `Store` 接口，`repo` 提供 pgx 实现；测试使用内存 fake store。`module.go` 只做 `service` + `api` 装配。
- **Rationale**: `service` 不依赖数据库，状态机与校验可单测；`repo` 只访问本模块 schema；`module.go` 满足平台“只装配”约束，集成 agent 可替换 store/adapter。
- **Alternatives considered**:
  - service 直接持有 `*pgxpool.Pool`：测试必须依赖真实 Postgres，且破坏分层。
  - 引入 ORM/sqlc：Stage 1 未建立生成链，且本 change 禁止修改 `go.mod`，不新增依赖。

### D3. 数据库 DDL 与幂等迁移

- **Decision**: `connectors` 创建 `connector` schema 与 `connector.connectors`；`ingestion` 创建 `ingestion` schema 与 `ingestion.sync_jobs`、`ingestion.sync_runs`。DDL 使用 `CREATE SCHEMA/TABLE/INDEX IF NOT EXISTS`，RLS 策略用 `DO $$ ... pg_policies ... $$` 守卫，最终由 `db.ExecStatements` 逐条执行；同时导出 `DownStatements` 作为回滚参考。
- **Rationale**: 平台 `Module.Migrate` 只拿到 `*pgxpool.Pool`，`db.ExecStatements` 是基座提供的模块迁移入口；幂等 DDL 保证重复启动不报错。
- **Alternatives considered**:
  - goose SQL 文件：基座已有 goose，但 `Module.Migrate` 当前没有 DSN，无法直接调用 `MigrateFS`；先按基座提供的 `ExecStatements` 实现，后续迁移编排可由集成 change 统一升级。
  - 只写 SQL 文件不执行：无法满足“迁移可运行”的可验证要求。

### D4. HTTP 契约与租户上下文

- **Decision**: 在 `/api/v1/connectors` 与 `/api/v1/ingestion` 下提供 REST 路由，响应字段对齐冻结 proto；列表统一 `page`/`per_page`，返回 `page` 对象；错误统一使用 `internal/platform/httperr`。租户从 `X-Ontoaxis-Tenant-Id` 内部 header 读取，缺失或非 UUID 返回 400；出现外部 `X-Tenant-Id` 时返回 400。`X-Ontoaxis-User-Id` 可选，用于 `created_by`。
- **Rationale**: `ARCHITECTURE.md` 16.2 要求网关清洗外部 header 后注入内部 header；模块不能信任外部租户头；统一分页和错误信封避免各模块自造格式。
- **Alternatives considered**:
  - 从 query/body 读取 tenant_id：可伪造，违反 fail-closed。
  - 每个接口自定义分页：违反 16.5 同一接口只选一种分页形态的要求。

### D5. 外部执行器适配端口

- **Decision**: `connectors` 定义 `ConnectorTester`、`SchemaInferrer`、`SyncStarter` 三个端口；`ingestion` 定义 `ConnectorReader`。未注入适配器时，测试/推断/启动同步分别返回 `ErrUnavailable` 或 `ErrNotImplemented`，绝不返回伪成功。
- **Rationale**: Stage 1 只接契约边界，真实 Meltano/Kafka Connect 在后续阶段实现；fail-closed 避免“空适配器返回成功”造成的假闭环。
- **Alternatives considered**:
  - 用固定成功 stub：会伪造上游成功，违反项目规则。
  - 直接实现 HTTP 调用 Meltano：超出 Stage 1 范围且无真实实例。

### D6. 同步任务与运行状态模型

- **Decision**: 创建 Job 时同时创建一条初始 Run；Job 与 Run 的状态迁移在同一服务方法中同步更新。状态机：`queued → running → succeeded | failed`，`queued|running → cancelled`，终态不可变。`WatchIngestJob` 采用轮询存储 + 通道推送，HTTP 层以 SSE 输出。
- **Rationale**: `ingestion.sync_jobs` 与 `ingestion.sync_runs` 的职责不同：Job 是任务主记录，Run 是执行尝试记录；同步更新避免二者不一致。轮询实现无需引入消息系统，符合 Stage 1 最小闭环。
- **Alternatives considered**:
  - 只维护 Job 不维护 Run：无法满足运行状态投影的验收。
  - 引入 Kafka/Redis 实现 Watch：Stage 1 不接真实基础设施，且会增加依赖。

### D7. UUIDv7 与时间

- **Decision**: ID 由应用层使用 `github.com/google/uuid` 的 `NewV7()` 生成；时间统一 `time.Now().UTC()`，通过可注入的 `Clock` 供测试固定时间。
- **Rationale**: 与 `ARCHITECTURE.md` 16.3 的 UUIDv7 要求一致；注入时钟让状态迁移测试可断言。
- **Alternatives considered**:
  - 数据库 `gen_random_uuid()`：不满足 UUIDv7 时间有序要求。

## Risks / Trade-offs

- [真实 Meltano/Kafka Connect 适配器缺失] → 通过端口显式返回不可用；集成/Stage 2 注入实现后再做真实连通性验证。
- [`tenancy` 模块接口尚未在本 change 可见] → 模块级只声明依赖；服务层以租户上下文 + RLS 兜底，集成时按 tenancy 实际 Service 注入或加适配层。
- [无真实 PostgreSQL 时 RLS/DDL 无法回归] → DDL 保持幂等并导出回滚语句；本 change 的验收只声明单测/handler 级证据，真实迁移验证留给集成环境。
- [模块未注册进 `internal/app`] → 本 change 按写入范围约束不修改共享入口，由 `stage-1-core-integration` 完成装配；在此之前不能宣称端到端可用。
- [敏感配置可能通过嵌套对象泄漏] → 递归检查所有键名并拒绝敏感键；测试覆盖嵌套场景。
- [SSE 监听在长时间无状态变化时占用连接] → 监听随请求 context 取消而结束，终态立即结束；Stage 1 不承诺跨实例广播。

## Migration Plan

1. 部署时按模块顺序执行 `connectors.Migrate` → `ingestion.Migrate`，均调用 `db.ExecStatements` 执行幂等 DDL；重复执行安全。
2. 回滚：先停止写入，再按 `ingestion.sync_runs` → `ingestion.sync_jobs` → `ingestion` schema → `connector.connectors` → `connector` schema 顺序执行 `DownStatements`；本 change 不迁移既有数据。
3. 本 change 不修改已发布迁移；后续 schema 变更只追加。

## Open Questions

- Meltano 与 Kafka Connect 的真实 job 标识/状态映射规则留待真实适配器接入时确定；当前端口只要求返回任务标识与状态结果，不改变本 change 的 specs 与任务拆分。
- `tenancy` 的公开服务接口以 tenancy-workspaces change 的最终实现为准；若其方法签名与 `ConnectorReader`/租户校验不匹配，由集成 change 增加适配层，不修改本模块的数据边界。
