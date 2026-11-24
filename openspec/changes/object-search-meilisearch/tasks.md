# Tasks: object-search-meilisearch

> 状态：G1 已于 2026-11-24 由负责人登记 `pass`；change 评审 2026-11-24 通过（见 [review-wave2-admission.md](review-wave2-admission.md) §6）。当前 **48/52**；未勾 7.5/7.6/8.2/8.4，均依赖 `production-delivery`（Compose/Helm）、`stage-2-integration`（证据收录）与真实 CI。本轮仅隔离本地与合成数据验证。

## 1. 索引契约与事件模型

- [x] 1.1 定义索引名派生、映射版本与索引设置常量（验证：单元测试断言同租户名稳定、不同租户名不同、索引名不含原始 UUID、非法 `index_version` 被拒绝）
- [x] 1.2 实现文档投影 `display_name` 与 `properties_text`（验证：表驱动单元测试覆盖中文、嵌套深度、超长截断、敏感键排除、非标量忽略、键序确定性）
- [x] 1.3 定义对象事件契约（`objects.object.created.v1`、`updated.v1`、`deleted.v1`）与最小载荷序列化（验证：单元测试往返一致、未知字段容错、载荷不含属性值）
- [x] 1.4 worker 自管迁移创建 `search.outbox_attempt` 与 `search.index_state`（验证：临时 PostgreSQL 上 goose up→down→up 通过，`\d search.*` 表结构断言）— 证据（2026-11-23）：真实 PostgreSQL 16（127.0.0.1:55432）集成测试 `TestIntegrationMigrationUpDownUp` 通过（information_schema 列/主键断言、执行 down 后 search schema 表数 0、再次 up 两次一致）；命令与输出见 `evidence/2026-11-23-object-search-integration.md` §1。

## 2. Meilisearch 客户端

- [x] 2.1 实现索引创建/删除/设置应用（验证：真实 Meilisearch 集成测试重复执行设置幂等、无索引时删除不报错）
- [x] 2.2 实现文档批量 upsert/delete（验证：集成测试 500 条分批写入、主键覆盖、删除后不可见）
- [x] 2.3 实现搜索查询（`q`、`filter`、`sort`、`limit`/`offset`、`maxTotalHits` 截断）（验证：集成测试过滤组合、排序方向、窗口截断标记）
- [x] 2.4 实现任务等待与 `swap-indexes`（验证：集成测试等待到 `succeeded`、切换后查询命中新索引、旧索引仍可访问）
- [x] 2.5 实现错误映射、请求超时与 trace/请求 ID 传播（验证：单元测试错误分类；端点停机时返回 `ErrUnavailable` 而非 panic）
- [x] 2.6 实现 `health` 探测（验证：集成测试健康/不健康两种断言）

## 3. 对象写路径与 outbox 生产

- [x] 3.1 实现"对象变更 + `jobs.outbox` 同事务"辅助函数（验证：集成测试提交后事件行存在；故意回滚后无事件行）
- [x] 3.2 接入 Create/Update/Delete 三条写路径并填充事件类型、聚合、版本、操作（验证：集成测试断言事件行数与字段；404/409 路径不产生事件）
- [x] 3.3 保证事件写入失败使业务写入失败（验证：注入 outbox 写错误，断言返回 5xx 且对象行未提交）
- [x] 3.4 事件载荷携带 schema 版本与发生时间（验证：单元测试断言字段存在且格式正确）
- [x] 3.5 回归既有对象 HTTP 契约（验证：`go test ./internal/modules/objects/...` 全绿，既有 handler 测试语义未变）

## 4. 索引消费者

- [x] 4.1 实现批量 claim（`FOR UPDATE SKIP LOCKED`）与同事务标记 `published_at`（验证：集成测试并发两消费者不重复处理；事务中断后事件可被再次领取）— 证据（2026-11-23）：真实 PostgreSQL + Meilisearch 集成测试 `TestIntegrationClaimConcurrent` 通过（B 消费者 15s 内领取 0 条不阻塞、A 回滚后事件可再领、最终文档恰 1 条、outbox 全 published）；命令见 `evidence/2026-11-23-object-search-integration.md` §1。
- [x] 4.2 实现 read-through 应用：读当前对象行 → upsert 或 delete（验证：集成测试覆盖创建、更新、软删、软删后重建事件）
- [x] 4.3 实现重复与乱序投递收敛（验证：集成测试重复事件后单文档；先新后旧事件最终等于数据库当前态）
- [x] 4.4 未知事件类型跳过并计数（验证：集成测试插入未知事件，断言指标计数、该事件被标记且不阻塞后续）
- [x] 4.5 实现失败退避、重试上限与死信（`search.outbox_attempt`）（验证：注入失败断言 `attempts`/`next_attempt_at` 递增与 `dead_at` 置位；死信存在时后续事件仍被处理）
- [x] 4.6 实现 `replay` 子命令重放死信（验证：集成测试重放后 `published_at` 置位、文档收敛、`dead_at` 清空）
- [x] 4.7 记录索引滞后指标（消费时 outbox 年龄）（验证：集成测试构造延迟事件，断言直方图样本落入预期桶）

## 5. 全量重建与对账

- [x] 5.1 实现按租户（可选对象类型）重建（验证：集成测试两个租户各自重建互不影响，索引设置正确）— 证据（2026-11-23）：`TestIntegrationTenantIsolation`（两租户索引名不同、不含原始 UUID、跨租户 filter 空集、settings 正确）与 `TestIntegrationRebuildObjectTypePreservesOtherTypes`（仅单类型重建保留其他类型）通过；**实施中发现并修复** `--object-type` 重建会覆盖整租户索引的缺陷（`rebuild.go` 改为按类型直接 upsert + 删 stale）。
- [x] 5.2 实现 keyset 分批扫描（每批 500）与逐批任务等待（验证：集成测试文档数与数据库行数一致，分页无重复/遗漏）— 证据（2026-11-23）：`TestIntegrationRebuildKeysetPagination` 通过（1100 行、`Summary.Documents=1100`、分页 id 无重复无遗漏、索引计数一致）。
- [x] 5.3 实现暂存索引 → `swap-indexes` 原子切换 → 旧索引保留一版（验证：集成测试切换期间查询连续可用，旧索引仍可查询）— 证据（2026-11-23）：`TestIntegrationRebuildSwapKeepsPreviousIndex` 通过（staging 暂停期间线上索引仍可查 3 条；放行后 4 条且 verified；旧 `_r` 索引保留旧 3 条可查）。
- [x] 5.4 实现重建窗口事件重放（`created_at >= rebuild_started_at`）（验证：重建过程中并发写入，完成后断言文档与数据库一致）— 证据（2026-11-23）：`TestIntegrationRebuildReplaysWindowEvents` 通过（重建扫描期间插入游标之前的事件，完成后 replay 使其进入索引，最终集合与数据库一致）。
- [x] 5.5 实现 `--verify` 摘要对账 — [2026-11-24 真实后端与 CLI 证据](evidence/2026-11-24-object-search-verification.md#1-真实-postgresqlmeilisearch-与-cli)：一致时退出 0；真索引等数量内容漂移时退出 1，输出摘要差异、不含文档内容；另有删除后计数差异用例。
- [x] 5.6 实现失败清理暂存索引（验证：注入中途失败，断言线上索引未变、暂存索引被删除）— 证据（2026-11-23）：`TestIntegrationRebuildFailureCleansStaging` 通过（staging upsert 注入失败后线上索引仍 3 条，新暂存索引 10s 内被清理）。
- [x] 5.7 验证两次重建可复现 — [2026-11-24 真实 CLI 证据](evidence/2026-11-24-object-search-verification.md#1-真实-postgresqlmeilisearch-与-cli)：`TestIntegrationRebuildCLIExitAndDigest` 连续两次运行二进制，均退出 0，文档数、索引名和摘要完全一致；25 文档的包级用例亦通过。HTTP/Compose 整栈单列于 8.1。

## 6. 搜索查询 API

- [x] 6.1 注册 `GET /api/v1/objects/search` 并实现参数解析与校验矩阵（验证：handler 单元测试覆盖 `q` 与 `object_type_id` 均缺失、`limit=0/101`、负 `offset`、非法 `sort`、>10 个类型，全部 400 且为统一错误信封）
- [x] 6.2 组装响应 `items`/`total`/`limit`/`offset`/`truncated`/`request_id`（验证：handler 单元测试断言字段与类型；响应不含 `properties` 原文）
- [x] 6.3 租户只取自 `authn.RequireTenant`，不使用 `X-Tenant-Id` 回退（验证：无认证上下文返回 401；仅带 `X-Tenant-Id` 头返回 401）
- [x] 6.4 查询始终附加 `tenant_id` 过滤（验证：集成测试跨租户查询返回空集，指标/日志不含他租户数据）
- [x] 6.5 接入平台授权守卫：`authorization-openfga-cedar` 已归档；按租户查询过滤类型事实，守卫不可用时拒绝请求。处理器回归及 [真实本地 HTTP 矩阵](evidence/2026-11-24-object-search-verification.md#5-原生进程-http-功能矩阵非-compose) 验证了允许、401、403 和 503；Compose 验证与安全评审仍待完成。
- [x] 6.6 接入 `ONTOAXIS_SEARCH_ENABLED` 开关（验证：关闭时搜索路由返回 404，`objects` 既有路由不受影响）
- [x] 6.7 实现搜索降级（验证：Meilisearch 停机时搜索返回 503 `unavailable`+`retryable`，同进程对象 CRUD 与 `/readyz` 正常）

## 7. Worker 与部署衔接

- [x] 7.1 新增 `cmd/ontoaxis-search` 子命令 `consume`/`rebuild`/`replay`/`migrate`（验证：`go build ./cmd/ontoaxis-search`，`migrate` 重复执行幂等）
- [x] 7.2 实现 worker 健康/就绪/指标端点与优雅关闭（验证：E2E 断言端点；SIGTERM 后当前批次完成、无事件重复处理）
- [x] 7.3 接入 telemetry 与 trace 跨度（验证：E2E 断言 trace 有 `search.consume.*`/`search.rebuild`；span 属性无租户 UUID、属性值、查询原文、凭证）
- [x] 7.4 在 `internal/platform/config` 增加搜索配置项（验证：单元测试缺省值、非法 URL/超时/批次返回错误、`enabled` 时缺 API key 报错）
- [ ] 7.5 产出 `deploy/compose/compose.yaml` 的 `search` profile 片段并交 `production-delivery`（验证：`docker compose -f deploy/compose/compose.yaml --profile search config` 通过；`up -d` 后 meilisearch 与 worker healthy）— 进展（2026-11-23）：片段在 `deploy/compose/search/compose.search.yaml`（已加 `profiles: ["search"]`，core 的 `depends_on.search-worker` 标 `required: false`）；`docker compose -f deploy/compose/compose.yaml -f deploy/compose/search/compose.search.yaml --profile search config -q` exit 0，无 profile 时同命令 exit 0；**主 `compose.yaml` 未改**（需 production-delivery 合并）；`search` profile 已在隔离 E2E 项目内 `up -d` 至 meilisearch 与 worker healthy（2026-11-24，见证据 §4），但主文件合并仍待交付负责人，故不勾。— 2026-11-24 交接包已备：[handoff/production-delivery.md](handoff/production-delivery.md) 列出需合并的服务/卷清单与 `config -q`、`up -d` 验收命令；主文件合并与 `up -d` 仍归 delivery。
- [ ] 7.6 产出 Meilisearch 与 worker 的 Helm/ArgoCD 要求（Deployment/Service/PVC/Secret/Probe/资源）并交 `production-delivery`（验证：`helm template` 渲染通过 + kind 部署评审）— 进展（2026-11-23）：要求文档 `openspec/changes/object-search-meilisearch/deployment-requirements.md` 已完成（StatefulSet/Deployment/PVC/Secret/探针/资源/ArgoCD wave/迁移与重建 Job/回滚，验收表均 `not executed`）；**chart 未创建**，`helm template` 与 kind 评审 **not executed**（归 production-delivery），故不勾。— 2026-11-24 交接包已备：[handoff/production-delivery.md](handoff/production-delivery.md) §2 列出 chart 创建后需执行的 `helm template` 与 kind 评审步骤。
- [x] 7.7 确认 `go.mod`/`go.sum` 无改动（验证：`git diff --exit-code go.mod go.sum`）— 证据（2026-11-23）：`git diff --exit-code -- go.mod go.sum` exit 0；未新增依赖。

## 8. 端到端、证据与质量

- [x] 8.1 编写 `scripts/object-search-e2e.sh`（验证：`search` profile 下通过 create→search→update→search→delete→search、重建、租户隔离、停机降级，输出 `PASS/FAIL` 矩阵）— 证据（2026-11-24 21:07）：隔离项目 `ontoaxis-search-e2e-a57bd15f8187` 复跑 `E2E_START_TIMEOUT=300 bash scripts/object-search-e2e.sh` 退出 0，`object-search e2e: 73 passed, 0 failed`；`search` profile 七容器真实启动，发布端口仅绑 `127.0.0.1`（56804–56807），覆盖 CRUD 后搜索收敛、A/B 租户隔离、两次 `rebuild --verify` 文档数与 digest 一致、OpenFGA 暂停 503/恢复、Meilisearch 停机时搜索 503 而 core `/healthz`/`/readyz` 200 且 worker 就绪 503、重启后自动追平、`/metrics` index-lag 非零；清理无残留。前提是本机 Docker 运行时已修复（首次尝试卡在容器 `start`，经重启 Docker Desktop 后复跑通过，详见 [Compose 整栈与交接](evidence/2026-11-24-object-search-verification.md#4-compose-整栈与交接) §4）。
- [ ] 8.2 集成测试接入真实 PostgreSQL + Meilisearch 且 CI 必跑（验证：CI job 绿；`ONTOAXIS_SEARCH_TEST_*` 未注入时本地 skip、CI 不 skip）— 进展（2026-11-23）：`.github/workflows/ci.yml` 新增 `search-integration` job（postgres:16 + getmeili/meilisearch:v1.11 service containers，`ONTOAXIS_SEARCH_TEST_REQUIRED=1`，`go test -tags=integration -p 1 -count=1`）；本地同命令在真实 PG16+Meilisearch 上 PASS；**CI run 未执行**（无 GitHub runner），无 run 链接，故不勾。— 2026-11-24 CI 同参本地实跑：`ONTOAXIS_SEARCH_TEST_REQUIRED=1` + 真实 PG16/Meili 1.11，`go test -tags=integration -p 1 -count=1` 退出 0、`--- SKIP` 为 0、60 个通过项（12 个顶层集成用例）；日志 [evidence/2026-11-24-search-integration-ci-local.log](evidence/2026-11-24-search-integration-ci-local.log)。仍缺 GitHub Actions run 链接。
- [x] 8.3 建立中文检索评测集与基线断言（验证：`fixtures/search/` 合成数据评测通过；阈值需负责人签核后才可以此判合格）— 证据（2026-11-23）：`fixtures/search/dataset.json`（24 条中文合成文档、7 类对象类型、6 条查询）与 `fixtures/search/README.md`；`scripts/search-quality-baseline.sh` 在真实 Meilisearch 1.11.3 实跑 exit 0，6/6 查询 recall@5=1.00、MRR@10=1.00。阈值 `status=pending-owner-signoff`（recall_at_5=0.9、mrr_at_10=0.8），**未签核，不判定业务合格**；`--strict` 在未签核时不应作为合格门禁。— 2026-11-24 负责人签核**工程回归门** `engineering_gate`（冻结语料 sha256、基线 1.00/1.00），`--strict` 改为强制工程门并接入 CI `search-integration`；**业务阈值仍 pending-owner-signoff**。
- [ ] 8.4 采集证据并交 `stage-2-integration` 收录（验证：`docs/stages/evidence/` 中证据文件含命令、关键输出摘要、CI/运行记录链接）— [2026-11-24 隔离验证](evidence/2026-11-24-object-search-verification.md) 已记录真实后端与 CLI 验证（后端集成 12 项、原生 HTTP 68/68、隔离 Compose E2E 73/73，环境恢复后复跑）及交接项；仍缺 CI run 链接，共享 Stage 证据须由 `stage-2-integration` 收录，故不勾。— 2026-11-24 证据源文件已含集成/复现日志，并备收录清单：[handoff/stage-2-integration.md](handoff/stage-2-integration.md)（列出可复制到 `docs/stages/evidence/` 的文件与待回填的 CI 链接、评审记录）。
- [x] 8.5 静态与竞态检查（验证：`go build ./...`、`go vet ./...`、`go test ./...`、`go test -race ./...` 全部通过）

## 9. 文档与评审

- [x] 9.1 编写运维手册：重建、重放、降级、索引清理、映射升级（验证：评审通过，命令可直接复现）— 进展（2026-11-23）：`docs/operations/object-search-runbook.md` 已完成（含配置、迁移、整租户与单类型重建、`--verify` 差异、死信/重放、降级恢复、索引清理、v1→v2 映射升级、告警表、本地命令）。— 2026-11-24 命令本地复现完成：`migrate` 两次均 exit 0（幂等，建 `search.index_state`/`search.outbox_attempt`）、`replay --limit 5` exit 0（events=0）、`rebuild --verify` 由 `TestIntegrationRebuildCLIExitAndDigest` 真实 CLI 两次 exit 0 且 digest 一致、漂移非零退出；并在手册 §3 增补“裸进程需先有 `object_data` 业务 schema”前置条件。记录见 [evidence/2026-11-24-object-search-runbook-repro.md](evidence/2026-11-24-object-search-runbook-repro.md)。**评审通过（2026-11-24，评审人 klb3713），见 [review-ops-runbook.md](review-ops-runbook.md) §5。**
- [x] 9.2 安全与数据治理评审：索引字段与敏感属性边界（验证：安全评审记录；如有更严要求则登记后续 change）— 材料（2026-11-23）：`security-data-governance-review.md`（S1–S15 事实清单、7 个待评审决策）。— 2026-11-24 补充 §5 风险登记 R-1..R-4（单类型非原子、语料规模、敏感字段、CI 门）与阈值分级决策项；**评审通过（2026-11-24，评审人 klb3713），§4 结论表与 §6 处置记录已填，S12/S13/S10/S11/S15 细化登记后续 change；业务质量门保持 `pending-owner-signoff`。**
- [x] 9.3 change 评审与 Wave 2 准入核对（验证：评审记录；G1 `pass` 记录存在后才进入实施）— 2026-11-24 [G1 已由负责人登记](../../../docs/stages/evidence/stage-2-g1-gate-record.md)；会话已确认仅用隔离本地环境和合成数据补验证。**change 评审已签（2026-11-24，评审人 klb3713，见 [review-wave2-admission.md](review-wave2-admission.md) §6），G1 pass 已登记。** 材料包：[review-packet.md](review-packet.md)。G2（kind/备份/回滚/生产 IdP）仍未执行。
- [x] 9.4 更新 `traceability.md` 的实际证据映射（验证：每条 requirement 的 evidence 与证据文件一致，planned 不写成 actual）— 证据（2026-11-23）：`traceability.md` 已按真实结果更新 R1/R4/R5/R6/R7/R9 与 S2-AC-07 证据，新增“证据索引”与 not executed 清单；E2E/CI/评审仍标 pending，未写成 actual。

## Workflow follow-up

- G1 已登记；本轮实施授权限隔离本地环境和合成数据，不等于 change 评审或 G2。
- 与 `stage-2-integration` 协调 `internal/app/` 装配 patch 的合并窗口，与 `production-delivery` 协调 Compose/Helm 片段合并
- `authorization-openfga-cedar` 已归档，搜索守卫已接入；跨租户、越权及守卫不可用已获真实本地 HTTP 证据，不能替代 Compose 或外部 IdP 验收。
- 若评审认定"对象写路径写 outbox"属于 `objects` 契约变更，则补 `### Modified Capabilities` 与 `specs/objects/spec.md` delta
- outbox 行清理策略、租户注销索引清理、生产 HA/备份另行开 change 或纳入 `production-delivery`
- 归档前执行 `openspec validate --all --strict`、`openspec status --change object-search-meilisearch`，核对 tasks 勾选均有证据
