# 授权边界（OpenFGA + Cedar）

> 状态：定稿（负责人已确认、安全评审通过，2026-11-22）。本文是 `authorization-openfga-cedar` change 的实施前置契约。<br>
> 适用范围：Stage 2 的 `ontoaxis-core` 业务 API、`oa-edge` 的认证/租户边界、PostgreSQL 关系事实与授权审计事件。<br>
> 关联：[`ARCHITECTURE.md`](../../ARCHITECTURE.md) §6.2、§6.3、§15.3；[`stage-2-ontoaxis-runtime-brief.md`](../stages/stage-2-ontoaxis-runtime-brief.md) S2-AC-03；[`stage-2-change-map.md`](../stages/stage-2-change-map.md)；[`observability-baseline.md`](observability-baseline.md)。

## 1. 目的与范围

### 1.1 目标

本文冻结 Stage 2 授权实现的四个契约面：

1. **OpenFGA 关系模型**：主体、租户、组织/工作空间/项目层级、资源归属与所有者关系，以及这些关系在 v1 如何被判定。
2. **Cedar 策略集**：动作分组、角色矩阵、租户不变量、租户管理范围与资源状态约束。
3. **默认拒绝**：缺失策略、缺失属性、依赖故障、配置错误、未知动作等所有失败路径的判定与 HTTP 行为。
4. **审计事件字段**：授权决策、策略加载与 Explain 的结构化事件、脱敏规则、写入失败语义。

本文是 `authorization-openfga-cedar` 的输入，不是实现说明。实现可以决定包内类型、函数签名和存储细节，但 MUST NOT 扩大或重叠本文定义的职责，也不得在实现中临时增加动作组、角色、默认放行或审计字段。

### 1.2 非目标

以下事项明确不在本边界内，不得由 `authorization-openfga-cedar` 顺带实现：

| 非目标 | 原因 | 后续归属 |
| --- | --- | --- |
| 项目级/工作空间级资源收窄 | 当前资源创建 API 只带租户，不加载 `core.project_resources`；v1 保持租户内成员可读写 | 后续 least-privilege change，先回填绑定元组 |
| 对象 markings 强制 | `object_data.objects.markings` 列存在，但 domain/proto/repo 未读取，无法在 API 层可靠判定 | 后续 marking/declassification change |
| OpenFGA 持久元组与 outbox 投影 | v1 使用请求内上下文元组，PostgreSQL 是事实源 | 当出现无法按请求解析的事实时再引入 |
| Keycloak → OpenFGA 成员同步 | JWT 已携带经 IdP 验证的租户/组织/工作空间声明 | 仅当需要 IdP 之外的细粒度关系时再评估 |
| 数据库审计表与审计查询 API | v1 审计是 append-only 结构化日志；`audit.proto` 保持可选 | `production-delivery`/后续 change |
| 生产 OpenFGA 服务、Helm/ArgoCD/Compose 供给 | `deploy/` 由 `production-delivery` 所有 | `production-delivery` |
| `dataset.dataset_permissions` 的读写 | 该表自 dataset-catalog 起无 API、无运行时读取；双源会破坏单一判定点 | 后续 data-permission change |
| 跨租户平台管理后台 | v1 的 platform-admin 仍受 `org_ids` 约束，只能选择已加入的租户 | Stage 3+ |
| UI 权限提示与菜单裁剪 | 前端不得作为授权真源 | `web/` 所属 change |

### 1.3 术语

| 术语 | 含义 |
| --- | --- |
| 主体（principal） | 经 `oa-edge`/`authn` 验证的 JWT subject；v1 只有 `user` |
| 租户（tenant） | Keycloak Organization 对应的隔离边界；JWT `tenant_id` |
| 关系（relationship） | OpenFGA 中“谁与哪个对象有什么关系”的元组事实 |
| 属性（attribute） | Cedar 实体属性与请求上下文，例如角色、租户、资源状态 |
| 判定（decision） | 一次授权请求的 allow/deny/error，带唯一 decision id |
| 事实（fact） | 从已验证 JWT 或租户内业务行加载的、不可由客户端伪造的数据 |

### 1.4 规范用语

- **MUST / MUST NOT**：契约要求；实现偏离即视为缺陷。
- **SHALL**：与 MUST 同义，用于规格条款。
- **SHOULD**：默认应遵守；偏离需在 change 设计中说明理由。
- **MAY**：可选实现，不得改变默认拒绝语义。

## 2. 职责边界

### 2.1 组件职责

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| `oa-edge` | JWT 签名/iss/aud/exp 校验、租户头一致性、路由级粗粒度准入、401/403/404/405/429 | 对象级关系、角色、资源状态、业务数据 |
| `internal/platform/authn` | claims → `TenantContext`、内部头注入、租户选择合法性、401/403 | 策略求值、角色权限、资源归属 |
| `internal/platform/authz` | 唯一授权判定点；上下文元组构造、OpenFGA Check、Cedar 求值、决策审计、Explain | 业务读写、身份签发、持久化业务关系 |
| 业务模块 `*/api` | 声明 `Action` 与 `Resource` 事实；调用授权守卫；拒绝时不产生副作用 | 自写角色判断、直接查 OpenFGA、读 `dataset_permissions` |
| PostgreSQL | 资源租户、父子关系、所有者、状态等事实源 | 策略、角色、OpenFGA 元组存储 |
| OpenFGA | 主体—租户—组织/工作空间/项目—资源的关系图 | 角色、属性、状态、审计 |
| Cedar（`cedar-go`，进程内） | 角色矩阵、租户不变量、租户管理范围、资源状态 | 成员关系图、数据库访问 |
| 审计 sink | append-only 结构化 JSON；策略加载与决策留痕 | 查询 API、可变存储、采样 |

### 2.2 判定链

```text
Client
  │  JWT + 可选 X-Tenant-Id
  ▼
oa-edge（JWT/租户/路由粗判，401/403/404/405/429）
  ▼
authn（claims → TenantContext；内部头；租户选择）
  ▼
业务 handler（decode/validate → 租户内加载 Resource 事实）
  ▼
authz（构造上下文元组 → OpenFGA Check → Cedar 求值 → 审计决策）
  ▼
业务 handler（allow 才执行副作用；deny/error 直接返回）
```

**单一判定点**：业务模块不得在 authz 之外做“角色是否足够”“是否同租户”“是否管理员”的判断。允许的检查只有输入合法性、资源存在性与租户内查询过滤；这些不是授权决策。`dataset_permissions` 在 v1 不得被读取或写入。

**纵深不变量**：OpenFGA 关系链保证主体属于资源租户，Cedar 再次比较 `principal.tenant_id` 与 `resource.tenant_id`。这是防御元组构造错误的不变量，不是第二次授权决策，也不构成“重复职责”。

### 2.3 禁止事项

- 禁止 `AUTH_DISABLED`、`AUTHZ_DISABLED`、`AUTHZ_FAIL_OPEN`、默认 Principal、匿名兜底、仅记录不拒绝的 shadow 模式。
- 禁止业务模块自行调用 OpenFGA/Cedar，或把角色名写进 SQL/HTTP 判断。
- 禁止把 `X-Tenant-Id` 当作授权事实；它只能经 authn 校验后进入 `TenantContext`。
- 禁止把角色元组写入 OpenFGA，或把成员关系写进 Cedar 实体属性。
- 禁止在 v1 写持久 OpenFGA 元组；专用 store 必须为零关系元组。
- 禁止把 token、Cookie、请求体、SQL、连接串、邮箱、姓名、原始 IP 写入审计或 trace。
- 禁止用“未找到”掩盖跨租户存在性：跨租户查询必须在租户过滤后返回 404，而不是先查全库再判 403。

## 3. 主体、租户与角色事实

### 3.1 主体类型

| 类型 | OpenFGA 表示 | v1 状态 | 说明 |
| --- | --- | --- | --- |
| 用户 | `user:<sub>` | 启用 | JWT `sub` 必须是 UUID；`authn` 已校验 |
| 组 | `group:<id>` / `group:<id>#member` | 保留，不启用 | JWT 无组声明；出现组主体时默认拒绝 |
| 服务账号 | `service_account:<id>` | 保留，不启用 | v1 无服务账号签发与声明；不得用 user 冒充 |

v1 的 `audit.actor.type` 只允许 `user` 或 `system`；`service` 保留给后续服务账号。系统主体只用于策略加载事件，不参与业务 Check。

### 3.2 租户事实

- 租户唯一来源是 JWT `tenant_id`，由 Keycloak Organization membership 签发；`authn` 已将其校验为 UUID 并写入 `TenantContext.TenantID`。
- `X-Tenant-Id` 只有在与 JWT 一致时通过；否则仅 platform-admin 且目标租户在 `org_ids` 内可通过。该规则由 `authn` 执行，authz 不重复实现。
- authz 的 `tenant_id` 一律取自 `TenantContext`；不得从请求体、query、路径或数据库行反推“当前租户”。
- 资源行的 `tenant_id` 必须等于 `TenantContext.TenantID`；不等时按跨租户处理：查询阶段返回 404，禁止进入 allow 分支。

### 3.3 角色事实

角色来自已验证 JWT `roles`，`authn` 已过滤未知值、去重并排序。v1 只识别：

| 角色 | 语义 |
| --- | --- |
| `platform-admin` | 平台管理员；仍只能操作 `org_ids` 内的目标租户 |
| `tenant-admin` | 租户管理员；租户内全部动作，含审计读取与租户级管理 |
| `workspace-admin` | 工作空间管理员；工作空间/项目级管理，不含租户级管理与审计读取 |
| `member` | 租户成员；租户内读写、调用与删除普通资源 |
| `viewer` | 只读成员；只能执行 read 组动作 |

- 多角色取并集；未知角色已被 `authn` 丢弃，空角色集合默认拒绝所有业务动作。
- 角色只进入 Cedar `principal.roles`，不进入 OpenFGA，不写入数据库。
- 角色是 IdP 事实，不由 `core.memberships` 推导；两者不一致时以 JWT 为准，并记录运维告警（不属于本 change 的审计事件）。

### 3.4 成员关系与撤销时延

- `tenant_id`、`org_ids`、`workspace_ids` 均来自 JWT；`core.memberships` 是业务元数据，不是 v1 授权事实源。
- v1 不展开组/服务账号成员；组主体默认拒绝。
- 撤销成员关系后，已签发 access token 在有效期内仍可访问；撤销时延 = access token 剩余有效期（≤15 分钟）+ Keycloak 同步时延。安全评审已接受该时延（2026-11-22）；如需即时撤销，必须引入 token introspection 或关系持久化，属于后续 change。
- `core.memberships` 的增删仍受 `tenancy:membership:add/remove` 授权保护，但其本身不改变 OpenFGA 元组；它影响后续 IdP 同步，不构成本请求内的授权事实。

## 4. OpenFGA 关系模型

### 4.1 建模原则

1. **租户是根**：`tenant` 是唯一根类型；组织、工作空间、项目是租户内层级，不是独立租户。
2. **关系只表达成员与归属**：`member`、`editor`、`owner`、父子边；不表达角色、状态、属性。
3. **资源归属直接挂租户**：dataset/object_type/connector/pipeline/action_type 等直接 `tenant`；object/run/execution/job/schedule 通过父资源继承。
4. **层级只服务租户管理端点**：organization/workspace/project 链在 tenancy 端点使用；普通资源仍走租户直接边，避免依赖未加载的 `project_resources`。
5. **v1 不写持久元组**：所有元组由请求内事实构造，PostgreSQL 是唯一事实源。

### 4.2 模型 DSL（v1）

文件：`policies/openfga/model.fga`。模型版本通过 OpenFGA `model_id` 与文件摘要固定；任何变更都必须提升 model id 并重新评审。

```fga
model
  schema 1.1

type user

type group
  relations
    define member: [user]

type service_account

type tenant
  relations
    define member: [user, group#member, service_account]
    define can_read: member
    define can_write: member
    define can_invoke: member

type organization
  relations
    define tenant: [tenant]
    define member: [user, group#member, service_account] or member from tenant
    define admin: [user, group#member, service_account]
    define can_read: member
    define can_write: member
    define can_invoke: member

type workspace
  relations
    define organization: [organization]
    define member: [user, group#member, service_account] or member from organization
    define admin: [user, group#member, service_account] or admin from organization
    define can_read: member
    define can_write: member
    define can_invoke: member

type project
  relations
    define workspace: [workspace]
    define member: [user, group#member, service_account] or member from workspace
    define editor: [user, group#member, service_account] or member
    define admin: [user, group#member, service_account] or admin from workspace
    define can_read: member
    define can_write: editor
    define can_invoke: editor

type dataset
  relations
    define tenant: [tenant]
    define project: [project]
    define can_read: member from tenant or can_read from project
    define can_write: member from tenant or can_write from project
    define can_invoke: member from tenant or can_write from project

type object_type
  relations
    define tenant: [tenant]
    define project: [project]
    define can_read: member from tenant or can_read from project
    define can_write: member from tenant or can_write from project
    define can_invoke: member from tenant or can_write from project

type object
  relations
    define object_type: [object_type]
    define can_read: can_read from object_type
    define can_write: can_write from object_type
    define can_invoke: can_invoke from object_type

type connector
  relations
    define tenant: [tenant]
    define workspace: [workspace]
    define can_read: member from tenant or member from workspace
    define can_write: member from tenant or member from workspace
    define can_invoke: member from tenant or member from workspace

type ingest_job
  relations
    define connector: [connector]
    define can_read: can_read from connector
    define can_invoke: can_invoke from connector

type pipeline
  relations
    define tenant: [tenant]
    define project: [project]
    define can_read: member from tenant or can_read from project
    define can_write: member from tenant or can_write from project
    define can_invoke: member from tenant or can_write from project

type pipeline_run
  relations
    define pipeline: [pipeline]
    define can_read: can_read from pipeline

type pipeline_schedule
  relations
    define pipeline: [pipeline]
    define can_read: can_read from pipeline
    define can_write: can_write from pipeline

type action_type
  relations
    define tenant: [tenant]
    define project: [project]
    define can_read: member from tenant or can_read from project
    define can_write: member from tenant or can_write from project
    define can_invoke: member from tenant or can_write from project

type action_execution
  relations
    define action_type: [action_type]
    define can_read: can_read from action_type
    define can_invoke: can_invoke from action_type

type saved_query
  relations
    define tenant: [tenant]
    define owner: [user, service_account]
    define can_read: member from tenant
    define can_write: owner

type audit_event
  relations
    define tenant: [tenant]
    define can_read: member from tenant

type operation
  relations
    define tenant: [tenant]
    define can_read: member from tenant
```

**关于 `admin` 关系**：`organization.admin`、`workspace.admin`、`project.admin` 仅为后续持久化关系预留，v1 **不写任何 admin 元组**，Cedar 也不读取它们。v1 的管理员语义完全由 Cedar 角色矩阵表达；不得因为模型里存在 `admin` 就默认某个角色是 admin。架构草图中 `organization.admin: [user] or member` 会让每个成员成为 admin，本文明确不采用该写法。

### 4.3 关系语义与事实源

| 类型.关系 | 含义 | v1 元组来源 | 是否持久化 |
| --- | --- | --- | --- |
| `tenant.member` | 主体属于租户 | JWT `tenant_id` | 否，上下文 |
| `organization.tenant` | 组织所属租户 | `core.organizations.tenant_id` | 否，行加载 |
| `organization.member` | 组织成员 | JWT `org_ids`（直接元组）；v1 另经 `tenant.member` 继承 | 否，上下文 |
| `workspace.organization` | 工作空间所属组织 | `core.workspaces.organization_id` | 否，行加载 |
| `workspace.member` | 工作空间成员 | JWT `workspace_ids`（直接元组）；v1 另经 `organization.member` 继承 | 否，上下文 |
| `project.workspace` | 项目所属工作空间 | `core.projects.workspace_id` | 否，行加载 |
| `project.member` | 项目成员 | 经 workspace/org/tenant 继承；v1 不写项目成员元组 | 否 |
| `dataset.tenant` / `object_type.tenant` / `connector.tenant` / `pipeline.tenant` / `action_type.tenant` / `saved_query.tenant` / `audit_event.tenant` / `operation.tenant` | 资源所属租户 | 业务行 `tenant_id` | 否，行加载 |
| `dataset.project` / `object_type.project` / `pipeline.project` / `action_type.project` | 资源绑定项目 | v1 不写；`core.project_resources` 尚未在资源 handler 加载 | 否，保留 |
| `object.object_type` | 对象所属对象类型 | `object_data.objects.object_type_id` | 否，行加载 |
| `connector.workspace` | 连接器所属工作空间 | v1 不写；连接器当前只按租户 | 否，保留 |
| `ingest_job.connector` | 同步任务所属连接器 | `ingestion.sync_jobs.connector_id` | 否，行加载 |
| `pipeline_run.pipeline` | 运行所属 Pipeline | `pipeline.runs.pipeline_id` | 否，行加载 |
| `pipeline_schedule.pipeline` | 调度所属 Pipeline | `pipeline.schedules.pipeline_id` | 否，行加载 |
| `action_execution.action_type` | 执行所属动作类型 | `action.action_executions.action_type_id` | 否，行加载 |
| `saved_query.owner` | 保存查询的所有者 | `query.saved_queries.owner_id` | 否，行加载 |

**租户继承链**：`project.member ← workspace.member ← organization.member ← tenant.member`。因此 tenancy 端点只要带上 `project.workspace`、`workspace.organization`、`organization.tenant` 三条边和 `tenant.member`，就能判定租户成员；v1 的项目端点因此仍是租户内成员可管理。`org_ids`/`workspace_ids` 的直接元组不改变 v1 的可见性：由于继承，任何租户成员都是所有组织/工作空间/项目的成员。这是 Stage 1 租户内行为的保留；后续要收窄项目权限，只需停止 `organization.member` 的 `or member from tenant` 继承并回填组织/工作空间成员元组，模型其余部分不变。

### 4.4 上下文元组构造

每个请求由 `internal/platform/authz` 构造，不由业务模块拼接。元组方向为 `object#relation@subject`。

固定元组：

```text
tenant:<tenant_id>#member@user:<sub>
organization:<org_id>#member@user:<sub>        // 当 org_id ∈ JWT org_ids
workspace:<workspace_id>#member@user:<sub>     // 当 workspace_id ∈ JWT workspace_ids
```

资源元组按资源类型追加：

| 资源 | 追加的上下文元组 | 事实来源 |
| --- | --- | --- |
| Tenant | 无（仅固定元组） | `TenantContext` |
| Organization | `organization:<id>#tenant@tenant:<tenant_id>` | 行 `tenant_id` |
| Workspace | `workspace:<id>#organization@organization:<org_id>` + 组织边 | 行 `organization_id` |
| Project | `project:<id>#workspace@workspace:<ws_id>` + 工作空间/组织边 | 行 `workspace_id` |
| Dataset / ObjectType / Connector / Pipeline / ActionType / SavedQuery / Operation / AuditEvent | `<type>:<id>#tenant@tenant:<tenant_id>` | 行 `tenant_id` |
| Object | `object:<id>#object_type@object_type:<type_id>` + object_type 的租户边 | 行 `object_type_id` + 同租户 |
| IngestJob | `ingest_job:<id>#connector@connector:<connector_id>` + connector 的租户边 | 行 `connector_id` |
| PipelineRun | `pipeline_run:<id>#pipeline@pipeline:<pipeline_id>` + pipeline 的租户边 | 行 `pipeline_id` |
| PipelineSchedule | `pipeline_schedule:<id>#pipeline@pipeline:<pipeline_id>` + pipeline 的租户边 | 行 `pipeline_id` |
| ActionExecution | `action_execution:<id>#action_type@action_type:<type_id>` + action_type 的租户边 | 行 `action_type_id` |
| SavedQuery | `saved_query:<id>#owner@user:<owner_id>` + 租户边 | 行 `owner_id` |

规则：

- 每条 `tenant` 边只能使用**该行**的 `tenant_id` 作为 subject，不能使用请求租户；请求租户与行租户不一致时在加载阶段返回 404。
- `owner` 边只允许 `user` 或保留的 `service_account`；`owner_id` 为空时不允许写/删，且不得回退到租户成员放行。
- 上下文元组数量上限为 8；超过时判定为内部构造错误，返回 503 并审计 `error`，不得截断后继续。
- 只加入当前资源祖先链上的 `org_ids`/`workspace_ids` 直接元组；不得把 JWT 中所有 org/workspace 声明展开为元组。直接元组只用于未来收窄，不是 v1 可见性的必要条件。
- 禁止从客户端传入祖先 ID 而不校验；所有祖先 ID 必须来自租户内加载的行。
- 禁止把 `group#member` 元组用于 v1，因为 JWT 没有组声明；模型保留类型仅为未来兼容。

### 4.5 存储、模型与启动校验

- OpenFGA 使用**专用 store**；v1 的业务关系元组数量 MUST 为 0。store 中存在任何元组时，启动失败（防止隐藏授权）。
- 模型由部署流水线上传，应用只读；应用不得调用 `WriteAuthorizationModel` 或写元组。
- 配置必须提供 `store_id` 与 `model_id`。应用启动时读取模型并校验：模型存在、类型/关系名与本文一致、`model_id` 与配置一致。任一失败则启动失败（配置错误），不以“未就绪但继续服务”降级。
- `model.fga` 的文件摘要进入启动审计；部署流水线必须保证上传的模型与文件摘要对应。应用无法从 API 证明服务器模型等于本地 DSL，因此 model id 是部署契约，摘要用于审计比对。
- 模型变更必须新增 model id，禁止原地覆盖；旧 model id 保留以支持回滚。模型变更需更新本文并重新安全评审。
- v1 **不启用 outbox**：没有持久元组写入，就没有元组投影。`jobs.outbox` 仍由后续持久化投影 change 使用；不得为了“与架构一致”而提前写空 outbox。

### 4.6 为什么 v1 采用上下文元组

| 方案 | 优点 | 缺点 / 风险 | 结论 |
| --- | --- | --- | --- |
| 上下文元组（本文） | PostgreSQL 单一事实源；无同步延迟；无脏元组；无需 backfill；不跨 change 所有权 | 每次 Check 需携带 2–5 条元组；列表过滤不能用 ListObjects 直接完成 | **采用** |
| 持久元组 + outbox | 支持 ListObjects、跨服务查询；Check 请求更小 | 需要写路径、投影消费者、回填、脏元组清理；v1 资源 API 不加载项目绑定，投影不完整 | 延后 |
| Keycloak → OpenFGA 同步 | 关系可由 IdP 管理 | 引入双向同步与撤销时延；与 JWT claims 重复 | 不采用 |
| 只做租户 SQL 过滤，不引入 OpenFGA | 简单 | 无法表达 owner/项目/未来细粒度；不满足 S2-AC-03 的关系判定要求 | 不采用 |

上下文元组的正确性由“事实来自可信上下文或租户内行”保证；任何无法按请求解析的事实都不得用默认 allow 补齐，而应拒绝并记录。

### 4.7 未来演进

1. **项目级收窄**：当资源 handler 能加载 `core.project_resources` 时，追加 `project:<id>#...` 绑定元组，并移除 `organization.member` 的租户继承；需要成员回填与迁移测试。
2. **持久化投影**：当出现无法按请求加载的事实（跨服务 Check、批量 ListObjects、审计关系变更）时，引入 outbox + 投影消费者；元组写入必须与业务事务同源。
3. **组/服务账号**：当 IdP 签发组或服务账号声明后，启用 `group#member`/`service_account` 元组，并增加对应的撤销测试。
4. **ListObjects**：只有在持久元组可用后才用于列表过滤；v1 列表一律“租户 SQL 过滤 + 逐行 Cedar（如需要）”，不得用未经验证的 ListObjects 结果放行。

## 5. Cedar 策略集

### 5.1 策略位置与加载

| 文件 | 内容 |
| --- | --- |
| `policies/openfga/model.fga` | OpenFGA 模型（§4.2） |
| `policies/platform/schema.cedar.json` | Cedar schema（§5.2，实现时生成并由 `cedar-go` 校验） |
| `policies/platform/tenant-isolation.cedar` | 租户不变量 |
| `policies/platform/role-matrix.cedar` | 动作组 × 角色矩阵 |
| `policies/platform/tenancy-scope.cedar` | 租户级管理范围 |
| `policies/platform/resource-state.cedar` | Pipeline/Connector/Action/Ingestion/Execution 状态约束 |

- 策略随二进制嵌入（`go:embed`），启动时编译；v1 不支持热加载。策略变更通过重新部署生效。
- 编译失败、schema 与策略不匹配、动作目录缺组、任一动作组无 permit：启动失败。
- 策略文件按字典序加载；同一文件内按出现顺序编号。策略 ID 为 `<相对路径>#<序号>`，例如 `policies/platform/role-matrix.cedar#2`。
- 策略 ID 一经发布即稳定：只允许追加；不得重排、删除后复用编号。删除策略必须发布新 bundle，并在审计中保留旧 ID 的历史含义。

### 5.2 Cedar Schema

实体与属性（所有资源实体都带 `tenant_id`；`Tenant.tenant_id` 等于其自身 id，用于统一租户不变量）：

| 实体 | 属性 | 来源 |
| --- | --- | --- |
| `User` | `tenant_id: String`、`user_id: String`、`roles: Set<String>`、`org_ids: Set<String>`、`workspace_ids: Set<String>` | `TenantContext` + claims |
| `Tenant` | `tenant_id: String` | `TenantContext.TenantID` |
| `Organization` | `tenant_id: String` | 行 |
| `Workspace` | `tenant_id: String`、`organization_id: String` | 行 |
| `Project` | `tenant_id: String`、`workspace_id: String` | 行 |
| `Dataset` | `tenant_id: String` | 行 |
| `ObjectType` | `tenant_id: String` | 行 |
| `Object` | `tenant_id: String`、`object_type_id: String` | 行 |
| `Connector` | `tenant_id: String`、`status: String` | 行 |
| `IngestJob` | `tenant_id: String`、`connector_id: String`、`status: String` | 行 |
| `Pipeline` | `tenant_id: String`、`status: String` | 行 |
| `PipelineRun` | `tenant_id: String`、`pipeline_id: String` | 行 |
| `PipelineSchedule` | `tenant_id: String`、`pipeline_id: String` | 行 |
| `ActionType` | `tenant_id: String`、`status: String` | 行 |
| `ActionExecution` | `tenant_id: String`、`action_type_id: String`、`status: String` | 行 |
| `SavedQuery` | `tenant_id: String`、`owner_id: String` | 行 |
| `AuditEvent` | `tenant_id: String` | 行/上下文 |
| `Operation` | `tenant_id: String` | 行 |

动作组（Cedar action groups）：

| 组 | 成员动作（按 §5.3 规则） | 语义 |
| --- | --- | --- |
| `read` | 所有读/列/预览/版本/校验/查询执行/Explain | 只读 |
| `write` | 创建、更新、绑定、批量写 | 租户内写 |
| `delete` | 删除普通资源 | 租户内删 |
| `invoke` | 运行、触发、同步、测试、取消、重试、物化 | 触发副作用 |
| `admin` | 租户/工作空间/项目管理、成员与资源绑定、发布 | 管理 |
| `audit` | 审计读取、用户列表 | 租户级只读管理 |

schema 的 `appliesTo.resourceTypes` 必须覆盖目录中该动作实际使用的资源类型；不得用 `Any`/未声明类型绕过类型检查。schema 不得包含本文未列出的实体属性。

schema 的 `actions` 必须为 §5.3 的每个**具体动作**声明一条，并通过 `memberOf` 恰好归入 `read`/`write`/`delete`/`invoke`/`admin`/`audit` 中的一个组；组动作只用于聚合，authz 不直接调用组动作。具体动作的 `appliesTo.principalTypes` 必须是 `User`，`resourceTypes` 必须与 §5.3 资源选择表一致；组的 `resourceTypes` 可为其成员的并集。示例如下（完整文件由 change 生成，并由 `cedar-go` 编译校验）：

```json
{
  "read": {
    "appliesTo": {
      "principalTypes": ["User"],
      "resourceTypes": ["Dataset", "ObjectType", "Object", "Tenant"]
    }
  },
  "dataset:read": {
    "memberOf": [{"id": "read"}],
    "appliesTo": {
      "principalTypes": ["User"],
      "resourceTypes": ["Dataset"]
    }
  }
}
```

`memberOf` 使用动作引用对象数组（`{"id": "read"}`）；cedar-go `v1.9.3` 的 schema 解析不接受字符串数组。

启动校验必须交叉检查 catalog、schema 与策略：目录动作集合等于 schema 具体动作集合（排除组动作）；每个具体动作恰好属于一个组；每个组至少被一个 `permit` 覆盖；schema 不得存在未登记动作。任一不一致即启动失败。

### 5.3 动作命名与分组

**命名**：`<domain>:<object>:<verb>` 或 `<domain>:<verb>`，全小写 snake_case；例如 `tenancy:membership:add`、`dataset:read`、`pipeline:invoke`、`action_execution:cancel`、`saved_query:delete`、`audit:read`。

**按动词分组**：

| 动词 | 组 | 例外 |
| --- | --- | --- |
| `create` | `write` | `tenancy:organization:create`、`tenancy:workspace:create`、`tenancy:project:create` → `admin`；`ingestion:create` → `invoke` |
| `read` / `list` / `get` / `watch` / `preview` / `versions` / `validate` | `read` | 无 |
| `update` | `write` | `tenancy:organization:update`、`tenancy:workspace:update`、`tenancy:project:update` → `admin` |
| `delete` / `remove` | `delete` | `tenancy:organization:delete`、`tenancy:workspace:delete`、`tenancy:project:delete`、`tenancy:membership:remove`、`tenancy:project_resource:remove` → `admin` |
| `add` | `admin` | 无 |
| `publish` | `admin` | 无 |
| `run` / `invoke` / `sync` / `test` / `infer_schema` / `materialize` / `cancel` / `retry` | `invoke` | `pipeline:publish` 已按 `publish` 归 admin |
| `execute` / `explain`（查询） | `read` | `authorization:explain` → `admin` |
| `check` / `list`（授权服务） | `admin` | 无 |
| `audit:read` / `identity:list` | `audit` | 无 |
| `operation:read` | `read` | 无 |

**显式例外**：

| 动作 | 组 | 原因 |
| --- | --- | --- |
| `tenancy:organization:create` | `admin` | 创建租户内组织是租户级管理 |
| `tenancy:organization:update` | `admin` | 修改组织是租户级管理 |
| `tenancy:workspace:create` | `admin` | 在组织下创建工作空间 |
| `tenancy:workspace:update` | `admin` | 修改工作空间是管理动作 |
| `tenancy:project:create` | `admin` | 在工作空间下创建项目 |
| `tenancy:project:update` | `admin` | 修改项目是管理动作 |
| `tenancy:organization:delete` | `admin` | 删除组织是租户级管理 |
| `tenancy:workspace:delete` | `admin` | 删除工作空间 |
| `tenancy:project:delete` | `admin` | 删除项目 |
| `tenancy:membership:add/remove` | `admin` | 成员管理 |
| `tenancy:project_resource:add/remove` | `admin` | 项目资源绑定 |
| `pipeline:publish` | `admin` | 发布是管理动作 |
| `object:batch_upsert` | `write` | 批量写不是“触发” |
| `ontology:binding:write` | `write` | 绑定/解绑是写 |
| `ontology:binding:materialize` | `invoke` | 物化触发副作用 |
| `action_type:validate/preview` | `read` | 不产生执行 |
| `action_type:run` | `invoke` | 产生执行 |
| `action_execution:cancel/retry` | `invoke` | 改变执行状态 |
| `action_execution:approve` | `admin` | 审批决定是管理动作 |
| `ingestion:create/cancel` | `invoke` | 触发/停止同步 |
| `ingestion:watch` | `read` | 只读流 |
| `query:execute/explain` | `read` | 只读 SQL |
| `saved_query:create` | `write` | 保存查询 |
| `saved_query:delete` | `delete` | 删除保存查询 |
| `identity:list` | `audit` | 租户级只读管理 |
| `authorization:check/list/explain` | `admin` | 权限内省 |

**动作目录**：每个业务 RPC 必须声明唯一动作；同一动作可覆盖多个 RPC（如 Get/List）。动作全集由 change 的 `internal/platform/authz/catalog.go` 枚举，并且 MUST 与本节规则一致。新增/改名动作必须同步更新本文；不得因“先实现再说”而使用未登记动作。未登记动作在运行时一律 deny。

当前模块的动作命名空间：

| 模块 | 动作 |
| --- | --- |
| tenancy | `organization:{create,read,update,delete}`、`workspace:{create,read,update,delete}`、`membership:{add,read,remove}`、`project:{create,read,update,delete}`、`project_resource:{add,read,remove}` |
| datasets | `dataset:{create,read,update,delete,preview,versions}` |
| ontology | `object_type:{create,read,update,delete}`、`property:{create,read,update,delete}`、`link_type:{create,read,update,delete}`、`binding:{write,materialize}` |
| objects | `object:{create,read,update,delete,batch_upsert}`、`object_link:{read,create,delete}` |
| connectors | `connector:{create,read,update,delete,test,sync,infer_schema}` |
| ingestion | `ingestion:{create,read,watch,cancel}` |
| pipelines | `pipeline:{create,read,update,delete,publish,invoke}`、`pipeline_run:read`、`pipeline_schedule:{read,write,delete}` |
| actions | `action_type:{create,read,update,delete,validate,preview,run}`、`action_execution:{read,cancel,retry,approve}` |
| query | `query:{execute,explain}`、`saved_query:{create,read,delete}` |
| audit | `audit:read` |
| authorization | `authorization:{check,list,explain}` |
| identity | `identity:list`；`identity:self` 不走资源授权 |
| operation | `operation:read` |

`identity:get_me` 与 `identity:get_tenant_context` 只要求认证，不产生资源授权决策；它们仍受 authn 的 401/403 约束，且不得返回其他租户数据。

**动作组 → OpenFGA 关系**（authz 对每个动作只检查一个关系）：

| 组 | 关系 |
| --- | --- |
| `read` | `can_read` |
| `audit` | `can_read` |
| `write` | `can_write` |
| `delete` | `can_write` |
| `admin` | `can_write` |
| `invoke` | `can_invoke` |

`admin`/`delete` 复用 `can_write`，不新增 `can_admin`/`can_delete`：成员关系是管理资格的前提，角色与范围由 Cedar 收紧。`explain` 属于 `admin` 组，因此检查资源上的 `can_write`；`authorization:check/list` 同理。若未来项目收窄使 `can_write` 不再覆盖管理员，必须引入专用关系、提升模型版本并修订本文，不得在代码里加特例。

**资源选择**（同一动作覆盖多个 RPC 时，按实际资源类型传入；schema 的 `appliesTo.resourceTypes` 必须包含下列类型）：

| 动作 | Resource 类型 | 说明 |
| --- | --- | --- |
| `tenancy:organization:create` / `:read`(List) | `Tenant` | 创建/列出租户内组织 |
| `tenancy:organization:{read,update,delete}` | `Organization` | 单组织读取/修改/删除 |
| `tenancy:workspace:create` / `:read`(List) | `Organization` | 在组织下创建/列出工作空间 |
| `tenancy:workspace:{read,update,delete}` | `Workspace` | 单工作空间 |
| `tenancy:project:create` / `:read`(List) | `Workspace` | 在工作空间下创建/列出项目 |
| `tenancy:project:{read,update,delete}` | `Project` | 单项目 |
| `tenancy:membership:{add,read,remove}` | `Organization` | 当前 API 仅支持 org 级；Workspace 关系保留给未来，不接 API |
| `tenancy:project_resource:{add,read,remove}` | `Project` | 项目资源绑定 |
| `ontology:object_type:create` | `Tenant` | 创建对象类型 |
| `ontology:object_type:{read,update,delete}`、`property:*`、`link_type:*`、`binding:*` | `ObjectType` | 属性/链接类型/绑定以父对象类型为资源 |
| `object:create` / `object:batch_upsert` | `ObjectType` | 创建/批量写对象 |
| `object:{read,update,delete}`、`object_link:*` | `Object` | 对象及对象链接；链接另校验目标对象同租户 |
| `connector:create` | `Tenant` | 创建连接器 |
| `connector:{read,update,delete,test,sync,infer_schema}` | `Connector` | 单连接器 |
| `ingestion:create` | `Connector` | 触发连接器同步 |
| `ingestion:{read,watch,cancel}` | `IngestJob` | 单同步任务 |
| `dataset:create` | `Tenant` | 创建数据集 |
| `dataset:{read,update,delete,preview,versions}` | `Dataset` | 单数据集 |
| `pipeline:create` | `Tenant` | 创建 Pipeline |
| `pipeline:{read,update,delete,publish,invoke}` | `Pipeline` | 单 Pipeline |
| `pipeline_run:read` | `PipelineRun` | 单运行 |
| `pipeline_schedule:{read,write,delete}` | `PipelineSchedule` | 单调度 |
| `action_type:create` | `Tenant` | 创建动作类型 |
| `action_type:{read,update,delete,validate,preview,run}` | `ActionType` | 单动作类型 |
| `action_execution:{read,cancel,retry,approve}` | `ActionExecution` | 单执行 |
| `query:{execute,explain}` | `Tenant` | 租户级 SQL 执行 |
| `saved_query:create` | `Tenant` | 创建保存查询 |
| `saved_query:{read,delete}` | `SavedQuery` | 单保存查询 |
| `audit:read` / `identity:list` / `authorization:{check,list,explain}` | `Tenant` | 租户级只读管理/内省 |
| `operation:read` | `Operation` | 单操作 |

列表 RPC 按父作用域选择资源，不对每行做 Check：带父过滤的 `workspace:list` 用 `Organization`，`project:list` 用 `Workspace`；无父过滤时 `workspace:list`/`membership:list`/`project:list` 与其它租户级列表一样用 `Tenant`；带父过滤的列表用父资源（`object` 按 `object_type_id` → `ObjectType`，`object_link` 按源对象 → `Object`，`ingestion` 按 `connector_id` → `Connector`，`pipeline_run`/`pipeline_schedule` 按 `pipeline_id` → `Pipeline`，`action_execution` 按 `action_type_id` → `ActionType`）；没有父过滤的租户级列表（datasets、object_types、connectors、pipelines、action_types、saved_queries、audit_events、operations 等）用 `Tenant`。父资源必须先按租户加载，加载失败 404。

当 handler 无法在不改变 service/repo 语义的前提下加载父资源时（v1 的 ontology `property`/`link_type` update/delete，以及 query 的 `/objects` 过滤读），以 `Tenant` 为授权资源，保留租户隔离与角色矩阵，路由仍做租户过滤；后续 least-privilege change 改为 `ObjectType`。query `/objects` 在 v1 未按 `object_type_id` 预加载父资源，因此不按合成事实判定；结果行仍由 query service 做租户过滤。

绑定与对象链接的跨资源校验：`binding:*` 以 `ObjectType` 为授权资源，但 handler 必须校验 `dataset_id` 同租户；`object_link:*` 以源 `Object` 为授权资源，并校验目标对象同租户。v1 不因目标资源未单独授权而放宽；未来项目收窄时两端都需授权。`object_link:delete` 在未配置 tenant-scoped link reader 时以 `Tenant` 为授权资源（与 Stage 1 等效），`DeleteLink` 仍按租户过滤并在不存在时 404；后续 objects change 增加 `GetLink` 后改为源 `Object`。

### 5.4 策略正文

#### 5.4.1 `policies/platform/tenant-isolation.cedar`

```cedar
// 租户不变量：任何资源动作都不得跨租户。
// 所有资源实体（含 Tenant）都声明 tenant_id。
forbid (
    principal,
    action,
    resource
)
when {
    principal.tenant_id != resource.tenant_id
};
```

#### 5.4.2 `policies/platform/role-matrix.cedar`

```cedar
// read：viewer 及以上
permit (
    principal,
    action in [Action::"read"],
    resource
)
when {
    principal.roles.contains("viewer") ||
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// write：member 及以上
permit (
    principal,
    action in [Action::"write"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// delete：member 及以上
permit (
    principal,
    action in [Action::"delete"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// invoke：member 及以上
permit (
    principal,
    action in [Action::"invoke"],
    resource
)
when {
    principal.roles.contains("member") ||
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// admin：workspace-admin 及以上
permit (
    principal,
    action in [Action::"admin"],
    resource
)
when {
    principal.roles.contains("workspace-admin") ||
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};

// audit：tenant-admin 及以上
permit (
    principal,
    action in [Action::"audit"],
    resource
)
when {
    principal.roles.contains("tenant-admin") ||
    principal.roles.contains("platform-admin")
};
```

#### 5.4.3 `policies/platform/tenancy-scope.cedar`

```cedar
// 组织级管理动作只允许 tenant-admin/platform-admin；
// workspace-admin 只能管理工作空间/项目，不能管理组织。
// 注意不覆盖 tenancy:workspace:create（资源为 Organization），该动作允许 workspace-admin。
forbid (
    principal,
    action in [
        Action::"tenancy:organization:update",
        Action::"tenancy:organization:delete",
        Action::"tenancy:membership:add",
        Action::"tenancy:membership:remove"
    ],
    resource
)
when {
    resource is Organization &&
    !(principal.roles.contains("tenant-admin") || principal.roles.contains("platform-admin"))
};

// 创建组织是租户级管理，不能由 workspace-admin 执行。
forbid (
    principal,
    action in [Action::"tenancy:organization:create"],
    resource
)
when {
    !(principal.roles.contains("tenant-admin") || principal.roles.contains("platform-admin"))
};
```

#### 5.4.4 `policies/platform/resource-state.cedar`

```cedar
// 已归档 Pipeline 只读。
forbid (
    principal,
    action in [Action::"pipeline:update", Action::"pipeline:delete"],
    resource
)
when {
    resource is Pipeline &&
    resource.status == "archived"
};

// 只有已发布 Pipeline 可以触发。
forbid (
    principal,
    action in [Action::"pipeline:invoke"],
    resource
)
when {
    resource is Pipeline &&
    resource.status != "published"
};

// 已归档 Pipeline 不得再发布。
forbid (
    principal,
    action in [Action::"pipeline:publish"],
    resource
)
when {
    resource is Pipeline &&
    resource.status == "archived"
};

// 只有 active ActionType 可以运行；draft/disabled 允许校验/预览但不允许执行。
forbid (
    principal,
    action in [Action::"action_type:run"],
    resource
)
when {
    resource is ActionType &&
    resource.status != "active"
};

// 只有 active Connector 可以同步；test/infer_schema 不受此限。
forbid (
    principal,
    action in [Action::"connector:sync"],
    resource
)
when {
    resource is Connector &&
    resource.status != "active"
};

// 只有 queued/running 的同步任务可以取消。
forbid (
    principal,
    action in [Action::"ingestion:cancel"],
    resource
)
when {
    resource is IngestJob &&
    !(resource.status == "queued" || resource.status == "running")
};

// 只有 queued/running 的动作执行可以取消。
forbid (
    principal,
    action in [Action::"action_execution:cancel"],
    resource
)
when {
    resource is ActionExecution &&
    !(resource.status == "queued" || resource.status == "running")
};

// 只有 failed/cancelled 的动作执行可以重试。
forbid (
    principal,
    action in [Action::"action_execution:retry"],
    resource
)
when {
    resource is ActionExecution &&
    !(resource.status == "failed" || resource.status == "cancelled")
};
```

### 5.5 策略版本与摘要

- 策略 bundle 摘要 = 对 `policies/` 下参与加载的文件按相对路径排序后，依次拼接 `path + "\0" + content + "\0"`，再计算 SHA-256，格式 `sha256:<hex>`。
- 模型摘要 = 对 `policies/openfga/model.fga` 内容计算 SHA-256。
- 审计记录 `bundle_digest`、`model_digest`、`model_id`、`source_commit`。摘要只用于审计与部署比对，不作为运行时放行依据。
- 策略 ID 与摘要必须同时记录；只记录摘要无法解释“哪条策略允许/拒绝”。

### 5.6 求值语义

1. Cedar 对每个请求求值完整策略集；`forbid` 优先于任何 `permit`。
2. 没有匹配的 `permit` 即 deny；空策略集、空动作组、未知动作均 deny。
3. 属性缺失或类型不匹配是求值错误：按 deny 处理，HTTP 403，审计 `reason_code=attribute_missing`；不得用默认值补齐后 allow。
4. Cedar 引擎内部错误、panic 恢复、超时：按 fail-closed 处理，HTTP 503，审计 `reason_code=engine_error`。
5. 角色集合为空、包含未知角色、只有未启用主体类型：deny，`reason_code=role_not_permitted` 或 `subject_unsupported`。
6. 模型/策略加载失败不是运行时 deny，而是启动失败；不得以“空策略集 + 默认拒绝”继续运行。

## 6. 授权决策协议

### 6.1 请求处理顺序

1. `oa-edge` 完成 JWT/租户/路由粗判；未认证 401，未知路由 404，方法不允许 405，限流 429。
2. `authn` 构建 `TenantContext`；缺失/非法租户 401/403；不允许客户端伪造内部头。
3. handler 解析路径/请求体并做输入合法性校验；非法输入 400，此时尚未加载资源、未做授权。
4. handler 在**租户过滤**下加载资源行；不存在返回 404（跨租户同样 404）。
5. handler 组装 `Request{Action, Resource}` 并调用 `Authorizer.Authorize`；authz 从 `TenantContext` 取主体/租户，不信任 handler 传入的租户。
6. authz 依次执行 OpenFGA Check 与 Cedar 求值；两者都 allow 才 allow；任一 deny 即 deny；依赖错误即 error。
7. authz 在返回前写决策审计；审计失败按 error 处理。
8. handler 仅在 allow 时执行副作用；deny/error 直接返回统一错误信封，不产生业务写入。

**关于“先加载还是先授权”**：对象级动作需要资源状态/所有者/父资源事实，必须在加载后判定；加载本身必须带租户过滤，不能泄露跨租户行。创建/列表动作使用 `Tenant`、`Organization` 或 `Workspace` 作为资源，在读取目标数据前判定。

### 6.2 授权请求契约

authz 包对外只暴露一个判定入口（具体签名由实现决定，语义固定）：

```text
Authorize(ctx, Request) → Decision
Request {
  Action   Action        // 目录中的枚举常量
  Resource ResourceFact  // 类型、ID、租户、祖先链、状态、所有者
}
Decision {
  Allowed    bool
  Reason     ReasonCode
  DecisionID string
}
```

`ResourceFact` 的字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `Type` | 是 | 模型类型；未知类型 deny |
| `ID` | 是 | 资源 UUID；Tenant 资源可为租户 ID |
| `TenantID` | 是 | 行 `tenant_id`；必须等于上下文租户 |
| `Ancestors` | 按类型 | 有序祖先链，如 Object → ObjectType、Run → Pipeline、Execution → ActionType、Job → Connector、Project → Workspace → Organization |
| `Status` | 按动作 | Pipeline/Connector/ActionType/ActionExecution/IngestJob 状态；其他资源为空 |
| `OwnerID` | 按类型 | SavedQuery 所有者；其他资源为空 |

- 祖先链必须由 tenant-scoped 查询加载；authz 校验类型顺序与租户一致性，不合法即 error。
- `Status`/`OwnerID` 只来自数据库行，不得来自请求体；客户端提交的同名字段被忽略。
- 业务模块不得把角色、claims、原始 JWT 放进 `Request`；authz 直接读 `TenantContext`。
- `Decision` 必须包含 decision id；即使 deny/error 也返回，供响应头与审计关联。

### 6.3 原因码

| 原因码 | 含义 | 典型 HTTP |
| --- | --- | --- |
| `allowed` | 关系与属性均允许 | 2xx |
| `tenant_mismatch` | 主体/资源租户不一致 | 403（加载阶段则为 404） |
| `no_relationship` | OpenFGA 无关系或关系链不成立 | 403 |
| `role_not_permitted` | 角色矩阵无匹配 permit | 403 |
| `admin_required` | 需要更高管理角色（含 tenancy scope） | 403 |
| `resource_state_denied` | 资源状态 forbid | 403 |
| `attribute_missing` | Cedar 必需属性缺失/类型不符 | 403 |
| `unknown_action` | 动作未登记或不在 schema | 403 |
| `subject_unsupported` | 组/服务账号等未启用主体 | 403 |
| `no_policy` | 无匹配策略（空策略集/编译失败在启动阶段终止） | 403 |
| `engine_timeout` | OpenFGA/Cedar 超时 | 503 |
| `engine_unavailable` | OpenFGA 不可达/5xx | 503 |
| `engine_error` | Cedar/模型内部错误 | 503 |
| `audit_unavailable` | 决策审计写入失败 | 503 |
| `internal_error` | 元组/请求构造不变量失败 | 503 |

原因码是对外错误信封之外的审计字段；HTTP 响应只返回统一错误码与 decision id，不返回原因码、策略 ID、关系名或资源是否存在之外的信息。

### 6.4 HTTP 映射

| 场景 | HTTP | 错误码 | 响应头 | 审计 |
| --- | --- | --- | --- | --- |
| 无/非法 JWT | 401 | `unauthenticated` | — | 无 authz 事件（authn 负责） |
| 租户缺失/非法 | 403 | `permission_denied` | — | 无 authz 事件 |
| 租户头与 JWT 不一致且无 platform-admin 资格 | 403 | `permission_denied` | — | 无 authz 事件 |
| 未知路由 | 404 | `not_found` | — | 无 |
| 方法不允许 | 405 | 由路由/`edge-gateway` 契约定义，不新增核心错误码 | — | 无 |
| 输入非法 | 400 | `bad_request` / `invalid_argument` | — | 无 |
| 跨租户/租户内不存在 | 404 | `not_found` | — | 可选 `authz.decision` deny（若已进入 authz） |
| 关系/角色/状态/属性 deny | 403 | `forbidden` | `X-Ontoaxis-Decision-Id` | `authz.decision` deny |
| 未知动作/未登记 | 403 | `forbidden` | `X-Ontoaxis-Decision-Id` | `authz.decision` error/deny |
| OpenFGA 超时/不可达 | 503 | `unavailable` | `X-Ontoaxis-Decision-Id` | `authz.decision` error |
| Cedar 求值错误 | 503 | `unavailable` | `X-Ontoaxis-Decision-Id` | `authz.decision` error |
| 审计写入失败 | 503 | `unavailable` | `X-Ontoaxis-Decision-Id`（尽力） | 无法保证；进程侧错误日志 |
| 策略/模型加载失败 | 启动失败 | — | — | `authz.policy_change` rejected（若 sink 可用） |

- 403 响应体固定为统一错误信封：`{"error":{"code":"forbidden","message":"permission denied"}}`；不得包含策略名、关系名、角色名或“缺少哪个权限”的提示。
- 503 响应体同样使用统一信封，`code=unavailable`，`message` 为通用文案；不得泄露上游地址或内部错误。
- `X-Ontoaxis-Decision-Id` 只在 authz 已产生 decision 时设置；401/400/404（未进入 authz）不设置。
- 决策事件不记录最终业务 HTTP 状态；判定结果由 `decision` 表达。若实现需要记录最终状态，必须另发 outcome 事件，不得改写 decision 事件。

### 6.5 列表与批量

- 列表：先判定父作用域（Tenant/Organization/Workspace，或 ObjectType/Pipeline/ActionType/Connector/Object 等）级 read，再执行租户过滤查询；v1 不对每行做 OpenFGA Check。若未来某属性需要逐行过滤（如 markings），必须逐行 Cedar 求值，且不得因过滤而放宽租户条件。
- 批量写（`BatchUpsertObjects`）：对目标 `ObjectType` 做一次 write 判定；批次内所有对象必须属于同一 object_type 与租户；混合类型/租户整批拒绝，不允许部分放行。
- 批量结果不得因“部分成功”而返回未授权对象的数据；deny 时整批无副作用。
- 对象链接创建/删除必须校验源对象与目标对象属于同一租户；v1 以源对象授权 + 目标对象租户范围校验，未来收窄到项目时两端都需授权。
- `ListSavedQueries` 保持租户内可见（与 Stage 1 一致）；不按 owner 过滤。安全评审已确认保持租户内可见；若未来要求仅 owner 可见，必须修订本节并同步 query-surface delta spec。
- `SavedQuery` 删除同时要求 `can_write` owner 关系与 `delete` 组（member+）；viewer 即使是 owner 也不允许删除（角色矩阵优先，owner 不是绕过角色限制的例外）。

### 6.6 超时、重试与缓存

| 项 | 值 | 说明 |
| --- | --- | --- |
| OpenFGA Check 超时 | 200ms | 单次；超时即 503，不重试 |
| 授权总预算 | 300ms | 含元组构造、Check、Cedar、审计 |
| Cedar 求值 | 进程内，目标 <5ms | 无网络 |
| 重试 | 无 | 重试会放大故障与延迟；失败即 fail-closed |
| 跨请求缓存 | 无 | 无 Valkey；allow 缓存会延长撤权窗口 |
| 请求内记忆化 | 允许 | 键 `subject|relation|object`；同一请求内复用，不跨请求 |

不使用 Valkey 的原因：当前 Compose 未部署 Valkey；更重要的是 allow 缓存在撤权/策略变更时会产生陈旧放行。若未来引入缓存，必须是 deny-aware、带事件失效且有安全评审，不得默认开启。

### 6.7 Explain

- `AuthorizationService.Explain` 只返回**调用者自己**在目标资源上的判定解释：`allowed`、命中的 OpenFGA 关系链（关系名，不含元组主体列表）、命中的 Cedar 策略 ID、最终 decision id。
- Explain 需要 `authorization:explain`（admin 组）；不得用它探测其他用户或其他租户。
- Explain 结果不得包含策略正文、其他主体 ID、JWT、资源内容或数据库字段。
- Explain 调用本身也写 `authz.explain` 审计；若解释失败，返回 503，不返回部分结果。
- v1 不实现 `ListPermissions` 的公开 HTTP 语义；如实现，返回调用者角色矩阵内的动作组集合，不返回资源实例列表。

## 7. 默认拒绝矩阵

| 失败/边界条件 | 判定 | HTTP | 审计 reason |
| --- | --- | --- | --- |
| 缺少 JWT / 签名/iss/aud/exp 非法 | authn 拒绝 | 401 | 无 authz 事件 |
| JWT 无 `tenant_id` 或非法 | authn 拒绝 | 403 | 无 authz 事件 |
| `X-Tenant-Id` 与 JWT 不一致且非合法 platform-admin | authn 拒绝 | 403 | 无 authz 事件 |
| 资源行租户 ≠ 上下文租户 | 加载阶段不返回行 | 404 | 可选 deny |
| 主体不在资源租户关系链 | deny | 403 | `no_relationship` |
| 角色集合为空/未知 | deny | 403 | `role_not_permitted` |
| 组/服务账号主体 | deny | 403 | `subject_unsupported` |
| 动作未登记/不在 schema | deny | 403 | `unknown_action` |
| 运行时无匹配 permit | deny | 403 | `no_policy` |
| Cedar 必需属性缺失/类型不符 | deny | 403 | `attribute_missing` |
| Pipeline 非 published 触发 | deny | 403 | `resource_state_denied` |
| Pipeline archived 更新/删除/发布 | deny | 403 | `resource_state_denied` |
| ActionType 非 active 运行 | deny | 403 | `resource_state_denied` |
| Connector 非 active 同步 | deny | 403 | `resource_state_denied` |
| IngestJob 非 queued/running 取消 | deny | 403 | `resource_state_denied` |
| ActionExecution 非 queued/running 取消、非 failed/cancelled 重试 | deny | 403 | `resource_state_denied` |
| workspace-admin 执行组织级管理 | deny | 403 | `admin_required` |
| member 执行 admin 组动作 | deny | 403 | `role_not_permitted` |
| viewer 执行 write/delete/invoke/admin/audit | deny | 403 | `role_not_permitted` |
| 非 owner 删除 SavedQuery | deny | 403 | `no_relationship` |
| OpenFGA 超时/不可达/5xx | error | 503 | `engine_timeout` / `engine_unavailable` |
| Cedar 内部错误/panic/超时 | error | 503 | `engine_error` |
| 元组/祖先链构造不变量失败 | error | 503 | `internal_error` |
| 审计写入失败（决策或策略加载） | error / 启动失败 | 503 | `audit_unavailable` |
| OpenFGA store 非空 | 启动失败 | — | `policy_change` rejected |
| 模型缺失/model_id 不匹配/DSL 校验失败 | 启动失败 | — | `policy_change` rejected |
| 策略编译失败/schema 不匹配/动作组无 permit | 启动失败 | — | `policy_change` rejected |
| 配置出现 `AUTH_DISABLED`/`AUTHZ_FAIL_OPEN`/默认 Principal | 启动失败 | — | 进程错误日志 |
| 未知路由/方法 | 边缘/路由拒绝 | 404/405 | 无 |
| `dataset_permissions` 被读取/写入 | 视为实现缺陷 | — | 代码评审/测试阻断 |
| markings 相关请求（v1 无 API） | 不存在 | 404/400 | 不适用 |

## 8. 审计事件

### 8.1 事件类型

| 事件 | 触发 | 必需 | 说明 |
| --- | --- | --- | --- |
| `authz.decision` | 每次进入 authz 的业务请求 | 是 | allow/deny/error 均记录；列表/批量一次事件 |
| `authz.policy_change` | 启动加载策略/模型时 | 是 | 记录摘要、model id、source commit、结果；失败则启动失败 |
| `authz.explain` | 每次 Explain 调用 | 是 | 记录被解释的主体/资源与命中策略，不记录策略正文 |
| `authz.relationship_change` | 未来持久元组变更 | 否 | v1 不产生；保留名称，禁止提前使用 |

v1 不产生 `authz.grant_change`：成员/项目资源变更本身是业务动作，由对应的 `authz.decision` 记录；它们不改变本请求内的授权事实。未来持久化投影引入元组写入时，再启用 `authz.relationship_change`。

### 8.2 公共信封

所有事件为一行 JSON，顶层包含 `level`、`time`、`msg`（等于 `event_type`），业务字段放在 `authz` 对象中。`authz` 字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `schema_version` | int | 是 | v1 = 1；字段变更必须提升 |
| `event_id` | string(UUIDv7) | 是 | 事件唯一 ID |
| `event_type` | string | 是 | 见 §8.1 |
| `occurred_at` | string(RFC3339Nano, UTC) | 是 | 事件时间 |
| `tenant_id` | string(UUID) | 条件 | 决策/解释必填；策略加载为空 |
| `request_id` | string | 条件 | 决策/解释必填；策略加载为空 |
| `trace_id` | string(hex) | 否 | 有 trace 时填写 |
| `span_id` | string(hex) | 否 | 有 span 时填写 |
| `actor` | object | 是 | `{type,id,roles}`；系统事件 `type=system` |
| `action` | string | 条件 | 决策/解释必填 |
| `resource` | object | 条件 | `{type,id,tenant_id,count?}`；决策/解释必填 |
| `decision` | string | 条件 | `allow|deny|error`；决策/解释必填 |
| `reason_code` | string | 条件 | 见 §6.3；决策/解释必填 |
| `decision_id` | string(UUIDv7) | 条件 | 决策/解释必填 |
| `policy` | object | 条件 | 决策/解释必填；见 §8.3 |
| `http` | object | 否 | `{method,route}`；不记录原始 URL/query/User-Agent |
| `latency_ms` | object | 否 | `{fga,cedar,total}`；整数毫秒 |
| `error` | object | 否 | `{engine,code,message}`；message 必须脱敏 |
| `policy_change` | object | 条件 | 仅 `authz.policy_change` |
| `explain` | object | 条件 | 仅 `authz.explain` |

### 8.3 `authz.decision`

字段补充：

- `policy.fga_model_id`：配置的 model id。
- `policy.fga_relation`：本次检查的最终关系（如 `can_read`），不记录完整元组。
- `policy.fga_decision`：`allow|deny|error`。
- `policy.cedar_policy_ids`：命中的 permit/forbid 策略 ID 数组；deny 且无命中时为空数组。
- `policy.cedar_decision`：`allow|deny|error`。
- `policy.bundle_digest`：策略 bundle 摘要。
- `resource.count`：仅批量请求记录请求项数；列表不记录（决策发生在查询前），单资源省略。
- `http.method`、`http.route`：route 必须是 chi pattern（如 `/api/v1/objects/{id}`），不得记录原始 URL/query。
- `latency_ms.fga`、`latency_ms.cedar`、`latency_ms.total`：整数毫秒；未执行到该阶段时省略。

决策事件在业务副作用之前写入。allow 事件写入成功后才执行 handler；deny/error 事件写入成功后才返回 403/503。若写入失败，不得继续业务，也不得返回 allow。

### 8.4 `authz.policy_change`

字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `result` | string | `loaded` / `rejected` |
| `model_id` | string | 配置/加载的 model id |
| `model_digest` | string | `sha256:<hex>` |
| `bundle_digest` | string | `sha256:<hex>` |
| `source_commit` | string | 构建 commit；未知时 `unknown` |
| `changed` | bool | 仅热加载场景有意义；v1 启动加载为 `false` |
| `previous_digest` | string | v1 为空；热加载时填上次摘要 |
| `error` | object | `rejected` 时必填，`{code,message}` 已脱敏 |

策略加载事件在进程开始接受业务流量之前写入；失败时进程退出（启动失败），不得以空策略集继续。启动加载成功的事件表示“本次进程使用的策略/模型摘要”，不表示部署流水线已完成审批；审批证据在 change/Stage 记录中。

### 8.5 `authz.explain`

字段补充：

- `explain.target_actor_id`：被解释的主体 UUID（只能是调用者本人）。
- `explain.target_resource`：`{type,id}`。
- `explain.matched_relations`：命中的 OpenFGA 关系名数组。
- `explain.matched_policy_ids`：命中的 Cedar 策略 ID 数组。
- `explain.allowed`：bool。

Explain 事件仍包含公共信封的 `actor`（调用者）、`action`、`resource`、`decision_id`；不包含策略正文、其他主体 ID、JWT 或资源内容。

### 8.6 禁止字段与脱敏

**禁止写入**：

- `Authorization`、Cookie、JWT、refresh token、API key、数据库连接串。
- 请求体/响应体、SQL 文本、查询计划、Connector config、密钥、邮箱、姓名、电话。
- 原始 IP、原始 URL（含 query）、User-Agent 原文。
- 其他用户/主体的 ID 列表（Explain 只允许调用者本人）。
- 策略正文、Cedar 实体完整属性集、OpenFGA 完整元组列表。

**允许写入**：租户/用户/资源 UUID、角色名、动作名、关系名、策略 ID、摘要、route pattern、HTTP 方法、原因码、耗时、错误码。

`error.message` 只允许固定的脱敏文案或错误码，不得拼接上游响应体。IP 在 v1 不记录；如未来需要，必须使用部署方持有的盐做 SHA-256，并在安全评审中说明用途与保留期。

### 8.7 写入、失败与保留

- sink：`AuditSink.Emit(ctx, Event) error`；默认实现为无缓冲 JSON 行写入 stdout，逐条检查写入错误。不得用会吞错的异步 logger 代替 fail-closed sink。
- 决策事件同步写入；超时 50ms；失败返回 `audit_unavailable`，HTTP 503，业务不执行。
- 策略加载事件在启动时同步写入；失败则退出。
- 不采样、不批量丢弃、不做“仅错误采样”。审计是安全证据，不是普通 telemetry。
- 日志轮转、保留期、集中采集由部署方负责；应用不得自行删除或改写历史事件。保留期必须覆盖 Stage 2 验收要求；具体期限按 §13.2 第 4 项与部署合规确定（建议不少于 90 天）。
- `audit.proto`/`AuditService` 保持可选；如未来实现，映射为：`AuditEvent.id←event_id`、`event_type←event_type`、`actor_id←actor.id`、`resource_type/id←resource`、`payload←authz` JSON、`created_at←occurred_at`。v1 不注册该 RPC。

### 8.8 示例

允许（单资源）：

```json
{
  "level": "INFO",
  "time": "2026-11-22T13:00:00.123456789Z",
  "msg": "authz.decision",
  "authz": {
    "schema_version": 1,
    "event_id": "01935f3e-8a5b-7c3d-9e1f-2a4b6c8d0e12",
    "event_type": "authz.decision",
    "occurred_at": "2026-11-22T13:00:00.123456789Z",
    "tenant_id": "01935f3e-0000-7000-8000-000000000001",
    "request_id": "req-7f3c2a",
    "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
    "actor": {
      "type": "user",
      "id": "01935f3e-1111-7000-8000-000000000002",
      "roles": ["member"]
    },
    "action": "dataset:read",
    "resource": {
      "type": "Dataset",
      "id": "01935f3e-2222-7000-8000-000000000003",
      "tenant_id": "01935f3e-0000-7000-8000-000000000001"
    },
    "decision": "allow",
    "reason_code": "allowed",
    "decision_id": "01935f3e-3333-7000-8000-000000000004",
    "policy": {
      "fga_model_id": "01J8Z0000000000000000000A",
      "fga_relation": "can_read",
      "fga_decision": "allow",
      "cedar_policy_ids": ["policies/platform/role-matrix.cedar#0"],
      "cedar_decision": "allow",
      "bundle_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    "http": {
      "method": "GET",
      "route": "/api/v1/datasets/{id}"
    },
    "latency_ms": {
      "fga": 8,
      "cedar": 1,
      "total": 11
    }
  }
}
```

拒绝（viewer 执行写动作，已进入 authz）：

```json
{
  "level": "INFO",
  "time": "2026-11-22T13:01:00.000000000Z",
  "msg": "authz.decision",
  "authz": {
    "schema_version": 1,
    "event_id": "01935f3e-8a5b-7c3d-9e1f-2a4b6c8d0e13",
    "event_type": "authz.decision",
    "occurred_at": "2026-11-22T13:01:00.000000000Z",
    "tenant_id": "01935f3e-0000-7000-8000-000000000001",
    "request_id": "req-7f3c2b",
    "actor": {
      "type": "user",
      "id": "01935f3e-1111-7000-8000-000000000002",
      "roles": ["viewer"]
    },
    "action": "dataset:update",
    "resource": {
      "type": "Dataset",
      "id": "01935f3e-2222-7000-8000-000000000003",
      "tenant_id": "01935f3e-0000-7000-8000-000000000001"
    },
    "decision": "deny",
    "reason_code": "role_not_permitted",
    "decision_id": "01935f3e-3333-7000-8000-000000000005",
    "policy": {
      "fga_model_id": "01J8Z0000000000000000000A",
      "fga_relation": "can_write",
      "fga_decision": "allow",
      "cedar_policy_ids": [],
      "cedar_decision": "deny",
      "bundle_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    "http": {
      "method": "PATCH",
      "route": "/api/v1/datasets/{id}"
    },
    "latency_ms": {
      "fga": 7,
      "cedar": 1,
      "total": 9
    }
  }
}
```

策略加载：

```json
{
  "level": "INFO",
  "time": "2026-11-22T13:02:00.000000000Z",
  "msg": "authz.policy_change",
  "authz": {
    "schema_version": 1,
    "event_id": "01935f3e-8a5b-7c3d-9e1f-2a4b6c8d0e14",
    "event_type": "authz.policy_change",
    "occurred_at": "2026-11-22T13:02:00.000000000Z",
    "tenant_id": "",
    "actor": {
      "type": "system",
      "id": "ontoaxis-core",
      "roles": []
    },
    "policy_change": {
      "result": "loaded",
      "model_id": "01J8Z0000000000000000000A",
      "model_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "bundle_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "source_commit": "unknown",
      "changed": false,
      "previous_digest": ""
    }
  }
}
```

## 9. 配置与启动

### 9.1 配置项

配置遵循 `ONTOAXIS_*` 约定；authz 使用 `ONTOAXIS_AUTHZ_*` 前缀。新增配置必须在 `internal/platform/config` 中校验，且不得记录敏感值。

| 配置 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `ONTOAXIS_AUTHZ_OPENFGA_API_URL` | 是 | 无 | OpenFGA HTTP API 地址；不得含凭据 |
| `ONTOAXIS_AUTHZ_OPENFGA_STORE_ID` | 是 | 无 | 专用 store id |
| `ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID` | 是 | 无 | 已上传模型 id |
| `ONTOAXIS_AUTHZ_OPENFGA_TIMEOUT` | 否 | `200ms` | 单次 Check 超时；必须 >0 |
| `ONTOAXIS_AUTHZ_DECISION_TIMEOUT` | 否 | `300ms` | 授权总预算；必须 ≥ OpenFGA 超时 |
| `ONTOAXIS_AUTHZ_POLICY_DIR` | 否 | 空=嵌入 | 本地覆盖策略目录，仅用于测试/开发；生产不得指向可写目录 |
| `ONTOAXIS_AUTHZ_AUDIT_SINK` | 否 | `stdout` | v1 仅支持 `stdout`；其他值启动失败 |
| `ONTOAXIS_AUTHZ_FAIL_OPEN` | 否 | 禁止 | 出现即启动失败；不得实现为“失败时放行” |
| `ONTOAXIS_AUTH_DISABLED` | 否 | 禁止 | 出现即启动失败 |

- `ONTOAXIS_AUTHZ_POLICY_DIR` 在非测试环境设置时，必须通过启动校验并记录来源；不得在运行时热加载。
- OpenFGA 凭据（如未来需要）只能通过环境/密钥注入，不得写入 YAML 或审计；v1 自托管默认无 token。
- `trust_gateway` 只影响 authn 信任内部头，不改变 authz；开启时必须由 `oa-edge` 清除客户端伪造的内部头。

### 9.2 启动顺序

1. 加载配置；拒绝 `AUTH_DISABLED`/`AUTHZ_FAIL_OPEN`/空策略目录等禁止配置。
2. 读取并编译 Cedar schema 与策略，计算 bundle 摘要；失败即退出。
3. 读取并解析 `model.fga`，计算模型摘要；连接 OpenFGA，校验 store、model id、模型关系与零元组；失败即退出（连接暂时不可用可有限重试，超时后退出）。
4. 加载动作目录；校验每个动作有组、每个组有 permit、每个动作用到的资源类型在 schema 中；失败即退出。
5. 写 `authz.policy_change` 审计；失败即退出。
6. 标记 authz ready；`/readyz` 仅在 authz、DB、其他既有依赖均就绪时返回 200。
7. 开始接受业务请求。

### 9.3 就绪与运行期故障

- 启动校验失败属于配置/供应链错误：进程退出，不以“未就绪”继续提供 503 服务，避免部署误判为可用。
- 运行期 OpenFGA 不可达/超时：请求 503，`/readyz` 必须反映依赖探测失败（缓存探测结果，避免每请求探测）；不得降级为 allow，也不得使用陈旧 allow 缓存。
- 运行期 Cedar 是进程内编译产物；无运行期加载失败。若出现 panic，recover 后 503，并记录 `engine_error`。
- 审计 sink 写失败：请求 503；`/readyz` 是否置 0 由实现决定，但不得继续放行。建议将连续审计失败计入 readiness。

### 9.4 禁止的配置形态

- `AUTH_DISABLED=true`、`AUTHZ_ENABLED=false`、`AUTHZ_MODE=shadow`、`AUTHZ_DEFAULT_PRINCIPAL=*`、`AUTHZ_FAIL_OPEN=true`。
- 空 `ONTOAXIS_AUTHZ_POLICY_DIR` 指向空目录后“跳过策略加载”。
- 用 `ONTOAXIS_AUTHZ_OPENFGA_API_URL` 指向 mock/fake 服务作为生产配置。
- 把角色列表、租户列表、策略 JSON 直接放进环境变量作为运行时真源。

## 10. 可观测性

### 10.1 指标

| 指标 | 类型 | Labels | 说明 |
| --- | --- | --- | --- |
| `ontoaxis_authz_decisions_total` | counter | `action`,`decision`,`reason` | 决策计数；action ≤ 100，reason ≤ 20 |
| `ontoaxis_authz_duration_seconds` | histogram | `engine`（`fga`/`cedar`/`total`） | 授权耗时；bucket 复用观测基线 |
| `ontoaxis_authz_errors_total` | counter | `engine`,`code` | 引擎/审计错误 |
| `ontoaxis_authz_policy_loaded` | gauge | `result`（`ok`/`error`） | 启动加载结果；1/0 |
| `ontoaxis_authz_ready` | gauge | — | authz readiness；1/0 |

禁止 label：`tenant_id`、`user_id`、`resource_id`、`decision_id`、`request_id`、`trace_id`、原始 URL、策略正文。

### 10.2 Trace

- span 名：`authz.check`；子 span：`authz.fga.check`、`authz.cedar.eval`、`authz.audit.emit`。
- attributes 仅允许：`authz.action`、`authz.decision`、`authz.reason`、`authz.fga.relation`、`authz.cedar.policy_ids`（低基数）、`authz.latency_ms`。
- 禁止：tenant/user/resource ID、策略正文、元组列表、JWT、SQL、请求体。
- span status：deny 记为 `OK`（业务拒绝不是系统错误）；error 记为 `ERROR` 并附原因码。

### 10.3 日志

- 授权日志只通过 `AuditSink` 输出 `authz.*` 事件；不得再写一条包含用户/资源的普通日志。
- 启动失败、配置错误、sink 错误可写普通 error 日志，但必须脱敏，且不得包含 JWT/策略正文。
- `request_id`/`trace_id` 只用于关联，不替代审计事件。

## 11. 测试与验收

### 11.1 单元测试

| 对象 | 覆盖 |
| --- | --- |
| OpenFGA DSL | 解析成功；必需类型/关系存在；未知关系/类型校验失败；模型摘要稳定 |
| 元组构造 | 每种资源类型的上下文元组；租户/祖先/owner 缺失；超过 8 条；跨租户行 |
| 动作目录 | 每个 RPC 有动作；每个动作有组；每个组有 permit；未登记动作 deny |
| Cedar schema/策略 | 编译；类型检查；每条 policy 至少一个 allow/deny 用例；forbid 优先 |
| 角色矩阵 | 5 角色 × 6 组矩阵；空/未知角色；多角色并集 |
| 状态策略 | Pipeline published/draft/archived；ActionType active/draft/disabled；Connector active/paused/error；IngestJob/Execution 状态 |
| 默认拒绝 | §7 每一行至少一个测试 |
| 审计 | 字段完整性、JSON 可解析、脱敏黑名单、写入失败返回 503、策略加载失败退出 |
| HTTP | 401/403/404/503 信封、decision id 头、无策略细节泄露 |

### 11.2 真实实例集成测试

- 使用 **真实 OpenFGA**（testcontainers 启动固定版本镜像，创建专用 store，上传 §4.2 模型）和 **真实 `cedar-go`**；不得用 fake 关系引擎冒充“真实实例”。
- 测试用 HTTP server 挂载至少一个真实业务 handler（如 datasets GET）和一个 authz guard；用真实 `TenantContext` 与真实 `Authorizer`。
- 必须覆盖：
  1. 租户成员读/写 allow；viewer 读 allow、写 deny。
  2. 跨租户资源 404/403，且不返回资源内容。
  3. 非 owner 删除 SavedQuery deny；owner allow。
  4. Object → ObjectType → Tenant 关系链 allow/deny。
  5. Pipeline archived/非 published 触发 deny；published allow。
  6. ActionType disabled 运行 deny；active allow。
  7. Connector paused 同步 deny；active allow。
  8. IngestJob/Execution 非法状态取消/重试 deny。
  9. workspace-admin 组织级管理 deny；tenant-admin allow。
  10. 未知动作 deny；空策略集启动失败。
  11. OpenFGA 不可达/超时 503；Cedar 错误 503；审计 sink 失败 503。
  12. 响应头 decision id 与审计 decision_id 一致。
- 运行方式：`go test -tags=integration ./internal/platform/authz/...`；CI 使用 Docker job。`make test` 不强制 Docker；集成测试在无 Docker 时必须显式失败或跳过并标注，CI 不得跳过。
- 证据：测试日志、OpenFGA 容器版本、模型 ID、bundle digest、关键 allow/deny 输出；放入 `docs/stages/evidence/stage-2-authorization/`（目录由 `stage-2-integration` 管理，本 change 提供原始输出）。

### 11.3 负向矩阵（最低要求）

| 用例 | 期望 |
| --- | --- |
| 无 JWT | 401 |
| JWT 无 tenant_id | 403 |
| 租户头与 JWT 不一致 | 403 |
| 跨租户 GET/PATCH/DELETE | 404（查询阶段）或 403（已进入 authz），不得 200 |
| viewer POST/PATCH/DELETE | 403 |
| member 组织管理/成员管理 | 403 |
| workspace-admin 组织创建/更新/删除 | 403 |
| tenant-admin 审计读取 | 200（allow） |
| workspace-admin 审计读取 | 403 |
| 非 owner 删除 SavedQuery | 403 |
| viewer 作为 SavedQuery owner 删除 | 403（角色矩阵优先） |
| owner 删除 SavedQuery | 200/204 |
| archived Pipeline 触发/更新/删除/发布 | 403 |
| draft Pipeline 触发 | 403 |
| disabled ActionType 运行 | 403 |
| paused Connector 同步 | 403 |
| queued Execution 重试 | 403 |
| succeeded Execution 取消 | 403 |
| 组主体/未知角色 | 403 |
| 未登记动作 | 403 |
| OpenFGA 超时 | 503 |
| 审计失败 | 503 且无业务写入 |
| 空策略集/策略编译失败 | 启动失败 |
| OpenFGA store 有元组 | 启动失败 |

### 11.4 验收映射（S2-AC-03）

| S2-AC-03 子项 | 本文条款 | 计划证据 |
| --- | --- | --- |
| OpenFGA 与 Cedar 生效 | §2、§4、§5、§9 | 启动加载事件、集成测试 allow |
| 关系 allow/deny | §4.3、§4.4、§11.2 | 租户成员 allow；非成员/跨租户 deny；SavedQuery owner |
| 属性 allow/deny | §5.4、§5.6 | viewer 读 allow/写 deny；状态策略 |
| 跨租户拒绝 | §3.2、§6.4、§11.3 | 404/403 用例，无内容泄露 |
| 越权拒绝 | §5.4.3、§6.3 | workspace-admin 组织管理 deny；member admin deny |
| 无策略默认拒绝 | §5.6、§7、§9.2 | 空 bundle 启动失败；未知动作 deny |
| 策略变更审计可复核 | §8.4、§8.7 | `authz.policy_change` 摘要/model id/commit；失败退出 |

Stage 2 归档时还需 `S2-AC-01`（认证/租户）与 `S2-AC-11`（可观测性）的既有证据；本文不重复定义。

## 12. 迁移与兼容

### 12.1 与 Stage 1 的行为差异

| 行为 | Stage 1 | Stage 2 v1 | 说明 |
| --- | --- | --- | --- |
| 租户内读 | 任意已认证租户成员 | read 组：viewer 及以上 | 新增角色约束 |
| 租户内写 | 任意已认证租户成员 | write 组：member 及以上 | viewer 变只读 |
| 租户内删除 | 任意已认证租户成员 | delete 组：member 及以上 | viewer 不能删 |
| 组织/工作空间/项目管理 | 任意已认证租户成员 | admin 组：workspace-admin+（组织级 tenant-admin+） | 收紧 |
| SavedQuery 删除 | 租户内任意成员 | owner 且 `delete` 组（member+） | 收紧，使用关系+角色判定 |
| Pipeline 触发 | 任意状态 | 仅 published | 属性判定 |
| ActionType 运行 | 任意状态 | 仅 active | 属性判定 |
| Connector 同步 | 任意状态 | 仅 active | 属性判定 |
| 执行取消/重试 | 任意状态 | 状态机允许的状态 | 属性判定 |
| 跨租户 | 已拒绝 | 保持拒绝，404 语义更明确 | 不变 |

跨租户、角色矩阵与默认拒绝属于本变更的 `authorization` delta spec 的统一行为，不需要每个模块重复声明；但 SavedQuery owner、Pipeline/ActionType/Connector/IngestJob/ActionExecution 状态、租户管理范围等资源特定收紧，必须进入对应模块的 delta spec（query-surface、pipelines、actions、connectors、ingestion、tenancy）。不得只在代码里体现，也不得由实现自行放宽。

### 12.2 项目级收窄迁移路径

1. 先让资源 handler 能加载 `core.project_resources` 绑定，并在请求中带上 project id。
2. 回填 `project_resources` 与成员关系，验证无孤儿资源。
3. 追加 `project:<id>#...` 绑定元组，逐步移除 `organization.member` 的 `or member from tenant`。
4. 用双读/双写过渡期对比判定结果，确认无意外拒绝后再切换。
5. 迁移期间不得默认放行；失败按 deny 处理。

v1 不执行上述步骤，也不得偷偷写 project 元组。

### 12.3 `dataset_permissions`

- 该表当前无 API、无运行时读取；v1 MUST NOT 读取或写入。
- 它不能成为第二个授权源；若未来要启用数据集级 ACL，必须先决定它与 OpenFGA 的单一真源关系，再迁移数据。
- 代码评审必须检查没有新的 SQL 读取该表；测试可断言该表在 authz 路径中未被引用。

### 12.4 markings

- `object_data.objects.markings` 列存在，但 domain/proto/repo 未读取；v1 不得声称已强制 markings。
- 后续 marking change 必须同时补齐：proto/domain/repo 读取、Cedar 实体属性、策略、解释/审计、越权测试。
- 在补齐前，任何“restricted 对象只允许 tenant-admin”的声明都是未实现承诺，不得写入规格或验收证据。

### 12.5 数据库与迁移

- 本边界不新增表、不修改表、不新增迁移。
- OpenFGA 使用独立 store；不复用业务 PostgreSQL schema。
- 若未来引入审计表或持久元组，迁移由对应 change 负责，且必须 append-only。

### 12.6 发布与回滚

1. 部署流水线上传新模型（如变更），获得 model id；更新配置。
2. 部署新二进制（内嵌新策略）；启动校验 + 策略加载审计。
3. `/readyz` 通过后逐步放量；观察 `ontoaxis_authz_*` 指标与 deny 原因。
4. 回滚：回滚到上一镜像 + 上一 model id；旧模型保留在 OpenFGA。禁止通过关闭授权回滚。
5. 若策略误拒导致业务不可用，回滚版本，而不是加 allow 例外；紧急例外必须走安全评审并记录。

### 12.7 所有权与接线

- `internal/platform/authz/`、`policies/`、本文由 `authorization-openfga-cedar` 所有。
- 各模块 `internal/modules/*/api/` 的授权接缝由本 change 修改；不得改模块 service/repo 的租户过滤语义。
- `internal/app/`、`cmd/ontoaxis-core/` 由 `stage-2-integration` 所有：本 change 提供构造函数与接线 patch，由 integration 应用；不得直接提交共享文件。
- `deploy/compose/`、`deploy/helm/`、`deploy/argocd/` 由 `production-delivery` 所有：OpenFGA 服务供给、健康检查、资源限制、日志采集由该 change 落地。本 change 的集成测试用 testcontainers，不修改 Compose。
- `go.mod`/`go.sum` 由本 change 提交补丁（OpenFGA SDK、cedar-go、testcontainers），integration 统一 tidy。
- `docs/stages/evidence/` 由 integration 管理；本 change 提供原始测试输出与摘要。
- `web/` 不修改。

## 13. 评审与未决事项

### 13.1 评审清单

- [ ] 单一判定点：没有模块自写角色/租户判断，没有第二个策略源。
- [ ] 模型：租户根、层级、资源边、owner、零持久元组与本文一致。
- [ ] 策略：动作分组、角色矩阵、租户不变量、管理范围、状态约束完整；forbid 优先。
- [ ] 默认拒绝：§7 每行有实现与测试；无可配置放行。
- [ ] 审计：字段、脱敏、fail-closed、策略加载事件、decision id 完整；无 token/PII。
- [ ] 行为差异：viewer 只读、owner 删除、状态收紧已进入对应 delta spec。
- [ ] 非目标：未实现项目收窄、markings、dataset_permissions、持久元组。
- [ ] 所有权：未修改 `deploy/`、`internal/app/`、`web/`；`go.mod` 仅补丁。
- [ ] 测试：真实 OpenFGA + 真实 Cedar；负向矩阵；CI 证据。
- [x] 安全评审：撤销时延、审计保留期、OpenFGA 供给与模型上传流程已确认。

### 13.2 已确认决策（负责人、安全评审，2026-11-22）

以下决策由项目负责人于 2026-11-22 确认，安全评审于同日通过。实现时不得自行放宽；如实现中发现不可行，按 §14 修订并重新评审。

1. **SavedQuery 读取范围**：v1 保持租户内可见（含 SQL 文本）；删除要求 owner 关系且属于 `delete` 组（member+），tenant-admin 不覆盖删除。owner 离职后的清理属于已知运维缺口，由平台/DB 处理；后续通过独立管理关系或管理工具解决。
2. **ActionExecution 取消/重试范围**：保持 member-wide + 状态机约束；不新增 owner/initiator 字段。若未来要求“只能操作自己的执行”，另开 change 增加 `created_by`，不在本 change 临时加列。
3. **workspace-admin 的用户列表权限**：`identity:list` 保持 `audit` 组（tenant-admin/platform-admin）。`ListUsersRequest` 仅分页、无 org/workspace 过滤，返回租户级 email/roles/attributes；`AddMembershipRequest` 仅 organization 级。若 workspace-admin 邀请成为必需，另加 scoped `identity:search` 或 workspace membership API，不放宽整个 list。
4. **审计存储**：v1 使用 stdout JSON，不建 DB 表、不实现 AuditService；生产由部署方集中收集，要求 append-only、访问控制、保留期（建议不少于 90 天，最终按合规确定）；审计 writer 必须可返回错误并 fail-closed。若后续合规或安全评审要求 WORM/应用内查询，另开 `audit-store` change 并作为生产前置。
5. **OpenFGA 版本与模型上传**：固定经过验证的具体 1.x 版本（不用 latest）；独立服务、仅集群内访问、token/TLS；由部署方在流水线上传 model 并记录 model ID/摘要；应用只读，校验 store ID、model ID/摘要与零关系元组；每个环境独立 store；集成测试使用同版本 testcontainers；不改 `deploy/compose/`。
6. **`object` 类型名**：实现前用固定版本 OpenFGA parser 验证；通过则保留 `object`；不通过仅将 FGA 类型改为 `ontology_object`，API/proto/资源名仍为 `object`，代码显式映射；Cedar `Object` 不变。
7. **项目级收窄**：v1 继续 defer；不读 `core.project_resources` 做授权，不写 project 元组；后续 change 按 §12.2 回填并移除 `organization.member` 的 tenant 继承。
8. **markings**：v1 继续 defer；不读列、不写策略、不宣称已强制；本次评审已接受该 defer；若后续合规要求行级标记，作为阻塞性的 `object-markings` change，不能在本 change 假执行。
9. **service account/group**：v1 只认 user subject；`group`/`service_account` 类型与关系保留但不写元组；实现前确认 CLI/机器客户端使用用户 token；若 Stage 2 需要机器身份，另开 change 增加 subject 类型与元组构造，不在本 change 临时放行。
10. **`dataset_permissions`**：保持不读不写，不作为第二授权源；后续清理或纳入统一模型，本 change 不双写、不迁移。

### 13.3 签核

| 角色 | 姓名 | 结论 | 日期 |
| --- | --- | --- | --- |
| 项目负责人 | klb3713 | 确认 §13.2 决策 | 2026-11-22 |
| 安全评审 | 项目负责人（其他会话确认） | 通过 | 2026-11-22 |
| 实现 owner |  | 待签核 |  |

> 负责人确认安全评审通过（其他会话）；实现 owner 尚未签核。本文状态为定稿，实现由 `authorization-openfga-cedar` change 单独授权。

## 14. 变更控制

- 本文的模型、策略、默认拒绝矩阵、审计字段任一变更，都必须更新本文、提升相应版本（模型 model id / 策略 bundle / 审计 schema_version），并重新进行安全评审。
- 实现中发现本文不可行时，先修订本文并评审，再改代码；不得用“实现细节”绕过契约。
- 任何新增动作组、角色、默认放行、审计字段或持久元组，都属于边界扩展，必须走新的 change 或修订本文，不得在 `authorization-openfga-cedar` 内临时加入。
- 本文不替代 OpenSpec delta spec；change 启动后仍需 proposal/specs/design/tasks/traceability，且 delta spec 必须与本文一致。

## 15. 实现状态

截至 2026-11-22：

- 本文为定稿（负责人已确认、安全评审通过，2026-11-22）。`authorization-openfga-cedar` change 已按负责人授权启动实施。
- 已实现：`internal/platform/authz/`（catalog/config/types/errors/policies/cedar/tuples/fga/audit/metrics/authorizer/guard/explain/module/authtest 及单测）与 `policies/`（`openfga/model.fga`、`platform/schema.cedar.json`、4 个策略文件）。
- 模型：20 个类型、88 个叶子动作 + 6 个动作组；策略 ID `<相对路径>#<序号>`；`schema_version=1`；`model_digest=sha256:1e3e94edf683a76fa21cceb312bb90d6044123ea644bb1c8ce30ad097566ec8f`；`bundle_digest=sha256:cfeb43a17994ec601dda5db747f47a70b81e0df41692060bf1cd3dd12a414b6b`（2026-11-22 本地构建；生产以部署时上传返回的 model_id 为准）。
- 9 个业务模块（datasets/tenancy/ontology/objects/connectors/ingestion/pipelines/actions/query）的 `api/` 接缝已接入统一 guard；`internal/app`/`cmd` 接线与 `go mod tidy` 按所有权归 `stage-2-integration`，尚未完成，因此当前仓库尚未在运行态启用授权。
- 未实现/未覆盖：audit 与 identity HTTP 接缝（无对应 handler）、`object:batch_upsert`（无路由）、项目级收窄、markings、持久关系元组、Valkey 缓存。审计 v1 仍为 stdout JSON，无数据库与查询 API。
- 验证证据见 `openspec/changes/authorization-openfga-cedar/evidence/2026-11-22-authz-verification.md`；真实 OpenFGA testcontainers 集成基础场景通过，24 条负向矩阵与 12 项 handler 集成仍待补齐。
- 上述状态不构成 Stage 2 退出或生产验收证据；Stage 2 验收以集成后的真实运行记录为准。
