# Proposal: Ontology Definition

## Why

Stage 1 的核心数据建模能力依赖一个独立、受租户隔离的 Ontology 元数据模块。当前仓库只有模块注册接口与平台基座，`internal/modules/ontology` 尚不存在，对象类型、属性、链接类型与数据集绑定没有可用的数据模型、迁移和 HTTP 契约。若继续等待其他模块先行，对象、查询与动作模块将没有可引用的类型系统。

本 change 冻结 Ontology 元数据的持久化边界与接口行为，为后续 `objects`、`query`、`actions` 等 change 提供稳定依赖，同时明确将 Project/ProjectResource 管理留给 `tenancy`，避免职责越界。

## What Changes

- 新增 `ontology` 业务模块，提供对象类型（Object Type）的创建、查询、列表、更新与删除。
- 新增属性的创建、列表、更新与删除，属性必须归属于同一租户下已存在的对象类型。
- 新增链接类型的创建、列表、更新与删除，链接类型必须引用同一租户下已存在的源/目标对象类型。
- 新增数据集绑定：将对象类型绑定到数据集、解除绑定，并提供物化绑定的受理入口（返回操作 ID，不执行真实物化）。
- 新增 `ontology` schema 下 `object_types`、`properties`、`link_types` 的幂等迁移；所有表包含租户隔离、软删除与更新时间语义。
- 新增 `/api/v1/ontology` 下的 HTTP JSON 路由与统一错误响应映射。
- 明确不读写 `tenancy` 的 Project/ProjectResource 表，不管理项目与项目资源。
- 新增 fake store 单元测试与 `httptest` 路由测试，不依赖真实 PostgreSQL。

**Out of scope**

- 不实现对象实例、对象数据存储与查询执行。
- 不实现真实的数据集物化、同步任务或外部系统调用；`MaterializeBinding` 仅做前置校验并返回受理操作 ID。
- 不实现认证/JWT 签发、租户解析中间件或平台级授权，只消费调用方传入的租户上下文。
- 不修改 `proto/ontoaxis/v1`，不修改 `cmd/`、`internal/app/`、`internal/platform/` 或其他模块目录。
- 不实现前端页面、真实数据集或生产环境验收。

## Capabilities

- **New Capabilities**: `ontology-metadata`
- **Modified Capabilities**: 无

## Impact

- 新增代码目录 `internal/modules/ontology/`（`module.go`、`api/`、`domain/`、`service/`、`repo/`、`migrations/`）。
- 新增数据库 schema `ontology` 与三张业务表；由模块自身迁移创建，不触碰其他 schema。
- 新增 `/api/v1/ontology` HTTP 路由；注册到进程的装配由后续集成 change 负责，本 change 只保证模块实现可被注册。
- 依赖 `tenancy` 模块（仅声明依赖顺序，不直接调用其内部实现或表）。
- 不新增第三方依赖；使用仓库已有的 `chi`、`pgx`、`google/uuid` 与平台 `httperr`、`db` 包。
- 无破坏性变更；尚无生产数据，迁移可重复执行。
