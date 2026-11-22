# authorization Specification

## Purpose
定义 OntoAxis 业务请求的统一授权契约：所有业务动作 SHALL 经过 `internal/platform/authz` 的单一判定点，由 OpenFGA 关系事实与 Cedar 属性/动作策略共同决定 allow/deny；任何缺失、错误或依赖故障 MUST 默认拒绝；每次判定 MUST 产生 append-only 审计事件，并支持 Explain 诊断。本规格是 Stage 2 S2-AC-03 的行为契约，与 `docs/architecture/authorization-boundary.md`（定稿，负责人与安全评审 2026-11-22 确认）一致，不扩大也不削弱其边界。

## Requirements

### Requirement: 单一授权判定点

所有注册业务动作 MUST 由 `internal/platform/authz` 的 `Authorize` 契约判定。业务模块 MUST 只声明登记动作与资源事实并调用守卫，MUST NOT 自写角色、同租户或管理员判断，MUST NOT 直接调用 OpenFGA 或 Cedar。每个请求 MUST 且只能产生一次授权判定（列表/批量请求只判定一次）。动作目录之外的动作 MUST 被拒绝（`unknown_action`）。系统 MUST NOT 支持 `AUTH_DISABLED`、`AUTHZ_FAIL_OPEN`、默认 Principal、匿名兜底或 shadow 模式。

#### Scenario: 业务动作进入统一判定

- **WHEN** 已认证请求命中任一跳业务路由
- **THEN** 守卫在副作用前调用 `authz.Authorize`，决策返回 `decision_id`
- **AND** 模块代码不读取角色做放行判断（由评审与静态检查复核）

#### Scenario: 未登记动作拒绝

- **WHEN** 某 RPC 使用的动作不在动作目录
- **THEN** 返回 403 `permission_denied`（`reason_code=unknown_action`）且无副作用

#### Scenario: 禁用配置导致启动失败

- **WHEN** 环境出现 `ONTOAXIS_AUTH_DISABLED` 或 `ONTOAXIS_AUTHZ_FAIL_OPEN`
- **THEN** 进程在接流量前退出，readyz 不通过

### Requirement: OpenFGA 关系判定与上下文元组

关系判定 MUST 基于 OpenFGA `Check`，使用请求作用域上下文元组：固定 tenant 成员元组、已认证祖先链元组与资源元组。v1 MUST NOT 写持久业务元组（专用 store 的业务元组数 MUST 为 0），应用对模型 MUST 只读。元组 MUST 由认证后的 `TenantContext` 与租户内加载的资源行构造，MUST NOT 使用客户端租户头。单请求元组上限 8 条，超过 MUST 返回 503 `internal_error`。启动时 MUST 校验 store/model 可读且元组为空，否则退出。

#### Scenario: 租户成员读取允许

- **WHEN** tenant 成员用户读取本租户数据集
- **THEN** OpenFGA `Check` 返回 allowed，请求继续

#### Scenario: 非成员关系拒绝

- **WHEN** 用户不是租户成员
- **THEN** 决策为 deny（`no_relationship`），返回 403 且不返回资源内容

#### Scenario: store 非空启动失败

- **WHEN** 启动校验发现专用 store 中存在业务元组
- **THEN** 进程退出，并记录 `authz.policy_change`（`result=rejected`）

#### Scenario: 元组上限

- **WHEN** 构造出的上下文元组超过 8 条
- **THEN** 返回 503 `unavailable`（`internal_error`），不降级为 allow

### Requirement: Cedar 属性与动作策略

Cedar 策略集 MUST 以 `go:embed` 内嵌、启动编译并与 schema 交叉校验，MUST NOT 热加载。策略 MUST 覆盖租户隔离、角色×动作组矩阵、tenancy 范围与资源状态四类规则；求值 MUST 遵循 forbid 优先、无 permit 即 deny、属性缺失/类型不符 deny（`attribute_missing`，不得补默认值）。策略 ID MUST 为 `<相对路径>#<序号>` 且只追加；bundle digest MUST 为按路径排序拼接 `path+\0+content+\0` 的 SHA-256。

#### Scenario: 策略集覆盖四类规则

- **WHEN** 启动编译策略集
- **THEN** 租户隔离 forbid（`principal.tenant_id != resource.tenant_id`）、角色矩阵（read=viewer+；write/delete/invoke=member+；admin=workspace-admin+；audit=tenant-admin+）、tenancy 范围（organization create/update/delete 与 membership add/remove 仅 tenant-admin/platform-admin）与资源状态（pipeline archived 禁 update/delete/publish、invoke 仅 published；action_type run 仅 active；connector sync 仅 active；ingestion:cancel 仅 queued/running；action_execution cancel 仅 queued/running、retry 仅 failed/cancelled）均存在并通过 schema 校验

#### Scenario: viewer 读允许写拒绝

- **WHEN** viewer 角色用户分别执行读动作与写动作
- **THEN** 读 allow；写 deny（`role_not_permitted`）返回 403

#### Scenario: workspace-admin 组织管理拒绝

- **WHEN** workspace-admin 执行 organization update 或 membership add
- **THEN** deny（`admin_required`）返回 403

#### Scenario: 资源状态拒绝

- **WHEN** 对 archived pipeline 执行 publish，或对 disabled action_type 执行 run
- **THEN** deny（`resource_state_denied`）返回 403，且资源状态不变

#### Scenario: 属性缺失

- **WHEN** 资源事实缺少策略需要的属性（如 tenant_id/status）
- **THEN** deny（`attribute_missing`）返回 403，不套用默认值

### Requirement: 默认拒绝与依赖故障 fail-closed

任何未知、缺失、超时、引擎错误、审计失败 MUST 拒绝或返回 503，MUST NOT 放行；MUST NOT 使用跨请求授权缓存，MUST NOT 重试后放行；运行期 OpenFGA 故障 MUST 反映到 readyz 并拒绝受保护请求；策略加载失败 MUST 为启动失败，MUST NOT 以空策略集继续。授权错误响应 MUST 使用统一错误信封，MUST NOT 泄露策略正文、关系名称、角色细节或他人主体信息。

#### Scenario: OpenFGA 超时

- **WHEN** OpenFGA `Check` 在 200ms 内未返回
- **THEN** 返回 503 `unavailable`（`engine_timeout`），不执行副作用

#### Scenario: Cedar 引擎错误

- **WHEN** Cedar 求值出错或超时
- **THEN** 返回 503 `unavailable`（`engine_error`），不执行副作用

#### Scenario: 审计写失败

- **WHEN** 决策审计写入失败
- **THEN** 返回 503（`audit_unavailable`），副作用不执行且不重试放行

#### Scenario: 空策略集

- **WHEN** 策略编译产生空 permit 集或 schema 不匹配
- **THEN** 进程启动失败

### Requirement: 租户层级与资源事实

授权事实 MUST 仅来自认证后的 `TenantContext`（tenant_id、org_ids、workspace_ids、roles）与租户内加载的资源行；资源行租户 MUST 等于上下文租户，否则 MUST 按 404 处理；祖先链 MUST 来自租户内加载行；资源状态与 owner MUST 来自数据库行，MUST NOT 来自请求体；`X-Tenant-Id` MUST NOT 作为授权事实；角色 MUST NOT 写入 OpenFGA，成员关系 MUST NOT 写入 Cedar；跨租户访问 MUST 先做租户过滤再以 404 返回（MUST NOT 先查全库再 403）。`dataset_permissions` MUST NOT 被读取或写入。

#### Scenario: 行租户不一致

- **WHEN** 加载到的资源行 tenant_id 与 `TenantContext` 不一致
- **THEN** 返回 404 `not_found`，不泄露资源存在性

#### Scenario: 请求体伪造状态被忽略

- **WHEN** 请求体携带 status/owner 字段试图影响判定
- **THEN** 判定使用数据库行事实，请求字段被忽略

#### Scenario: 组织/工作空间/项目层级事实

- **WHEN** 资源属于 organization/workspace/project 层级
- **THEN** 上下文元组按已认证祖先链构造，未声明的祖先不展开

#### Scenario: SavedQuery 删除

- **WHEN** 非 owner 或 viewer owner 删除 SavedQuery
- **THEN** deny（`no_relationship`/`role_not_permitted`）返回 403；owner 且具备删除组时 allow

### Requirement: 审计决策事件

系统 MUST 为每次授权判定产生一条 append-only JSON 审计事件 `authz.decision`，为启动记录 `authz.policy_change`，为 Explain 记录 `authz.explain`；决策事件 MUST 在副作用前同步写入，sink 失败 MUST fail-closed。系统 MUST NOT 采样、批量或丢弃决策事件。MUST NOT 记录 token/Cookie/JWT、请求或响应体、SQL、连接串、邮箱、姓名、电话、原始 IP、原始 URL 与 query、User-Agent、他人主体 ID 列表、策略正文、完整元组或完整实体属性。

#### Scenario: 决策事件字段完整

- **WHEN** 产生一条 `authz.decision`
- **THEN** 事件包含 `schema_version=1`、`event_id`（UUIDv7）、`occurred_at`（RFC3339Nano UTC）、`tenant_id`、`request_id`、`actor{type,id,roles}`、`action`、`resource{type,id,tenant_id}`、`decision`、`reason_code`、`decision_id`、`policy{fga_model_id,fga_relation,fga_decision,cedar_policy_ids,cedar_decision,bundle_digest}`、`http{method,route}`、`latency_ms` 与脱敏 `error`

#### Scenario: allow 决策审计先于副作用

- **WHEN** 判定 allow 且副作用将执行
- **THEN** `authz.decision` 先写入 stdout，`decision_id` 与响应头 `X-Ontoaxis-Decision-Id` 一致

#### Scenario: deny 决策不产生副作用

- **WHEN** 判定 deny
- **THEN** 写 `authz.decision` 且不产生任何业务写入

#### Scenario: 脱敏

- **WHEN** 审计事件包含主体与资源事实
- **THEN** 仅允许 UUID、角色名、动作、关系名、策略 ID、摘要、route pattern、方法、原因码、耗时与错误码，敏感字段被排除

#### Scenario: 策略变更审计

- **WHEN** 服务启动完成策略与模型校验
- **THEN** 写 `authz.policy_change`（`result=loaded`，含 model_id、model_digest、bundle_digest、source_commit）

### Requirement: 授权守卫与模块接缝

业务 handler MUST 按顺序执行：认证（401）→ 输入校验（400）→ 租户过滤加载（404）→ `Authorize` → 仅 allow 时执行副作用或读。守卫 MUST 统一映射 401/403/404/503 与错误信封；deny MUST 带 `X-Ontoaxis-Decision-Id`；列表 MUST 只判定父作用域一次并做租户过滤；批量写 MUST 对目标 ObjectType 判定一次且要求同租户同类型，任一失败 MUST 整批拒绝且无副作用。模块 MUST NOT 把角色写入 SQL/HTTP 判断，MUST NOT 调用 OpenFGA/Cedar；`identity:get_me` 与 `identity:get_tenant_context` 仅认证、不进入资源授权。

#### Scenario: viewer 写请求

- **WHEN** viewer 对本租户资源执行 PATCH
- **THEN** 403 `permission_denied`（统一信封），无写入，带 decision id

#### Scenario: 列表只判定一次

- **WHEN** 读取本租户数据集列表
- **THEN** 仅对 Tenant 作用域判定一次，逐行不再调用 OpenFGA

#### Scenario: 批量写全有或全无

- **WHEN** 批量写入包含跨租户或不同类型对象
- **THEN** 整批 deny，无任何行写入

#### Scenario: 不泄露跨租户资源

- **WHEN** 请求本租户之外的资源 ID
- **THEN** 返回 404 `not_found`，MUST NOT 返回 403 或资源内容

### Requirement: Explain 与诊断

`authorization:explain` MUST 只允许诊断调用者本人对目标资源的决策，动作 MUST 登记为 admin 组并要求 Tenant 资源 `can_write`；结果 MUST 包含 allowed、命中的关系名、命中的 Cedar 策略 ID 与 decision id，MUST NOT 包含策略正文、他人主体 ID 或资源内容；Explain MUST 写 `authz.explain`；失败 MUST 返回 503 且不返回部分结果。指标 MUST 仅使用低基数标签（action/decision/reason/engine/code/result），MUST NOT 含 tenant/user/resource/decision/request/trace 标签。

#### Scenario: admin Explain 允许

- **WHEN** tenant-admin 请求 Explain 本人对某资源的决策
- **THEN** 返回 allowed、关系名、策略 ID 与 decision id，不返回策略正文

#### Scenario: 非 admin Explain 拒绝

- **WHEN** workspace-admin 调用 Explain
- **THEN** 403 `forbidden`（`admin_required`）

#### Scenario: Explain 不泄露他人

- **WHEN** 请求描述其他主体或非本人资源
- **THEN** 拒绝，且不返回他人决策信息

#### Scenario: 指标低基数

- **WHEN** 产生大量不同租户与资源的判定
- **THEN** 指标标签集不随租户/资源/决策 ID 增长

### Requirement: 负向测试与实现授权门

每个 Requirement 的拒绝路径 MUST 有自动化负向测试覆盖：跨租户、越权（角色/owner/状态）、缺失事实（无 tenant、无角色、属性缺失、未登记动作、组主体）与依赖故障（OpenFGA 不可达/超时、Cedar 错误、审计失败、元组不变量失败、启动校验失败）。集成测试 MUST 使用真实 OpenFGA（固定版本、专用 store、上传模型）与真实 cedar-go，并挂真实业务 handler 与真实 `TenantContext`。实现授权 MUST 在实现开始前由负责人记录，并引用已签核的 `docs/architecture/authorization-boundary.md` 与默认拒绝矩阵；实现 MUST NOT 扩大或削弱边界。证据 MUST 保留原始命令输出与容器/模型/摘要标识，MUST NOT 以合成数据或未运行命令充当证据。

#### Scenario: 跨租户负向

- **WHEN** 用户对另一租户资源执行 GET/PATCH/DELETE
- **THEN** 全部拒绝（404/403），无内容泄露且写无副作用

#### Scenario: 越权与缺失事实负向

- **WHEN** 角色、owner 或资源状态不满足，或缺 tenant/roles/属性，或使用未登记动作或组主体
- **THEN** 按边界文档 §7 默认拒绝矩阵映射拒绝（401/403），且不执行副作用

#### Scenario: 依赖故障负向

- **WHEN** OpenFGA 不可达、Cedar 出错或审计写入失败
- **THEN** 返回 503，且不执行副作用

#### Scenario: 实现授权已记录

- **WHEN** 负责人已记录实现授权且边界文档已由安全评审签核
- **THEN** 实现按本规格推进，不得扩大或削弱边界
