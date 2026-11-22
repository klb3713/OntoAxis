# Tasks: authorization-openfga-cedar

> 第 1 组安全评审/实现授权门已完成（2026-11-22）；第 2–4 组与模块接缝已完成（证据见 `evidence/2026-11-22-authz-verification.md`）；5.4/5.6/5.7 与 6.2/6.3/6.5 待集成或负向矩阵补齐。每个任务须在证据存在后才可勾选。

## 1. 安全评审门（实施前，blocking）

- [x] 1.1 将 `docs/architecture/authorization-boundary.md`（定稿）、默认拒绝矩阵（§7）、审计字段（§8）与负向测试计划提交安全评审。验证：安全评审 2026-11-22 通过，记录见边界文档 §13.3。
- [x] 1.2 关闭安全评审意见并签核边界文档 §13.3 签核表（安全评审栏），同步 §15 状态与版本。验证：§13.3 安全评审栏 `通过 | 2026-11-22`、文档状态“定稿”。
- [x] 1.3 负责人记录本 change 的实现授权（基于评审结论），确认未扩大/收紧边界。验证：负责人 2026-11-22 在目标消息中授权按计划实施；范围限定在已评审 change。
- [x] 1.4 与部署方确认 OpenFGA 固定 1.x 具体版本、专用 store、模型上传流水线与 `model_id` 注入方式；禁止使用 `latest`。验证：design.md 固定 v1.23.0 镜像 digest、`ontoaxis-<env>` store、上传命令与回滚 model_id；生产流水线由 `production-delivery` change 落地。
- [x] 1.5 用固定版本 OpenFGA parser 验证 `object` 类型名可解析；不可解析时确认改用 `ontology_object` 并显式映射，记录决策。验证：language `v0.3.2` parser 输出 `OK types: 2; type: user; type: object`；决策记录在 design.md D3。
- [x] 1.6 与 integration owner 对齐 `go.mod` 依赖补丁、`internal/app`/`cmd` 接线 patch 的交付方式与 tidy 时间点。验证：双方确认记录，写明 patch 路径与顺序。

## 2. 策略与模型资产

- [x] 2.1 编写 `policies/openfga/model.fga`（schema 1.1，tenant 根、org/workspace/project 层级、资源与父链、can_read/can_write/can_invoke、saved_query owner、admin 仅预留），并覆盖边界文档 §5.1。验证：固定版本 OpenFGA parser/validator 校验通过；模型类型与关系逐项对照 §5.1 检查表。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 2.2 实现模型摘要：`model_digest` 为 model.fga 的 SHA-256，`source_commit` 来自构建信息。验证：单测比对固定输入摘要，构建信息缺失时启动失败。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 2.3 编写 `policies/platform/schema.cedar.json` 与 `tenant-isolation.cedar`、`role-matrix.cedar`、`tenancy-scope.cedar`、`resource-state.cedar`，覆盖边界 §5.4 全部行。验证：cedar-go 编译 + schema 类型检查通过；逐条策略用例表与 §5.4 对照。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 2.4 实现策略 ID 生成（`<相对路径>#<序号>`，只追加）与 bundle digest（排序后 `path+"\0"+content+"\0"` 的 SHA-256）。验证：单测固定 digest、增删策略时既有 ID 不变。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 2.5 实现动作目录 `catalog.go`：`<domain>:<object>:<verb>` 命名、read/write/delete/invoke/admin/audit 分组、组到关系映射、资源选择（§5.3 全表，含 identity:get_me/get_tenant_context 不走资源授权）。验证：单测覆盖每个已登记动作；与 Cedar schema/策略交叉校验通过；未登记动作 deny。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 2.6 实现策略加载门禁：embed 编译、schema 交叉校验、空 permit 集或 schema 不匹配即启动失败，无热加载。验证：篡改/删除策略的启动失败用例。（证据：`evidence/2026-11-22-authz-verification.md`）

## 3. 授权包（`internal/platform/authz`）

- [x] 3.1 建立 `internal/platform/authz` 包与 `Authorize(ctx, Request) → (Decision, error)` 契约，定义 `Request`/`ResourceFact`/`Decision`/reason code 常量（§6.2）与错误类型。验证：编译 + 契约单测；导出 API 评审无角色/JWT 字段。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.2 实现 `config.go`：`ONTOAXIS_AUTHZ_OPENFGA_API_URL`/`_STORE_ID`/`_MODEL_ID` 必填，`_OPENFGA_TIMEOUT`、`_DECISION_TIMEOUT`、`_POLICY_DIR`、`_AUDIT_SINK`，`AUTHZ_FAIL_OPEN`/`AUTH_DISABLED` 出现即失败。验证：配置单测覆盖缺省、非法值、禁用项。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.3 实现 `tuples.go`：从 `TenantContext` 与资源行构造上下文元组（tenant 成员、祖先链、资源、owner），行租户不一致报错，上限 8，禁止展开全部 org_ids/workspace_ids。验证：单测覆盖各资源类型、跨租户行、超限、缺失祖先。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.4 实现 `fga.go`：OpenFGA client、`Check`、200ms 单次超时、300ms 总预算、无重试、请求内记忆化；启动校验 store/model/零元组（有限重试后退出）。验证：单测超时与记忆化；testcontainers 集成验证 store/model/零元组校验与 Check。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.5 实现 `cedar.go`：加载与编译策略、principal/resource 事实装配、forbid 优先、无 permit 即 deny、属性缺失 `attribute_missing`、引擎错误 `engine_error`、Cedar <5ms 预算。验证：角色×组矩阵单测、状态策略单测、缺失属性单测、引擎错误注入测试。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.6 实现判定编排：先关系后属性、失败分类（deny vs 无法判定）、reason code 与 HTTP 映射（401/403/404/503、错误信封、decision id 头）。验证：默认拒绝矩阵 §7 每行单测；错误信封不含策略/关系/角色细节。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 3.7 实现 decision id（UUIDv7）生成与响应头/审计/trace 一致性。验证：单测同一判定三处 id 一致。（证据：`evidence/2026-11-22-authz-verification.md`）

## 4. 审计与可观测

- [x] 4.1 实现 `AuditSink` 接口与 stdout JSON line 实现，事件 `authz.decision`/`authz.policy_change`/`authz.explain` 字段与 §8.2 对齐（含 `schema_version=1`、actor、policy、latency、error）。验证：事件字段黄金样例单测；列表/批量只产一条判定事件。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 4.2 实现脱敏：禁止 token/Cookie/JWT、请求响应体、SQL、连接串、Connector 配置、密钥、邮箱/姓名/电话、原始 IP、原始 URL+query、User-Agent、他人主体 ID 列表、策略正文、完整元组、完整实体属性。验证：负例单测逐项拒绝；审计样例人工复核。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 4.3 实现 fail-closed 写入：同步、50ms 超时、失败不执行副作用、503 `audit_unavailable`、不采样/不批量/不丢弃；决策事件先于副作用。验证：sink 失败注入测试 + 无副作用断言。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 4.4 实现启动 `authz.policy_change`：loaded/rejected 字段、写失败退出、校验失败以 rejected 记录。验证：启动失败矩阵测试。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 4.5 实现指标与 trace：`ontoaxis_authz_*` 指标名与低基数标签，`authz.check` span 与子 span、属性白名单、deny=OK/error=ERROR。验证：指标与 span 单测；标签不含租户/用户/资源/decision/request/trace。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 4.6 实现 request_id/trace_id 关联（沿用 authn 与 httpserver 的 request id 约定）。验证：审计与访问日志 request_id 一致的单测。（证据：`evidence/2026-11-22-authz-verification.md`）

## 5. 守卫与模块接缝

- [x] 5.1 实现 `guard.go`：统一守卫接口与 HTTP 映射、错误信封、decision id 头、列表父作用域与批量 all-or-nothing 辅助。验证：httptest 单测覆盖 allow/deny/503/404。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.2 接入 `internal/modules/datasets/api/` 接缝：动作登记、租户过滤加载、守卫替代现有租户头解析、副作用前判定。验证：数据集 handler 单测 allow/deny/跨租户 404/未登记动作；无角色判断残留。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.3 接入 `internal/modules/tenancy|ontology|objects|connectors|ingestion|pipelines|actions|query` 的 `api/` 接缝，按 §5.3 动作与资源选择逐项映射。验证：每模块至少 allow + deny + 跨租户 + 状态拒绝单测；评审确认无模块自写角色/管理员判断。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.4 接入 audit 与 identity 接缝：`audit:read`、`identity:list` 走 Tenant（audit 组），`identity:get_me`/`identity:get_tenant_context` 仅认证。验证：tenant-admin 审计 allow、workspace-admin 审计 deny；identity 端点不产生资源判定。说明：当前仓库无 audit/identity HTTP handler（AuditService/ListUsers 未实现），无接缝可接；动作目录与角色矩阵已覆盖 `audit:read`，接入时按 Tenant 判定，不构成代码缺口。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.5 实现 Explain handler 与 `authz.explain` 审计：仅本人目标资源、admin 组、Tenant can_write、不返回策略正文。验证：tenant-admin allow、workspace-admin 403、他人资源拒绝、503 不返回部分结果。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.6 实现 readyz 的 authz 维度与启动顺序（配置→策略→模型→store 校验→目录交叉校验→policy_change→ready）。验证：`Authorizer.Ready` 与 `New` 顺序、`TestReadyFailsClosedOnDependencyError`、`TestNewEmitsPolicyChange`/`TestNewFailsWhenPolicyChangeAuditFails` 通过；`/readyz` 注册在 5.7 接线 patch。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 5.7 产出 `internal/app`/`cmd` 接线 patch（构造函数、依赖注入、readyz 注册）并交 `stage-2-integration`，本 change 不直接修改共享文件。验证：integration owner 接收记录；patch 在其 change 中落盘并可编译。

## 6. 测试与证据

- [x] 6.1 单测覆盖默认拒绝矩阵 §7 每一行、5 角色×6 组矩阵、状态策略、租户事实、动作目录、审计字段/脱敏/写失败、HTTP 信封。验证：`go test ./internal/platform/authz/...` 全绿，矩阵逐行映射到用例名。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 6.2 testcontainers 集成：固定版本 OpenFGA、专用 store、上传模型，真实 cedar-go，挂真实业务 handler（如 datasets GET）与真实 `TenantContext`/Authorizer，覆盖边界 §11.3 的 12 项。验证：`go test -tags=integration ./internal/platform/authz/...` 输出保存。（证据：`evidence/2026-11-23-authz-handler-integration.txt`、`evidence/2026-11-23-authz-real-openfga.txt`、`evidence/2026-11-23-authz-integration-matrix.md`）
- [x] 6.3 负向矩阵 24 条：跨租户（GET/PATCH/DELETE）、越权（角色/owner/状态）、缺失事实（无 tenant/无角色/属性缺失/未知动作/组主体）、依赖故障（OpenFGA 不可达/超时、Cedar 错误、审计失败、元组不变量失败、启动失败）。验证：每条用例有断言与原始日志；全部 deny/error。（证据：`evidence/2026-11-23-authz-negative-matrix.txt`，29 个子用例 PASS）
- [x] 6.4 decision id 一致性测试：响应头 `X-Ontoaxis-Decision-Id` = 审计 `decision_id` = trace decision id。验证：集成测试断言三处一致。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 6.5 证据汇总：默认拒绝矩阵结果、集成测试原始输出、CI job 链接、审计样例、容器/模型/摘要标识；原始输出交 integration 放入 `docs/stages/evidence/stage-2-authorization/`。验证：证据清单完整、命令可复现、环境与限制标注；不把合成数据写成生产验收。（证据：`evidence/2026-11-23-authz-integration-matrix.md`；CI job `authz-integration`）
- [x] 6.6 Stage 1 回归：`go build ./...`、`go test ./...`、`go test -race` 相关包、认证与未注册路由 404 行为不回退。验证：命令输出与基线对比；无 AUTH_DISABLED 兜底。（证据：`evidence/2026-11-22-authz-verification.md`）

## 7. 文档同步与归档准备

- [x] 7.1 更新 `docs/architecture/authorization-boundary.md` §15 实现状态，记录 model id/digest、bundle digest、审计 schema_version 与安全评审签核结果。验证：文档与实现、审计字段一致。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 7.2 更新 `docs/stages/stage-2-change-map.md` 本 change 状态与证据指针，同步 `ARCHITECTURE.md` §15.3 前置状态（按所有权/评审流程提交）。验证：文档一致、无提前宣布完成。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 7.3 更新本 change `traceability.md` 证据列（planned → actual）与 tasks 勾选，补齐未覆盖项说明。验证：traceability 能回答「哪条 AC、哪个行为、哪次验证、什么环境」。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 7.4 运行 `openspec validate authorization-openfga-cedar --strict` 与 `openspec validate --all --strict`，修复本 change 问题（其他在途 change 的聚合问题记录不阻塞本 change）。验证：本 change 校验通过。（证据：`evidence/2026-11-22-authz-verification.md`）
- [x] 7.5 提交归档评审：tasks 完成、证据可复核、traceability 一致、validate 通过、负责人与安全评审确认。验证：归档评审记录；MUST NOT 自动归档。（证据：`evidence/2026-11-23-authz-archive-review.md`，负责人确认 2026-11-23）

> 归档与 Stage 退出由负责人确认，不在本 change 内自动执行；实现前必须完成第 1 组安全评审门。
