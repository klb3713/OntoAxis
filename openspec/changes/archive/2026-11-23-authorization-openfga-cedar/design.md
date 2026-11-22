# Design: authorization-openfga-cedar

## Context

- Stage 2 S2-AC-03 要求 OpenFGA 关系授权 + Cedar 属性/动作策略生效，默认拒绝，负向路径可复核；本 change 是 Wave 0 的最后一个 change，前置 `edge-gateway` 已归档（`runtime-observability` 亦已归档）。
- 现状：`internal/platform/authn` 已提供 `TenantContext` 与认证中间件；`internal/platform/httpserver` 提供路由、readyz、request id、recoverer、access log、metrics；`internal/app` 有模块注册表；`internal/modules/*/api/` 的 handler 只做内部租户头绑定与输入校验，没有对象级授权。`internal/platform/authz/` 与 `policies/` 不存在，`go.mod` 无 OpenFGA/cedar-go/testcontainers 依赖。
- 唯一行为边界是 `docs/architecture/authorization-boundary.md`（定稿；负责人与安全评审于 2026-11-22 确认）。本设计逐条对应其 §4–§12，不新增动作组、角色、默认放行、审计字段或持久元组。
- 文件所有权（边界文档 §12.7、change map）：本 change 拥有 `internal/platform/authz/`、`policies/` 与本设计产物；只修改 `internal/modules/*/api/` 授权接缝；`internal/app/`、`cmd/` 归 `stage-2-integration`（提供接线 patch）；`deploy/` 归 `production-delivery`；`docs/stages/evidence/` 归 integration；`web/` 不涉及。
- `docs/architecture/authorization-boundary.md` 与 `ARCHITECTURE.md` §6.2.1/§6.2.3 的旧草图冲突时，以边界文档为准（ARCHITECTURE §6.2 末尾注记）。

## Goals

- 一个判定点：所有业务动作经 `internal/platform/authz.Authorize`，模块只声明动作与事实，消除模块自写的角色/租户/管理员判断。
- 默认拒绝：未知、缺失、超时、引擎错误、审计失败一律拒绝或 503，无降级 allow。
- 行为可复核：每次判定写 append-only 审计事件，decision id 可在响应头、审计与 trace 中对齐。
- 可诊断：Explain 只解释调用者本人对目标资源的决策，不泄露策略正文与他人信息。
- 可测试：真实 OpenFGA + 真实 cedar-go 的集成测试，覆盖跨租户、越权、缺失事实、依赖故障。
- 安全评审门：安全评审已于 2026-11-22 通过，负责人已记录实施授权；实现不得扩大或削弱边界。

## Non-Goals

- 不实现项目级收窄、markings、持久关系元组、outbox、`dataset_permissions` 读写、审计数据库/查询 API。
- 不引入 Valkey/跨请求缓存、策略热加载或策略管理 API。
- 不改 `deploy/compose|helm|argocd`、不改 `web/`、不接生产 IdP。
- 不在本 change 内调整模块 service/repo 的领域语义（除事实加载已存在）；模块级 delta spec 同步由对应模块 change 完成。
- 不做权限管理 UI、不做角色/成员管理写接口（`membership:add/remove` 仅作为被授权动作类型存在）。

## Decisions

### D1 包边界与依赖方向

`internal/platform/authz/` 单包，内部按职责分文件：

- `config.go`：`ONTOAXIS_AUTHZ_*` 解析、禁用项检测（`AUTHZ_FAIL_OPEN`、`ONTOAXIS_AUTH_DISABLED` 出现即失败）。
- `decision.go`：`Authorize(ctx, Request) (Decision, error)`、`Request`/`ResourceFact`/`Decision`、reason code 常量。
- `catalog.go`：动作目录与资源选择（边界文档 §5.3），启动时与策略 schema 交叉校验。
- `tuples.go`：上下文元组构造、祖先链、上限 8、租户一致性。
- `fga.go`：OpenFGA client 封装、`Check`、200ms 超时、请求内记忆化。
- `cedar.go`：embed 策略、编译、schema 校验、求值、策略 ID、bundle digest。
- `audit.go`：`AuditSink` 接口、stdout JSON 实现、脱敏、50ms、fail-closed。
- `guard.go`：模块守卫与 HTTP 映射（403/404/503、错误信封、decision id 头）。
- `errors.go`/`metrics.go`/`trace.go`：错误码、指标、span。

依赖方向：`internal/modules/*/api` → `authz.Guard`/`authz.Decision` → `authz` 核心；authz 包不 import 任何 module；module 不 import `authz` 的 fga/cedar 子实现（只依赖守卫与决策类型）。备选方案：把判定放 HTTP middleware 按路由表推断（拒绝：资源事实必须由 handler 在租户内加载，middleware 无法可靠拿到）；把策略放各模块（拒绝：违反单一判定点，产生冲突检查）。

### D2 请求事实模型

```text
Authorize(ctx, Request) → (Decision, error)
Request{Action string, Resource ResourceFact}
ResourceFact{Type, ID, TenantID, Ancestors []Ancestor, Status, OwnerID string}
Decision{Allowed bool, Reason string, DecisionID string}
```

- `Action` 必须是动作目录中已登记项；`Resource` 事实由 handler 在**租户过滤加载后**填充，`Status`/`OwnerID` 只来自数据库行。
- 角色、tenant_id、org_ids、workspace_ids 只从 `authz` 包内从 ctx 的 `TenantContext` 读取，**不进入 `Request`**，调用方无法伪造。备选：把 roles 放进 Request（拒绝：扩大伪造面）。
- `DecisionID` 必返（UUIDv7），用于响应头、审计、trace 对齐。
- 失败必须区分「决策 deny」（返回 Decision + nil error，映射 403/404）与「无法判定」（返回 error，映射 503），避免把引擎故障误当业务拒绝。

### D3 OpenFGA 客户端、模型与上下文元组

- 模型文件 `policies/openfga/model.fga`（schema 1.1，边界文档 §5.1）：tenant 为唯一根；organization/workspace/project 租户内层级；资源直接挂 tenant；object/ingest_job/pipeline_run/pipeline_schedule/action_execution 经父链；关系 `can_read`/`can_write`/`can_invoke`；saved_query 额外 `owner`（can_write=owner）；admin 关系仅预留。
- 专用 store，业务元组必须为 0：启动时校验 store 可读、model 可读且 `model_id` 匹配、store 元组数为 0，有限重试后失败退出。模型由部署流水线上传，应用只读。
- 应用配置 `ONTOAXIS_AUTHZ_OPENFGA_API_URL/STORE_ID/MODEL_ID`，记录 `model_digest`（model.fga 的 SHA-256）；`source_commit` 来自构建信息。
- 上下文元组（边界 §5.2）：固定 `tenant:<tenant_id>#member@user:<sub>`；org/workspace 成员元组仅在 JWT 声明且属祖先链时加入；资源元组按行 tenant_id 追加父链；owner 边仅 user；上限 8，超限 `internal_error` 503。
- 超时：单次 `Check` 200ms，判定总预算 300ms，Cedar <5ms；无重试、无跨请求缓存；允许请求内按 `subject|relation|object` 记忆化（仅本次请求）。
- 集成测试用 testcontainers 固定 OpenFGA 1.x 具体版本（版本号在 tasks 1.4 记录，禁止 `latest`），每测试专用 store、上传模型后执行。
- 运行契约（2026-11-22 记录）：store 命名 `ontoaxis-<env>`；部署流水线执行 `openfga model write --store-id "$STORE" --file policies/openfga/model.fga`，捕获返回的 model ID 并注入 `ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID`；应用只读、不写元组。回滚时把 `MODEL_ID` 指回上一版本（旧模型保留在同一 store），应用无需改代码。生产部署由 `production-delivery` 落地，本 change 固定契约并提供 testcontainers 夹具。
- 固定版本（2026-11-22 决策）：OpenFGA server `v1.23.0`（镜像 manifest `sha256:2a1b4ef95f2f27e6d420f11cf14390b4f537e699de8d18b6a07d8b355e11cf02`）、Go SDK `v0.8.4`、language parser `v0.3.2`、cedar-go `v1.9.3`、testcontainers-go `v0.39.0`。`object` 类型已用 language `v0.3.2` parser 验证可解析，保留 `object` 类型名。

### D4 Cedar 策略集与求值

- 资产：`policies/platform/schema.cedar.json`、`tenant-isolation.cedar`、`role-matrix.cedar`、`tenancy-scope.cedar`、`resource-state.cedar`；`go:embed` 打包，启动编译并与 schema 交叉校验，无热加载。
- 策略 ID 为 `<相对路径>#<序号>`，稳定且只追加；bundle digest = 按路径排序后拼接 `path+"\0"+content+"\0"` 的 SHA-256；与 model_digest、model_id、source_commit 一起写入审计。
- 求值语义：forbid 优先；无 permit 即 deny；属性缺失/类型不符 deny `attribute_missing`（不补默认值）；引擎错误/panic/超时 503 `engine_error`；空/未知角色 deny；加载失败 = 启动失败，不得以空策略继续。
- 角色矩阵（边界 §5.4）：read=viewer+；write/delete/invoke=member+；admin=workspace-admin+；audit=tenant-admin+；`tenancy-scope` 约束组织创建/更新/删除与成员增删仅 tenant-admin/platform-admin（workspace-admin 可创建 workspace）；`resource-state` 约束 pipeline/action_type/connector/ingestion/action_execution 状态（见边界 §5.4）。
- 备选：OPA/Rego（拒绝：架构已选定 Cedar in-process）；策略热加载（拒绝：审计一致性与撤销风险）。

### D5 守卫与模块接缝

- `authz.Guard` 提供 `Authorize(ctx, req) error` 语义：成功返回 nil，deny 返回带 Decision 的类型化错误，无法判定返回引擎错误；HTTP 层集中映射 401/403/404/503、错误信封与 `X-Ontoaxis-Decision-Id`。
- handler 顺序固定：认证（401）→ 输入校验（400）→ 租户过滤加载（404）→ 守卫 → 仅 allow 执行副作用。审计写入发生在副作用之前（由 authz 包内保证）。
- 列表只判父作用域一次（Tenant/Organization/Workspace/父资源），查询必须带租户过滤，逐行不调用 OpenFGA；批量写对目标 ObjectType 判一次并要求同租户同类型，任一失败整批拒绝且无副作用。
- 模块只依赖守卫与决策类型，不直接 import OpenFGA/Cedar；不得把角色写进 SQL/HTTP 判断；`dataset_permissions` 不读不写。
- 接缝仅改 `internal/modules/*/api/`；`internal/app/`、`cmd/` 的构造函数与接线以 patch 形式交 `stage-2-integration`，由其在集成 change 中落盘与 tidy。

### D6 审计事件形状

- `AuditSink.Emit(ctx, Event) error`；默认实现写 stdout 一行 JSON：顶层 `{level,time,msg=event_type}`，业务字段在 `authz` 对象内（边界 §8.2）。
- 事件：`authz.decision`（每次判定一次，allow/deny/error；列表/批量一次）、`authz.policy_change`（启动，loaded/rejected）、`authz.explain`；`authz.relationship_change` 仅预留，v1 不产生。
- 字段固定：`schema_version=1`、`event_id`(UUIDv7)、`event_type`、`occurred_at`(RFC3339Nano UTC)、`tenant_id`、`request_id`、可选 `trace_id`/`span_id`、`actor{type,id,roles}`、`action`、`resource{type,id,tenant_id,count?}`、`decision`、`reason_code`、`decision_id`、`policy{fga_model_id,fga_relation,fga_decision,cedar_policy_ids,cedar_decision,bundle_digest}`、`http{method,route}`、`latency_ms{fga,cedar,total}`、`error{engine,code,message}`；`policy_change` 与 `explain` 用各自字段集。
- 同步写、50ms 超时、失败 fail-closed（不执行副作用，映射 503 `audit_unavailable`）；不采样、不批量、不丢弃；轮转与保留（建议 ≥90 天）由部署方负责，安全评审确认。
- 脱敏在 sink 前完成：禁止 token/Cookie/JWT、请求响应体、SQL、连接串、Connector 配置、密钥、邮箱/姓名/电话、原始 IP、原始 URL+query、User-Agent、他人主体 ID 列表、策略正文、完整元组、完整实体属性。

### D7 启动、配置、就绪与可观测

- 配置键（边界 §9.2）：`ONTOAXIS_AUTHZ_OPENFGA_API_URL`/`_STORE_ID`/`_MODEL_ID` 必填；`_OPENFGA_TIMEOUT` 默认 200ms；`_DECISION_TIMEOUT` 默认 300ms；`_POLICY_DIR` 默认空（=嵌入，仅测试/开发）；`_AUDIT_SINK` 仅 `stdout`；`AUTHZ_FAIL_OPEN` 与 `ONTOAXIS_AUTH_DISABLED` 出现即启动失败。
- 启动顺序：配置校验（拒绝禁用项）→ 编译 Cedar 与 schema、计算摘要 → 解析 model.fga、连接 OpenFGA、校验 store/model/零元组（有限重试后退出）→ 动作目录交叉校验 → 写 `authz.policy_change` 审计（失败退出）→ ready（readyz 含 authz）→ 接流量。运行期 OpenFGA 故障返回 503 并反映到 readyz，不降级 allow。
- 指标：`ontoaxis_authz_decisions_total{action,decision,reason}`、`ontoaxis_authz_duration_seconds{engine}`、`ontoaxis_authz_errors_total{engine,code}`、`ontoaxis_authz_policy_loaded{result}`、`ontoaxis_authz_ready`；禁止 tenant/user/resource/decision/request/trace 标签。
- Trace：`authz.check`（子 span `authz.fga.check`/`authz.cedar.eval`/`authz.audit.emit`）；属性仅 action/decision/reason/fga.relation/cedar.policy_ids/latency_ms；deny=OK，error=ERROR。

### D8 Explain

- 动作 `authorization:explain` 登记为 admin 组，资源选择 Tenant，要求 `can_write`。
- 只允许调用者本人对目标资源的决策；返回 allowed、命中的关系名、命中的 Cedar 策略 ID、decision id；不返回策略正文、他人主体 ID、资源内容；每次 Explain 写 `authz.explain`；失败 503 且不返回部分结果。

### D9 交付、门禁与回滚

- 实现顺序：安全评审签核（已完成，2026-11-22）→ 策略资产与动作目录 → authz 包与单测 → testcontainers 集成测试 → 模块接缝逐个接入 → 接线 patch 交 integration → 证据汇总 → validate/archive（人工确认，不自动归档）。
- CI：`go build/test/vet/lint`，另加 docker job 跑 `go test -tags=integration ./internal/platform/authz/...`；证据原始输出交 integration 放入 `docs/stages/evidence/stage-2-authorization/`。
- 回滚：上一镜像 + 上一 `model_id`/策略 bundle；MUST NOT 通过 `AUTH_DISABLED`/关闭策略/默认放行回滚。
- 依赖：OpenFGA SDK、cedar-go、testcontainers 固定版本；`go.mod`/`go.sum` 补丁由本 change 提供、integration tidy。

## Risks / Trade-offs

- **安全评审未签核或结论改变边界**：实现阻塞、返工。缓解：tasks 第 1 组为门禁；边界变更先改文档再改代码（边界 §14）。
- **OpenFGA 成为请求路径强依赖**：可用性风险、503 面扩大。缓解：200ms/300ms 预算、readyz 反映、部署冗余（由 production-delivery 负责）、固定版本。
- **Stage 1 客户端行为回归**：角色矩阵与状态策略收紧是 **BREAKING**。缓解：负向测试逐条覆盖、发布说明、证据记录；不做 shadow 模式（边界禁止）。
- **`object` 类型名与固定 parser 兼容性**：需在 tasks 1.4 用固定版本 parser 验证；不通过则 FGA 类型改 `ontology_object` 并显式映射（负责人已确认方向；仍需安全评审通过、记录并同步文档）。
- **审计量/成本与保留期**：每请求一行 JSON。缓解：stdout 由部署方轮转；保留期建议 ≥90 天待安全评审/合规确认。
- **模块接缝面广（9 个模块）**：列表/批量语义易错、测试量大。缓解：分阶段接入、每模块独立测试、列表父作用域与批量 all-or-nothing 单测强化。
- **集成测试基础设施稳定性**：容器/镜像抖动。缓解：固定镜像、专用 store、有限重试、CI docker job 独立。
- **共享文件所有权冲突**：`internal/app/`、`cmd/`、`deploy/`、`go.mod` 由其他 change 拥有。缓解：以 patch 交付、由 integration 落盘 tidy；本 change 不直接改共享文件。
- **合成环境证据不等于生产验收**：证据必须标注环境与限制，不写成生产验收。

## Migration Plan

- 无数据库迁移；v1 不写持久关系元组；不读写 `dataset_permissions`。
- 上线顺序：安全评审签核并记录实现授权 → 上传 v1 模型到目标环境专用 store（流水线，应用只读）→ 部署含策略 bundle 的镜像（启动校验、`authz.policy_change`）→ 观察 readyz/指标/审计样本 → 逐模块启用守卫（同一二进制内按动作目录生效，无运行期开关）。
- Stage 1 → v1 行为收紧：viewer 只读；写/删/调用 member+；组织/工作空间/项目管理 workspace-admin+；SavedQuery 删除 owner+delete 组；Pipeline 仅 published 触发；ActionType 仅 active；Connector 仅 active；执行取消/重试状态机。该差异需进入发布说明，并由对应模块 change 在其 delta spec 中同步（本 change 先在 `authorization` 契约中锁定）。
- 回滚：切回上一镜像与上一 `model_id`/策略 bundle；禁止关闭授权或使用 `AUTH_DISABLED`。

## Open Questions

- 安全评审已确认项（2026-11-22）：撤销延迟（JWT 角色/成员变化传播）、审计保留期与合规要求、OpenFGA 生产部署形态（由 production-delivery 定）、`authorization:explain` 的暴露范围（tenant-admin+）。
- 模块 delta spec 同步归属：`query-surface`、`pipelines`、`actions`、`connectors`、`ingestion`、`tenancy` 的收窄表述由各自 change 完成；若负责人要求与本 change 同批，则需要在本 change 增加对应 MODIFIED 规格并重新评审。
- `object` FGA 类型最终名（待 tasks 1.4 验证后记录）。
- `go.mod`/`go.sum` 依赖版本由 integration 统一 tidy 的时间点（需在实现前与 integration owner 对齐）。
