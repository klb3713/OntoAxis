# Proposal: Connector 目录与同步任务

## Why

Stage 1 的平台基座已经具备模块注册、配置、数据库迁移与 HTTP 骨架，但还没有任何数据接入能力：系统无法登记数据源连接器，也无法发起、跟踪或取消同步任务。数据接入是 Dataset、Pipeline 与对象建模的上游，如果这一步缺失，后续模块只能依赖手工导入，无法形成可验证的数据链路。

本 change 在既有 `proto/ontoaxis/v1` 冻结契约与模块边界内，交付 Connector 定义、配置、状态与同步任务/运行状态的最小闭环，并为 Meltano / Kafka Connect 等真实执行器保留可替换的适配端口。

## What Changes

- 新增 `connectors` 业务模块：Connector 的创建、查询、列表、更新、软删除，支持 `postgres`、`mysql`、`s3`、`rest_api`、`kafka` 五种类型；配置只允许保存非敏感信息，敏感值必须通过 `secret_ref` 引用。
- 新增 Connector 状态（`active`、`paused`、`error`）与业务校验：同租户内名称唯一、非法类型/名称/敏感配置被拒绝、跨租户不可见。
- 新增 Connector 适配端口：`Test`、`InferSchema`、`StartSync` 通过注入的适配器执行；适配器未配置时明确返回不可用/未实现，不伪造成功。
- 新增 `ingestion` 业务模块：SyncJob 的创建、查询、列表、取消与监听，SyncRun 的运行状态、统计与错误信息投影；Job 状态为 `queued`、`running`、`succeeded`、`failed`、`cancelled`。
- 新增 `connector.connectors`、`ingestion.sync_jobs`、`ingestion.sync_runs` 三张表及索引、RLS 策略；模块迁移使用 `db.ExecStatements` 执行幂等 DDL。
- 新增 `/api/v1/connectors` 与 `/api/v1/ingestion` HTTP 路由，使用统一错误信封与 `page`/`per_page` 分页，租户从受信任的内部 header 读取并做空值/UUID 校验。
- 两个模块实现 `platform.Module`：`connectors` 声明依赖 `tenancy`，`ingestion` 声明依赖 `connectors`；按写入范围约束不修改 `internal/app`、`cmd/` 与平台路由，模块注册由集成 change 完成。

**Out of scope**

- 不接入真实 Meltano、Kafka Connect、Debezium 或任何外部数据源，不执行真实同步。
- 不实现连接器密钥托管、OAuth 授权流程或 `secret_ref` 背后的秘密存储，只保留引用字段。
- 不实现 CDC 细节、字段级 Schema 映射、数据质量规则或 Dataset 落盘。
- 不修改 `proto/ontoaxis/v1` 契约，不修改 `go.mod`/`go.sum`，不修改共享入口与部署文件。

## Capabilities

### New Capabilities

- `connectors`: 连接器目录能力，负责连接器定义、非敏感配置、状态、租户隔离以及测试/推断 Schema/启动同步的对外行为。
- `ingestion`: 同步任务能力，负责 SyncJob 生命周期、SyncRun 运行状态投影、取消与监听行为。

### Modified Capabilities

- 无。本 change 只引入新能力，不修改既有 `openspec/specs/` 中的行为契约。

## Impact

- 新增代码目录：`internal/modules/connectors/`、`internal/modules/ingestion/`（`module.go`、`api/`、`domain/`、`service/`、`repo/`、`migrations/`）。
- 新增数据库对象：`connector` schema 与 `connector.connectors` 表；`ingestion` schema 与 `ingestion.sync_jobs`、`ingestion.sync_runs` 表，均含 `tenant_id`、软删除与 RLS。
- 新增 HTTP 前缀：`/api/v1/connectors`、`/api/v1/ingestion`；无破坏性 API 变更。
- 运行期依赖：`connectors` 依赖 `tenancy`（模块级声明，服务层通过租户上下文与 RLS 隔离）；`ingestion` 依赖 `connectors` 的查询接口校验连接器存在且可用。集成阶段由 `stage-1-core-integration` 装配模块与路由。
- 不引入新的 Go 依赖；测试使用 fake store 与 `httptest`，不依赖真实 PostgreSQL。
