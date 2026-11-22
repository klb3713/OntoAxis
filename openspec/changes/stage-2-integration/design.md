# Design

> 状态：planning（2026-11-22 创建）。本文件描述“计划如何做”，命令、路径与场景均为计划；在 `tasks.md` 对应任务完成并把证据写入 `docs/stages/evidence/` 之前，不构成已验证事实。

## Context

- Stage 1 已退出（2026-11-22）：9 个业务模块以 `internal/app/modules.go` 的 `BuildModules` 为唯一装配点接入单进程 `ontoaxis-core`，顺序为 tenancy → connectors → ingestion → datasets → pipelines → ontology → objects → query → actions，`internal/app/modules_test.go` 的 `TestBuildModulesResolvesDependencyOrder` 断言该顺序；`cmd/ontoaxis-core/main.go` 只负责加载配置并以 `app.BuildModules` 调用 `app.New`。
- Stage 2 Wave 0 已归档 `runtime-observability`（`openspec/specs/observability/spec.md`，7 条 requirement）与 `edge-gateway`（`openspec/specs/edge-gateway/spec.md`，5 条 requirement）；Wave 1/2 的 6 个 change（`authorization-openfga-cedar`、`connector-runtime`、`pipeline-kestra-dbt`、`action-temporal`、`object-search-meilisearch`、`production-delivery`）目前均未归档、未实施（起草本 change 时多数仍只有 `openspec new change` 脚手架），其 proposal/design/tasks 由各自 change 推进。
- 当前可复用的集成面：`deploy/compose/compose.yaml` 已有 `default`、`edge`（Traefik v3.3 + `ontoaxis-core-edge`）、`observability`（OTel Collector/Prometheus/Alertmanager/Jaeger/Perses）三类 profile；`config/edge/routes.yaml` 是路由单一来源，`tools/edgegen` 生成 Envoy/Traefik 资源；`scripts/smoke-foundation.sh`、`scripts/edge-smoke.sh`、`scripts/observability-smoke.sh` 已存在；CI 有 build-test / proto / frontend / docker / observability / compose-smoke / edge 七组 job。
- 基线（2026-11-22 起草时实测）：`openspec validate --all --strict` 有 7 个在途 change 因缺少 delta spec 或 `skip_specs` 失败（含本 change）；本 change 设置 `skip_specs: true` 后通过，其余在途 change 的失败由各自 change 在归档前修复。

## Goals / Non-Goals

**Goals**

1. 提供一个（且只有一个）装配点，把 Wave 0–2 的运行时接缝合入 `ontoaxis-core`，保持 9 个模块边界、依赖顺序和既有公开契约不变。
2. 在同一套 Compose 与 kind/k3d 环境里证明跨 change 的全栈 E2E 可运行，并重新证明 Stage 1 全量回归绿色（S2-AC-01）。
3. 产出可复核的 Stage 2 证据索引与退出候选报告，运行 `openspec validate --all --strict` 并记录 CI 结果（S2-AC-11）。
4. 对跨 change 缺陷建立分类、修复、退回归属 change 或阻塞记录的统一流程，禁止绕过鉴权、删除断言或放宽 skip。
5. 明确不自动批准、不自动归档、不自动宣布 Stage 退出。

**Non-Goals**

- 不新增业务模块/页面/第 10 个模块，不改变领域行为、公开 API 或数据库 schema。
- 不重新实现或补齐任何 Wave 0–2 change 的功能与 AC 证据，不重写已归档 change 的 artifacts。
- 不做生产环境部署、真实客户数据、生产密钥或 7×24 运营；不引入多区域 HA。
- 不自动归档、不自动修改 Stage 2 brief 状态、不自动宣布 Stage 2 退出。
- 不扩大 Stage 2 范围；范围外发现记录到报告，移交 Stage 3 决策。

## Decisions

### D1. Wave 3 以“准入 + 基线”开场，不提前并行

只有 `docs/stages/stage-2-change-map.md` §4 记录 G2 为 `pass`、且 8 个前置 change 均处于 archived 状态后才开始合并。当前（2026-11-22）G2 未记录 pass，因此本 change 只能保持 planning/blocked，不得用部分栈的绿色结果冒充集成通过。
替代方案：提前并行联调——被否决，会与共享文件所有权冲突并制造虚假绿色。

### D2. 装配根形态不变：9 个模块 + option/adapter 注入

`internal/app/modules.go` 的 `BuildModules(pool, cfg)` 仍是唯一具体构造点；Temporal、Kestra、Kafka Connect、Meilisearch、OpenFGA/Cedar 客户端由组合根按配置构造，通过既有模块的 option/接口注入（依赖倒置），不新增模块、不改模块内部实现。模块注册顺序、依赖校验与 `/readyz` 的 9 模块 + database 聚合保持不变。
替代方案：新增 `integration`/`runtime` 第 10 个模块——被否决，违反 brief 的 9 模块边界；让各 change 自行注册——被否决，触碰共享文件且顺序不可控。

### D3. 依赖与生成物由本 change 统一收口

各 change 只提交 `go.mod`/`go.sum` 补丁片段；本 change 统一 `go mod tidy`、`make generate`、`make edge-generate`，并在干净工作区验证 `make generate-check`、`make edge-generate-check`、`make proto-lint`、`make proto-breaking` 无差异。生成物与依赖变更单独成提交，便于失败时二分。
替代方案：各 change 直接改 `go.mod`/生成物——被否决，合并冲突与漂移风险高。

### D4. 合并顺序严格按 change map DAG，一次一个 change

顺序：`runtime-observability` → `edge-gateway` → `authorization-openfga-cedar` → `connector-runtime` → `pipeline-kestra-dbt` → `action-temporal` → `object-search-meilisearch` → `production-delivery`。每个 change 合并后立即运行该 change 自己的测试/smoke 与 `go build ./...`、`go test ./...`，记录提交与证据链接；失败时停止后续合并并进入失败处理流程，不带病叠加。
替代方案：全部合并后再统一验证——被否决，失败无法归因到 change。

### D5. E2E 分两层：Compose 快速层 + kind/k3d 交付层

Compose 层覆盖身份→edge→模块→运行时→查询/搜索的完整链路，迭代快、日志完整；kind/k3d 层覆盖 Helm/ArgoCD、probes/PDB/Secret、备份恢复与镜像回滚等交付语义。两层共用同一组场景编号，便于对照。
替代方案：只做 kind——被否决，反馈慢、成本高，无法支撑日常回归；只做 Compose——被否决，不能证明 S2-AC-08/09。

### D6. 证据采用“索引 + 原始目录”，一行一命令一结果

`docs/stages/evidence/stage-2-integration.md` 作为索引，按 Stage AC 组织；`docs/stages/evidence/stage-2-integration/` 存放原始日志、JSON、截图与 CI 记录。每行必须含命令、环境、结果、证据路径；未执行或未通过的一律标 `not passed`/`planned`，不用“应当通过”类表述。
替代方案：把证据散落在各 change 目录——被否决，S2-AC-11 需要单点复核入口。

### D7. 失败处理以“分类 + 停线”为核心

- 依赖缺失（前置未归档/G2 未 pass）：阻塞，不开始。
- 跨 change 缺陷：优先退回归属 change 修复；若该 change 已归档，则本 change 带 owner 签核修复，并记录“何处、为何、owner 同意记录、验证命令”。
- 环境问题（Docker/kind/网络/资源）：与代码缺陷分离，记录重试次数与原始错误，不记为通过。
- 安全回归（鉴权绕过、默认 Principal、`AUTH_DISABLED`、伪造身份头被接受）：停线，禁止用放宽断言/关闭鉴权修复，立即上报负责人。
- 证据不足：记为 not passed，补证据后才可勾选任务。
替代方案：先修后不记录——被否决，无法复核且容易掩盖回归。

### D8. 回滚以“镜像/发布固定 + 数据备份”为准

迁移只向前：schema 回滚 = 备份恢复（`deploy/README.md` 已定义）。应用回滚 = 固定上一个已验证镜像 tag + `docker compose up -d --no-build` / Helm rollback / ArgoCD rollback；配置回滚 = revert 对应提交后重新生成校验。集成提交本身是加法式接线，回滚 = revert + 重跑 build/test/smoke。
替代方案：回滚时手改迁移历史——被否决，会破坏可重复性。

### D9. 不自动 Stage 退出

本 change 最多产出“Stage 2 退出候选”报告与证据；change 归档需评审确认，Stage 2 brief 状态变更与退出声明只能由负责人确认后执行。`tasks.md` 的归档/退出步骤只放在 `## Workflow follow-up` 作为参考，不进入 tracked 进度。

## Assembly Sequence

每一步有明确入口/出口，任一出口不满足即停止并进入 D7 失败处理。

**Step 0 准入**：入口 = 本 proposal/design/tasks/traceability 通过评审；出口 = change map G2 记录 `pass`（日期、命令、结果、确认人）、8 个前置 change 已归档、`openspec list` 仅本 change 在途、基线 `openspec validate --all --strict` 中 specs 全绿。

**Step 1 基线快照**：记录 commit SHA、镜像 tag、Go/Node/容器版本、CI run id、`/readyz` 输出；写入 `docs/stages/evidence/stage-2-integration/baseline/`。

**Step 2 按 DAG 逐 change 合并**：按 D4 顺序，每个 change 合并其登记补丁（依赖、接线、配置键、生成物），运行该 change 证据中的验证命令 + `go build ./...` + `go test ./...`，记录提交与证据链接。

**Step 3 依赖与生成物收口**：`go mod tidy`、`make generate-check`、`make edge-generate-check`、`make proto-lint`、`make proto-breaking`，要求 `git status --porcelain` 无生成物差异。

**Step 4 组合根装配**：`internal/app/` 接线、配置必填校验、启动 fail-fast、`/readyz` 10 项；`internal/app/modules_test.go` 仍断言 9 模块顺序。

**Step 5 Compose 集成**：`docker compose config` → default profile up → `smoke-foundation.sh`；edge profile up → `EDGE_TEST_FAIL_CLOSED=1 scripts/edge-smoke.sh`；observability profile up → `make observability-rules-test` + `scripts/observability-smoke.sh`；Wave 1/2 运行时 profile up（按各 change 文档的健康检查）；`down -v` 后重启一次验证可重复。

**Step 6 kind/k3d 交付层**：Helm install（probes/PDB/Secret）→ ArgoCD Application 同步 → Envoy `oa-edge` 路由验证 → 备份恢复演练 → 镜像回滚演练；命令与日志进证据目录。

**Step 7 全栈 E2E 矩阵**：执行下文 E2E-01…E2E-12；Compose 与 kind 结果分别记录，未跑栈标 `not covered`。

**Step 8 回归与质量门**：完整命令矩阵（见下）全部通过；CI 在真实 runner 上全绿并记录 run URL。本地通过不等于 CI 通过。

**Step 9 证据与报告**：证据索引齐全、报告草稿完成、traceability 无 `planned` 残留（除明确未覆盖项）、`openspec validate --all --strict` 通过。

**Step 10 评审与收口**：提交评审，记录评审意见与处置；不自动归档、不自动改 Stage 状态。

## Module Registration

- 保持 `ModuleFactory` 签名与 `BuildModules(pool, cfg)` 位置；具体构造只出现在组合根，模块包只声明自己需要的接口（repository、外部客户端、验证器）。
- 新运行时接缝（Temporal/Kestra/Kafka Connect/Meilisearch/OpenFGA/Cedar）以 option 或接口注入既有模块；禁止模块之间直接 import 彼此的 repository 实现，模块间只通过 service 接口/Resolver 交互（沿用 datasets/pipelines 的 TenantResolver、DatasetResolver 模式）。
- 9 模块名称、顺序、`Dependencies()` 不变；`modules_test.go` 继续断言数量与顺序，并新增：在测试配置下断言各 adapter 被注入、缺失必填配置时启动失败。
- 失败语义：真实运行时不可用时启动失败并报出组件与配置键；不允许静默退回 Stage 1 no-op adapter 冒充成功。降级必须显式配置且不得计入任何 AC 通过。
- 迁移：平台迁移 → 模块迁移（按依赖顺序）→ 新 adapter 所需模块迁移；集成在空库（`down -v` 后）与既有库各验证一次。
- 中间件顺序：edge 路径只有边缘认证中间件 + 授权中间件；direct 路径保持 JWT 认证 + 租户上下文注入；授权链不因装配改变顺序（authenticate → authorize → handler）。

## Compose / Edge / Observability Integration

- **Compose**：同一 project name 下单栈共存，端口按 `docs/architecture/services-and-ports.md`（18080/19090/18082/18083/18081/15433/18085/19091/19093/16686/18084/14317/14318）；新增 Wave 1/2 服务使用独立 profile 且与被测组件同网络；`depends_on` + healthcheck 保证启动顺序；`docker compose config` 作为静态校验。
- **Edge**：`config/edge/routes.yaml` 仍是唯一路由来源，生成物由 `make edge-generate` 产出；Compose 用 Traefik、kind 用 Envoy Gateway，两者行为以 edge spec 的 5 条 requirement 与 smoke 矩阵为准；direct core 保持 `ONTOAXIS_TRUST_GATEWAY=false`，edge core 为 true；`/metrics` 不经公网入口。
- **Observability**：核心/worker 设置 `ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT` 后导出 OTLP；验证 edge→core→worker 的 traceparent 传播、RED 指标与运行时指标、SLO 面板与告警规则、错误预算；标签不得引入高基数；本地栈只用本地 Alertmanager，不接真实 PagerDuty/Slack 冒充生产告警。
- **kind/k3d**：Helm chart 的 Deployment/Service/ConfigMap/Secret 引用、readiness/liveness probe、PDB、资源限额；ArgoCD Application 同步到集群并验证 `Synced/Healthy`；Secret 由 `kubectl create secret`/外部注入，不提交明文。

## Full-stack E2E Scenarios

场景编号跨栈复用；“关联 AC”列表示该场景主要覆盖的阶段 AC，但 S2-AC-02…10 的证据归属仍以各自 change 为准，本 change 只做组合验证并记录。

| 场景 | 栈 | 关键步骤 | 期望 | 关联 AC |
| --- | --- | --- | --- | --- |
| E2E-01 基础闭环 | Compose | 登录 Keycloak → oa-web 加载 → 工作台读取资源 → 创建/修改对象类型与对象 → `/healthz`、`/readyz` | `/readyz` 10 项 ok；页面无错误；对象可读回 | S2-AC-01 |
| E2E-02 edge 身份链 | Compose | `make edge-generate-check` → edge profile → 无 token/坏 token/有效 token/伪造头/错误方法/未知路由/突发请求/fail-closed | 401/401/200/403/405/404/429/5xx 与 edge spec 一致 | S2-AC-02 |
| E2E-03 授权 | Compose | 关系授权与属性策略的 allow/deny → 跨租户访问 → 无策略请求 → 审计事件 | 允许路径成功、拒绝路径 403/404、跨租户默认拒绝、审计有记录 | S2-AC-03 |
| E2E-04 对象搜索 | Compose | 创建/更新/删除对象 → outbox → 索引消费 → 搜索命中/消失 → 全量重建 | 索引与对象状态一致；重建后可复现 | S2-AC-07 |
| E2E-05 连接器 CDC | Compose | 源库变更 → Debezium → Kafka Connect → ingestion → dataset → 查询 | 变更可见于 dataset 与查询结果；同步状态回写 | S2-AC-06、S2-AC-01 |
| E2E-06 Pipeline | Compose | Kestra 发布/运行 Flow → dbt 产出 → 状态回写 → dataset 可查询；失败重试 | 成功产出可查；失败路径重试且状态如实回写 | S2-AC-05、S2-AC-01 |
| E2E-07 Action | Compose | 触发 Temporal workflow → activity 重试/补偿 → 审批分支 → 状态回写 | 重试与补偿可见；未审批不推进；状态一致 | S2-AC-04、S2-AC-01 |
| E2E-08 跨模块组合链 | Compose | 对象创建 → action → pipeline 产出 → dataset → 查询/搜索 → 审计/指标 | 一条链上各模块状态一致，无孤儿数据 | S2-AC-01 |
| E2E-09 可观测 | Compose | 带 traceparent 请求经 edge → core → worker；Prometheus 查 RED/运行时指标；SLO 面板；告警触发；负载基线复跑 | trace 贯通、指标可见、面板有数据、告警可见、基线结果可复现 | S2-AC-09 |
| E2E-10 k8s 交付 | kind/k3d | Helm install → ArgoCD sync → probes/PDB/Secret → Envoy 路由 → 备份恢复 → 镜像回滚 | `Synced/Healthy`；探针生效；配置/密钥正确挂载；恢复与回滚后可用 | S2-AC-08 |
| E2E-11 生产 IdP | Compose/预发 | 非本地 OIDC issuer/JWKS/claims 登录与 API 调用 | 登录成功、claims 映射租户/角色、无生产密钥入库 | S2-AC-10 |
| E2E-12 安全负路径 | 两栈 | 未认证、错误租户、伪造内部头、越权资源、已注销 token | 401/403/404 符合预期；无默认放行；日志不泄露 token | S2-AC-01、02、03 |

## Evidence Plan

- **索引**：`docs/stages/evidence/stage-2-integration.md`，章节 = 环境与提交 → S2-AC-01 回归矩阵 → S2-AC-02…10 组合复验 → S2-AC-11 门禁与报告 → 证据边界。
- **原始目录**：`docs/stages/evidence/stage-2-integration/`，按执行批次命名（如 `baseline/`、`compose/`、`kind/`、`e2e/`、`ci/`）；每个文件在索引中有对应行。
- **每行必填**：日期、环境（Compose/kind/CI）、commit、镜像 tag、命令、结果（PASS/FAIL/NOT-COVERED）、证据相对路径、执行人；失败/未跑不得省略。
- **边界声明**：合成夹具与本地环境不能证明真实数据验收或生产 SLO；未接入真实告警通道、未做生产压测的项要在“证据边界”明确写出。
- **密钥检查**：证据目录与提交内容不得包含 token、密码、生产连接串；提交前执行仓库现有 secrets/license 检查并在索引中记录结果。
- **归属**：S2-AC-01、S2-AC-11 由本 change 产出主证据；S2-AC-02…10 主证据在各 change，本 change 记录“组合复验”结果与链接。

## Stage 2 Report Structure

计划路径 `docs/stages/stage-2-exit-report.md`，结构对齐 `docs/stages/stage-1-exit-report.md`：

1. 状态与日期（`exit candidate pending owner confirmation`，不是 exited）。
2. Stage 2 范围与波次回顾。
3. Change 清单与归档状态（8 个 change + 本 change）。
4. Stage AC 证据表 S2-AC-01…11：证据、状态（passed / passed-with-caveat / not passed）、链接。
5. 质量门结果：build/test/race/vet/lint/license、proto lint/breaking/generate-check、edge-generate-check、前端 check/test/build、Playwright、Compose/edge/observability smoke、CI run。
6. 全栈 E2E 矩阵：Compose 与 kind 两栈 × E2E-01…12。
7. 证据索引与可复核入口。
8. 偏差、风险与未覆盖项（含合成数据/本地环境边界）。
9. 文档同步清单（ARCHITECTURE 15.x、change map 状态、README、local-development、services-and-ports）。
10. 退出建议与待负责人确认项；注明“不自动退出”。

规则：任何未执行/未通过的项必须如实标 `not passed`；报告不得把“计划”写成“已完成”。

## Rollback / Failure Handling

| 对象 | 回滚动作 | 验证 |
| --- | --- | --- |
| core/web 镜像 | 固定上一个已验证 tag，`docker compose up -d --no-build`；Helm rollback；ArgoCD rollback | `/healthz`、`/readyz`、`smoke-foundation.sh` |
| 组合接线提交 | `git revert` 本 change 的装配提交并重建 | `go build ./...`、`go test ./...`、smoke |
| 数据库 | 停止写入 → `pg_dump` 备份 → 备份恢复（迁移只向前） | `/readyz`、关键数据抽查 |
| edge 生成物 | 用目标 revision 重新 `make edge-generate` | `make edge-generate-check` + edge smoke |
| observability 配置 | revert 配置提交并重建 profile | `make observability-rules-test` + observability smoke |
| k8s 资源 | `kubectl rollout undo` / ArgoCD rollback | probes、PDB、业务 smoke |

失败处理按 D7 分类；任何安全回归立即停线并上报。合并期间出现失败时，禁止“先合完再修”；当前 change 的合并必须回退到上一个绿色点。

## Risks / Trade-offs

- **前置 change 尚未规划，Wave 3 可能长期阻塞**：缓解 = 严格准入、把阻塞写入报告，不用局部绿色冒充通过。
- **跨 change 缺陷在集成阶段才暴露**：缓解 = 逐 change 合并 + 每步测试，缺陷尽早归因；已归档 change 的修复需 owner 签核。
- **共享文件（`cmd/`、`internal/app/`）成为冲突热点**：缓解 = 本 change 独占写权，其他 change 只提交补丁；生成物单独提交。
- **环境不稳定（Docker/kind/资源不足）**：缓解 = 记录重试与环境错误，与代码缺陷分离；必要时固定资源与版本。
- **证据膨胀/重复**：缓解 = 统一索引 + 归属列，避免把各 change 证据再抄一遍。
- **集成阶段变成隐藏的行为变更**：缓解 = `skip_specs: true`、不新增 delta、diff 评审以接线/配置为主。
- **时间压力下跳步**：缓解 = tasks 顺序即合并顺序，勾选必须有证据；`Workflow follow-up` 明确归档/退出需评审与负责人确认。

## Migration Plan

无生产迁移。执行顺序即 Assembly Sequence Step 0–10：准入 → 基线 → 逐 change 合并 → 依赖/生成物 → 组合根 → Compose → kind → E2E → 回归 → 证据/报告 → 评审。每个 Step 失败按 D7 处理并按上表回滚到上一个绿色点。文档同步在 Step 9 完成，历史归档 change 的 artifacts 不改写。

## Open Questions

1. `action-temporal` 是否包含 `web/` 审批 UI？（影响 E2E-07 是否包含 Playwright 步骤；以该 change 的 proposal 为准。）
2. `production-delivery` 以 kind 还是 k3d 作为 S2-AC-08 的主证据栈？（本 change 验证至少一栈并把另一栈标 `not covered` 或补跑，不伪称两栈都过。）
3. Wave 1/2 运行时（Temporal/Kestra/Kafka Connect/Meilisearch/OpenFGA）在同一 Compose 栈中的资源需求是否允许同时启动？（若不允许，按 profile 分组顺序执行 E2E，并在报告说明。）
4. S2-AC-10 的“非本地 OIDC issuer”由哪个预发环境提供、凭证如何注入？（本 change 只验证，不保存任何生产密钥。）
5. 负载基线的工具与阈值是否由 `runtime-observability`/`production-delivery` 定稿？（本 change 只复现其基线，不自造阈值判“合格”。）
