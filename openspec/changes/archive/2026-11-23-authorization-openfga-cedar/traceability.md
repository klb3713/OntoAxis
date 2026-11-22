# Traceability: authorization-openfga-cedar

> 本文件登记 Stage 2 AC 与规格要求到证据的映射。实现已完成到“单测 + 真实 OpenFGA 集成（含真实 datasets handler）+ 模块接缝”程度；原始输出见 `evidence/2026-11-22-authz-verification.md`、`evidence/2026-11-23-authz-handler-integration.txt`、`evidence/2026-11-23-authz-real-openfga.txt`、`evidence/2026-11-23-authz-negative-matrix.txt` 与汇总 `evidence/2026-11-23-authz-integration-matrix.md`。24 条负向矩阵与 12 项 handler 集成已补齐；剩余仅 7.5 归档评审。最终归档位置由 integration 统一放入 `docs/stages/evidence/stage-2-authorization/`。

## Stage 2 AC 映射

| Stage AC | 验收要求（Stage 2 brief / 边界文档 §11） | 本 change 覆盖 | 证据 |
| --- | --- | --- | --- |
| S2-AC-03 | OpenFGA 关系授权有允许与拒绝用例 | `authorization` 要求「OpenFGA 关系判定与上下文元组」：成员读/写 allow、非成员 deny、跨租户 404、store 非空启动失败、元组超限 503 | actual：testcontainers 真实 OpenFGA 集成 PASS（viewer dataset read allow、viewer update deny、跨租户 deny）；启动零元组/model 校验由集成用例覆盖；元组上限有 max-chain 单测。§11.3 12 项 handler 集成 actual（真实 datasets handler + 真实 OpenFGA，见 `2026-11-23-authz-handler-integration.txt`） |
| S2-AC-03 | Cedar 属性/动作策略有允许与拒绝用例 | 要求「Cedar 属性与动作策略」：角色×组矩阵、tenancy 范围、资源状态、属性缺失、forbid 优先 | actual：`go test ./internal/platform/authz/` 角色×组 30 用例、资源状态 9 用例、tenancy 范围、属性缺失用例通过 |
| S2-AC-03 | 单一且无冲突的授权判定点（授权 middleware/guard） | 要求「单一授权判定点」「授权守卫与模块接缝」：模块只声明动作/事实并调用守卫；列表父作用域一次；批量 all-or-nothing | actual：guard/Explain httptest 用例；9 个模块接缝 allow/deny/跨租户/状态用例通过；代码评审未发现模块自写角色/管理员判断 |
| S2-AC-03 | 跨租户默认拒绝且不泄露 | 要求「默认拒绝与依赖故障」「租户层级与资源事实」：行租户不一致 404、租户头不作事实、跨租户 GET/PATCH/DELETE 全拒 | actual：datasets/tenancy/ontology/objects/connectors/ingestion/pipelines/actions/query 跨租户 404 且不调用 guard 的用例通过；24 条汇总矩阵 actual（`TestNegativeMatrix24`，29 子用例 PASS，见 `2026-11-23-authz-negative-matrix.txt`） |
| S2-AC-03 | 越权（角色/owner/状态）默认拒绝 | 要求「Cedar 属性与动作策略」「租户层级与资源事实」：viewer 写/删/调用 deny、member admin deny、workspace-admin 组织管理 deny、SavedQuery owner+delete 组、状态机拒绝 | actual：角色×组矩阵、状态策略用例通过；SavedQuery owner+delete 矩阵由角色矩阵/接缝用例覆盖；24 条汇总矩阵 actual（`TestNegativeMatrix24`，29 子用例 PASS，见 `2026-11-23-authz-negative-matrix.txt`） |
| S2-AC-03 | 无策略/未登记动作/缺失事实默认拒绝 | 要求「默认拒绝与依赖故障」「单一授权判定点」「Cedar 属性与动作策略」：unknown_action、no_policy、attribute_missing、空/未知角色、组主体、空策略集启动失败 | actual：unknown_action、disabled/fail-open 配置、缺失属性、策略/schema 交叉校验用例通过；空策略集启动失败矩阵 actual（`TestNegativeMatrix24/空策略集启动失败`） |
| S2-AC-03 | 策略变更审计可复核（及每次判定可追溯） | 要求「审计决策事件」：`authz.decision`/`authz.policy_change`/`authz.explain` 字段、脱敏、append-only、fail-closed、decision id 一致 | actual：审计信封/脱敏/sink 失败、decision id 三处一致、`authz.policy_change` 启动与写失败用例通过 |
| S2-AC-03 | 依赖故障 fail-closed（本 change 的解释性验收） | 要求「默认拒绝与依赖故障」：OpenFGA 超时/不可达、Cedar 错误、审计失败、元组不变量失败均 503 且无副作用 | actual：OpenFGA 超时/审计失败 503 单测、Ready fail-closed 单测通过；Cedar 引擎错误 actual（`TestNegativeMatrix24/Cedar 错误` 503）；readyz 运行期故障由 `Ready` 单测覆盖 |
| 边界文档 §11.4 | 安全评审确认边界后方可实现（S2-AC-03 前置） | 要求「负向测试与实现授权门」；proposal/tasks 第 1 组 | actual：边界文档 §13.3 安全评审 `通过 | 2026-11-22`；负责人 2026-11-22 记录实现授权 |

## Requirement 映射

| Requirement | 场景 | 预期证据 |
| --- | --- | --- |
| 单一授权判定点 | 业务动作进入统一判定；未登记动作拒绝；禁用配置导致启动失败 | actual：guard 单测、`unknown_action`、禁用配置启动失败用例通过；模块接缝无角色分支 |
| OpenFGA 关系判定与上下文元组 | 租户成员读取允许；非成员关系拒绝；store 非空启动失败；元组上限 | actual：testcontainers 集成 PASS、启动 store/model/零元组校验、元组构造/上限单测；store 非空启动失败 actual（`TestNegativeMatrix24/OpenFGA store 有元组启动失败`） |
| Cedar 属性与动作策略 | viewer 读允许写拒绝；workspace-admin 组织管理拒绝；资源状态拒绝；属性缺失 | actual：cedar-go 编译/schema 检查、30 用例矩阵、9 状态用例、`attribute_missing` 用例通过 |
| 默认拒绝与依赖故障 fail-closed | OpenFGA 超时；Cedar 引擎错误；审计写失败；空策略集 | actual：OpenFGA 超时/审计失败 503、错误信封单测；Cedar 错误注入与空策略启动失败 actual（`TestNegativeMatrix24`） |
| 租户层级与资源事实 | 行租户不一致；请求体伪造状态被忽略；组织/工作空间/项目层级事实；SavedQuery 删除 | actual：租户事实/跨租户 404 单测、handler 接缝用例、SavedQuery 角色/owner 用例；真实 Authorizer + datasets handler 集成 actual（`TestDatasetHandlerWithRealOpenFGA`） |
| 审计决策事件 | allow 决策审计先于副作用；deny 决策不产生副作用；脱敏；策略变更审计 | actual：审计字段、sink 失败、脱敏、`authz.policy_change`、decision id 一致性用例通过 |
| 授权守卫与模块接缝 | viewer 写请求；列表只判定一次；批量写全有或全无；不泄露跨租户资源 | actual：9 模块接缝 allow/deny/跨租户/状态用例、列表单次判定用例通过；无 batch_upsert 路由；批量 all-or-nothing 无 batch_upsert 路由；以契约/接缝用例记录 |
| Explain 与诊断 | admin Explain 允许；非 admin Explain 拒绝；Explain 不泄露他人；指标低基数 | actual：Explain allow/403、指标注册/标签白名单单测；`authz.explain` 样例由 Explain 测试覆盖 |
| 负向测试与实现授权门 | 跨租户负向；越权与缺失事实负向；依赖故障负向；实现授权已记录 | partial：安全评审/实现授权 actual；跨租户/越权/缺失事实/依赖故障用例分散通过；24 条独立原始输出与 CI job `authz-integration` actual |

## Wave 0 / G0 门贡献

| 门禁项（change map G0） | 本 change 计划证据 |
| --- | --- |
| 授权模型、策略与拒绝路径通过负向测试 | actual：单测矩阵 + testcontainers 集成基础场景；24 条原始输出 actual（`evidence/2026-11-23-authz-negative-matrix.txt`） |
| `go build` / `go test` 通过 | actual：`go build ./...`、`go test ./...`、`go test -race ./internal/platform/authz/` 通过；CI job `authz-integration` 已加入 `.github/workflows/ci.yml`，本地等价命令已通过；远端 CI 未运行 |
| Stage 1 回归 | actual：`go test ./...` 通过（含既有 authn/httpserver 用例）；未注册路由 404 与认证失败路径未单独建立回归记录 |
| Compose 或 kind 至少一种可启动 | 本 change 不修改 `deploy/`；启动验证由 `stage-2-integration`/`production-delivery` 的 change 提供，本 change 提供 authz 启动校验与 readyz 证据 |

## 未覆盖 / 后续

- 模块级 delta spec 同步：`query-surface`、`pipelines`、`actions`、`connectors`、`ingestion`、`tenancy` 的角色矩阵与资源状态收紧，由对应模块 change 在其 delta spec 中补齐（边界文档 §12.1）；本 change 以 `authorization` 契约先锁定守卫与用例。
- `deploy/compose|helm|argocd` 的 OpenFGA 实例、store 与模型上传流水线归 `production-delivery`；`internal/app`/`cmd` 接线 patch 归 `stage-2-integration`；`go.mod`/`go.sum` 由 integration 统一 tidy。
- 审计保留期（建议 ≥90 天）与轮转由部署方负责，需安全评审/合规确认；审计数据库与查询 API 明确 defer。
- 项目级收窄、markings、service account/group 主体、持久关系元组、outbox、`dataset_permissions` 读写、SavedQuery 共享均 defer（边界文档 §12.5）。
- 生产 OpenFGA 部署形态与容量、JWT 角色变更的撤销延迟已在安全评审中确认（2026-11-22）；生产部署由 `production-delivery` change 落地。
- 真实数据、生产环境与生产 IdP 未涉及；现有证据计划均基于本地/预发实例与合成 fixture，不得写成生产验收。

## 归档检查

- [~] tasks 42/43 完成并勾选，证据可复核；仅 7.5 归档评审待负责人/安全评审确认。
- [x] `traceability.md` 证据列已更新为 actual；24 条负向矩阵、12 项 handler 集成、CI job 已补齐。
- [x] `openspec validate authorization-openfga-cedar --strict` 通过；`--all --strict` 通过。
- [x] 安全评审签核与负责人实现授权记录在案；归档评审记录待实现完成后补。
- [ ] 不自动归档、不自动宣布 Stage 退出。
