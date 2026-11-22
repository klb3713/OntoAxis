# Stage 1 — OntoAxis Core 一次性构建（批准版）

> 状态：approved（负责人复核确认，2026-11-22）<br>
> 本次更新：按 `ARCHITECTURE.md` v1.0（2026-11-22）同步模块清单、数据所有权、契约基线、目录与部署口径；变更后的 Stage 范围与 AC 已按负责人复核确认，作为 Stage 1 新基线。<br>
> 阶段编号：S1<br>
> 负责人：klb3713<br>
> 最后更新：2026-11-22<br>
> 目标设计：[`ARCHITECTURE.md`](../../ARCHITECTURE.md)（v1.0，2026-11-22，重点第 3、5、6、9、16、17 章）<br>
> 已确认决策：Go module `github.com/klb3713/ontoaxis`、GitHub Actions、Keycloak 单 realm + Organizations、Trino 真实单节点、Kestra/Temporal 契约适配器、`oa-web` Refine resource-first、Stage 1 actions 只到 Action Type + adapter 骨架；业务模块固定 9 个，`proto/ontoaxis/v1` 的 21 个 proto 文件（1,272 行）已通过 `buf format`/`buf lint`/`buf build`，作为 Stage 1 的冻结契约基线。

## 1. 目的

以 `ARCHITECTURE.md` v1.0 第 3、5、6、9、16、17 章为实施依据，一次性构建首个可运行的 OntoAxis 平台基线：

- 一个 Go 模块化单体 `ontoaxis-core`（部署单元 `oa-core`），承载 9 个业务模块；
- 一个 `oa-edge`（Envoy Gateway / Traefik + YAML 路由表）；
- 一个 `oa-web`（React 19 + Refine + 生成客户端，源码位于 `web/`）；
- 一套可在本地启动的 OSS 数据面（PostgreSQL、Keycloak、OpenFGA、Valkey、Kafka/Connect/Debezium、Nessie、Trino、Kestra、Temporal、SeaweedFS、Meilisearch 等按下文范围接入）；
- 一套可执行的构建、测试、契约与部署质量门。

本 Stage 采用**一次性构建、直接替换**策略，不保留过渡服务、不做双写或灰度迁移。

## 2. 交付范围

### 2.1 必须交付

- `ontoaxis-core` 单二进制，包含 `tenancy`、`connectors`、`ingestion`、`datasets`、`pipelines`、`query`、`ontology`、`objects`、`actions` 九个业务模块；`identity`、`authorization`、`health`、`operation` 作为 platform 能力提供，不注册为业务模块；
- `oa-edge` 路由表、JWT/租户上下文与 `oa-web` 核心页面；
- 以 `proto/ontoaxis/v1` 为唯一契约源（21 个 proto 文件、1,272 行，已通过 `buf format`/`buf lint`/`buf build`），生成 Go/Connect、OpenAPI 与 TypeScript 客户端；
- PostgreSQL 单实例、每模块独立 schema（`core`、`connector`、`ingestion`、`dataset`、`pipeline`、`query`、`ontology`、`object_data`、`action`、`jobs`）；
- `deploy/compose` 的本地 Compose 与初始化脚本、GitHub Actions CI 质量门；
- 与 `ARCHITECTURE.md` 第 17.4 节对应的核心 E2E 验收。

### 2.2 明确不交付

- 生产 HA、容量规划、跨区域灾备与生产数据验收；
- 完整的高级功能（复杂图遍历、完整 BI、完整 AI/ML、全部 Connector 适配器）；
- 真实客户数据；本 Stage 的 E2E 只使用合成 fixture；
- 多服务代码结构、按服务拆分数据库或旧 Helm chart 的直接复制。

## 3. 前置条件

1. Stage 1 brief 经负责人评审并标记为 `approved`；
2. `ARCHITECTURE.md` v1.0（2026-11-22）作为目标设计基线；
3. 已确认 Go 1.25、Node 22 LTS、OpenSpec 2.1.0、容器运行时与 GitHub Actions；
4. 已确认 Keycloak 单 realm + Organizations、OpenFGA、PostgreSQL、Valkey、SeaweedFS、Kafka、Trino 单节点本地启动方式；Kestra/Temporal 在 Stage 1 只接契约适配器；RustFS 仅作为通过 S3 兼容性 PoC 后的候选，不进入默认 Compose。
5. 确认本 Stage 不接入真实数据、不启动生产环境。

## 4. Stage 验收标准（AC）

| AC | 验收结果 |
|---|---|
| S1-AC-01 | `ontoaxis-core` 可通过单一配置启动，按 `ARCHITECTURE.md` 3.4.4 顺序装配模块，暴露 `/healthz`、`/readyz`、`/metrics`，未注册路由返回 404；`go build ./...` 与 `go test ./...` 通过 |
| S1-AC-02 | Keycloak OIDC 登录可用；JWT 校验、租户上下文注入、fail-closed 鉴权通过负向测试 |
| S1-AC-03 | `tenancy` 模块提供组织、工作区、成员关系、项目与项目资源（Project/ProjectResource）CRUD/绑定，租户隔离测试通过 |
| S1-AC-04 | `ontology` 模块提供 object type、property、link type 与 dataset binding 管理，不管理 project/project resource |
| S1-AC-05 | `objects` 模块提供对象与链接的 JSONB 持久化、分页查询与过滤 |
| S1-AC-06 | `datasets` 模块提供 Dataset、Branch、Transaction、View 元数据与 S3 文件能力 |
| S1-AC-07 | `query` 模块提供保存查询、执行查询与对象查询 API；复杂查询通过真实单节点 Trino 端到端验证 |
| S1-AC-08 | `connectors` 与 `ingestion` 提供 Connector 定义、同步任务、运行状态最小闭环 |
| S1-AC-09 | `pipelines` 提供 Pipeline 定义、版本、运行入口与 Kestra 契约适配；`actions` 提供 Action Type、校验/预演与 Temporal adapter 骨架（不实现真实动作执行） |
| S1-AC-10 | `oa-web` 的登录、Projects、Ontology Manager、Object Explorer、Datasets、Queries 页面通过 Playwright E2E |
| S1-AC-11 | 本地 Compose 可一键启动核心闭环；CI 通过 build、test、契约（含 `buf lint`/`buf breaking`）、前端与许可证门禁 |
| S1-AC-12 | 发布镜像、路由配置与回滚 tag 可用，业务部署单元只包含 `oa-core`（二进制 `ontoaxis-core`）、`oa-edge` 与 `oa-web` |

## 5. Change 清单

| Change 建议目录名 | 类型 | 行为/能力边界 | 主要模块 | 依赖 | 并行波次 |
|---|---|---|---|---|---|
| `stage-1-platform-foundation` | Stage 支撑 | Go module `github.com/klb3713/ontoaxis`、`internal/app`、平台包、配置、DB、健康检查、日志、错误模型、模块注册接口（`Name/Dependencies/Migrate/RegisterRoutes/Ready`）、`proto/ontoaxis/v1` 生成链、GitHub Actions CI 骨架、`deploy/compose` 骨架 | `internal/app`、`internal/platform`、`cmd/ontoaxis-core` | — | Wave 0 |
| `identity-edge` | 能力 | Keycloak 单 realm + Organizations、OIDC/JWKS、JWT/租户头、`oa-edge` 路由表、fail-closed | `oa-edge`、`internal/platform/authn` | `stage-1-platform-foundation` | Wave 1 |
| `tenancy-workspaces` | 能力 | 组织、工作区、成员关系、项目、项目资源绑定、租户边界 | `internal/modules/tenancy` | foundation | Wave 1 |
| `ontology-definition` | 能力 | Object Type、Property、Link Type、Dataset Binding | `internal/modules/ontology` | foundation | Wave 1 |
| `dataset-catalog` | 能力 | Dataset、Branch、Transaction、View、S3 文件元数据 | `internal/modules/datasets` | foundation | Wave 1 |
| `connector-catalog` | 能力 | Connector 定义、配置、同步任务、Meltano/Kafka Connect 适配边界 | `internal/modules/connectors`、`internal/modules/ingestion` | foundation | Wave 1 |
| `pipeline-authoring` | 能力 | Pipeline 定义、版本、运行入口、Kestra 契约适配（Stage 1 不接真实 Kestra） | `internal/modules/pipelines` | foundation | Wave 1 |
| `frontend-core` | 能力 | Refine resource-first + 薄页面：登录、Projects、Ontology Manager、Object Explorer、Datasets、Queries | `web/` | foundation、OpenAPI/生成客户端 | Wave 1 |
| `object-graph` | 能力 | 对象/链接 JSONB、对象查询、Meilisearch 索引边界 | `internal/modules/objects` | `ontology-definition` | Wave 2 |
| `query-surface` | 能力 | 保存查询、查询执行、真实单节点 Trino 适配 | `internal/modules/query` | `dataset-catalog`、`object-graph` | Wave 2 |
| `action-orchestration` | 能力 | Action Type、校验/预演、Temporal adapter 骨架（不实现真实动作执行） | `internal/modules/actions` | `ontology-definition` | Wave 2 |
| `stage-1-core-integration` | Stage 支撑 | 路由表合并、wire types、全栈 E2E、Compose/镜像、切换与清理 | `cmd`、`internal/app`、`internal/platform/httpserver`、`deploy/` | 所有模块 change | Wave 3 |

## 6. 多 Agent 并行策略

### 6.1 并行原则

1. **先冻结契约**：`stage-1-platform-foundation` 中先校验并接入 `proto/ontoaxis/v1`（21 个 proto 文件、1,272 行）、OpenAPI、数据库 schema 和路由表；所有 agent 只依赖契约，不依赖其他 agent 的代码。
2. **按目录隔离**：每个能力 change 只写自己的 `internal/modules/<module>/`、migration 目录和 `openspec/changes/<change>/`；不得修改其他模块目录。
3. **共享文件单一所有权**：`cmd/ontoaxis-core/main.go`、`internal/app`、`internal/platform/httpserver/router.go`、`go.mod`、`deploy/compose`、`web/` 入口由指定 owner 修改；其他 agent 只提交 patch 或请求集成。
4. **单向依赖**：模块之间只能通过 `service` 接口或 API 契约调用；禁止跨模块直接读表。
5. **小步可验证**：每个 change 至少交付一个独立可跑的垂直切片、单元测试和一个 fixture；不把多个模块塞进一个 change。

### 6.2 并行波次

- **Wave 0（串行，1 个 agent）**：`stage-1-platform-foundation`
  - 产出：Go module、`internal/app`、`internal/platform`、`cmd`、配置/DB/健康检查、契约生成链、路由骨架、CI、`deploy/compose` 骨架。
- **Wave 1（并行，7 个 agent）**：`identity-edge`、`tenancy-workspaces`、`ontology-definition`、`dataset-catalog`、`connector-catalog`、`pipeline-authoring`、`frontend-core`（frontend 可用 mock/generated client 先行）。
- **Wave 2（并行，3 个 agent）**：`object-graph`、`query-surface`、`action-orchestration`。
- **Wave 3（串行，1 个集成 agent）**：`stage-1-core-integration`，跑全量 E2E、修复跨模块问题、生成最终路由与发布镜像。

### 6.3 Agent 交付包

每个 change 的 agent 必须收到同一组输入：

- 本 Stage brief、对应 change 的 `proposal.md` / delta specs / `tasks.md`；
- `ARCHITECTURE.md` 第 3、5、6、9、16、17 章；
- 冻结的 API 契约与数据库 schema；
- 自己负责的目录范围和禁止修改的共享文件清单。

## 7. 安全、数据与证据

- 认证/授权默认 fail-closed；禁止 `AUTH_DISABLED`、默认 Principal 或伪造身份；
- 未注册路由保持 404；健康端点不是匿名业务入口；
- 本 Stage 使用合成 fixture，不使用真实客户数据；
- 每个 AC 的证据必须是可复核的测试输出、运行记录或契约检查，不允许只写“代码已写”；
- 所有构建/测试结果保存在报告或 CI 日志中，失败必须如实记录。

## 8. 退出条件

1. 所有必需 changes 已归档，Stage AC 全部有证据或明确标记未覆盖；
2. `openspec validate --all --strict` 通过；
3. `go build ./...`、`go test ./...`、前端 check/build/test、Playwright E2E、`buf lint` 与 `buf breaking` 通过；
4. 本地 Compose 一键起栈并完成核心 E2E；
5. Stage 报告、架构文档同步、风险与遗留项记录完成；
6. 负责人确认 Stage 退出；不自动归档、不自动宣布完成。

## 9. 已确认决策（2026-11-22）

> 以下决策已由负责人确认；不代表 Stage 1 已批准或已获实施授权。

1. **Go module 路径与 CI 平台**：module 使用 `github.com/klb3713/ontoaxis`，CI 使用 GitHub Actions；CI 首版覆盖 `go test`、`golangci-lint`、`buf lint`、前端 `pnpm check/build` 与镜像构建。若未来迁移组织账号，先建空组织仓库再初始化 module。
2. **Keycloak 租户模型**：采用 **单 realm + Keycloak Organizations**。租户隔离由 Organization membership + JWT tenant claim + 应用层 tenant filter 保证；仅强监管物理隔离租户使用独立 realm sharding。
3. **外部依赖接入深度**：**Trino 接真实单节点实例**；**Kestra 与 Temporal 先接契约适配器**，默认使用内存/HTTP adapter，真实实例在 integration profile 和 Stage 2 接入。
4. **`oa-web` 页面结构**：采用 **Refine resource-first + 薄页面**，不复制旧页面结构；通用 CRUD 由 Refine resource 生成，核心页面做轻量定制。
5. **Stage 1 的 `actions` 范围**：只交付 **Action Type CRUD + 校验/预演 + Temporal adapter 骨架**，不实现真实动作执行；完整执行放 Stage 2 的 `action-orchestration` change，`S1-AC-09` 只验证 Action Type 契约与 adapter 可替换性。
6. **模块边界与数据所有权**：业务模块固定为 `tenancy`、`connectors`、`ingestion`、`datasets`、`pipelines`、`query`、`ontology`、`objects`、`actions` 九个；`Project`/`ProjectResource` 唯一 owner 是 `tenancy`，`ontology` 只保存 object type/property/link type 与 dataset binding，不读写 project 表（依据 `ARCHITECTURE.md` 3.4.1/3.4.2）。
7. **契约基线**：`proto/ontoaxis/v1` 为唯一契约源，21 个 proto 文件（1,272 行）已定稿；本 Stage 不新建 `proto/ontoaxis/core/v1`，也不手改 `gen/`、`openapi/`、`web/src/generated` 生成物。

## 10. 关联文档

- [`ARCHITECTURE.md`](../../ARCHITECTURE.md)
- [`doc/workflow/RUNBOOK.md`](../workflow/RUNBOOK.md)
- [`stage-1-change-map.md`](stage-1-change-map.md)
- [`openspec/config.yaml`](../../openspec/config.yaml)
