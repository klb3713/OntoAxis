# Traceability: object-search-meilisearch

> G1 已由负责人登记；本轮仅实施隔离本地与合成数据验证。见 [2026-11-24 验证记录](evidence/2026-11-24-object-search-verification.md)。后端集成及真实 CLI 结果、HTTP/认证整栈、CI 与 Stage 门禁分别记账，不得互相替代。

## Stage 2 AC 映射

| Stage AC | 验收要求 | 本 change 覆盖 | 证据 |
| --- | --- | --- | --- |
| S2-AC-07 | Meilisearch 对象搜索：outbox 驱动索引更新，对象新增/修改/删除后搜索结果一致；支持全量重建 | 本地后端覆盖 outbox 消费、增量与整租户重建；部署和 Stage 验收独立 | [2026-11-24 验证记录](evidence/2026-11-24-object-search-verification.md)：真实 PostgreSQL 与 Meilisearch 12 项集成通过；实际 CLI `--verify` 一致时退出 0、同数量内容漂移时退出 1。Compose 在依赖启动阶段超时（首次），经修复本机 Docker 运行时后于 2026-11-24 21:07 隔离 Compose E2E 复跑 73/73 通过；真实 CI run [45405474349](https://github.com/klb3713/OntoAxis/actions/runs/45405474349) 的 `search-integration` job 绿（8.2）；G2 验收（kind/备份回滚/生产 IdP）仍无结果 |
| S2-AC-03 | OpenFGA + Cedar：允许/拒绝、跨租户、默认拒绝、审计 | 部分覆盖：授权 change 已归档，搜索使用实际守卫，并按租户加载过滤类型事实 | 处理器回归与 [真实本地 HTTP](evidence/2026-11-24-object-search-verification.md#5-原生进程-http-功能矩阵非-compose) 验证允许、401/403/503、无结果泄漏及恢复；未验证外部 IdP |
| S2-AC-09 | 可观测：trace/指标/SLO/告警/负载基线 | 部分覆盖：索引事件结果、滞后、重建耗时、搜索 RED 指标与 trace 串接 | 指标端点抓取输出、trace 截图、`make observability-rules-test` 结果（告警规则本体归 `production-delivery`） |

S2-AC-07 拆分核对：

| AC07 要素 | 对应 Requirement | 预期证据 |
| --- | --- | --- |
| outbox 驱动索引更新 | 对象变更事件的可靠捕获；异步索引与最终一致窗口 | 集成测试断言同事务事件、回滚无事件、窗口内可检索 |
| 新增/修改/删除后搜索结果一致 | 异步索引与最终一致窗口；重复与乱序投递的幂等性；增量同步与全量重建收敛 | E2E 三步矩阵；`--verify` 对账退出码 0；重建窗口并发写入后收敛断言 |
| 支持全量重建 | 增量同步与全量重建收敛 | 连续两次重建摘要一致；重建期间搜索可用；失败不产生半成品索引 |
| 搜索可用性与授权边界 | 对象搜索查询契约；分页与结果窗口；过滤与排序；搜索授权过滤；租户隔离的索引与文档 | 参数校验、跨租户关键词不泄漏、类型范围 403、依赖故障 503；分别记录处理器与整栈证据 |
| 运维可诊断 | 搜索可观测性与健康隔离 | 滞后/死信/重建指标与 trace 证据；核心 ready 不受搜索影响断言 |

## Requirement 映射

| # | Requirement | 关键场景 | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| R1 | 租户隔离的索引与文档 | 跨租户不可见、租户不可由请求指定、缺少认证租户、索引名不含原始租户标识 | 集成：`TestIntegrationTenantIsolation`（索引名不同/不含 UUID、跨租户 filter 空集、settings）；单测：401 与索引名派生 | verified（integration） |
| R2 | 对象搜索查询契约 | 关键字命中、缺少检索条件、响应不含属性原文、非法对象类型标识 | handler 单测（400 矩阵、响应字段白名单）；原生 HTTP 与隔离 Compose E2E 均验证关键字命中 | verified（unit） |
| R3 | 分页与结果窗口 | 缺省分页、非法分页参数、结果窗口截断标记、深分页上限 | handler 单测；集成 `truncated`/`offset` 断言未单独覆盖 | verified（unit） |
| R4 | 过滤与排序 | 对象类型过滤、时间范围过滤、排序白名单、过滤器不得放宽租户边界 | 集成：`TestIntegrationTenantIsolation` 覆盖 tenant filter 边界、`TestIntegrationRebuildObjectTypePreservesOtherTypes` 覆盖 type filter；时间/排序矩阵仍缺 | partial（integration） |
| R5 | 索引文档 schema 与映射版本 | 映射确定性、映射版本升级、敏感键不进入索引、软删除对象不索引 | 映射单测（确定性/敏感键/截断）；集成 `TestIntegrationTenantIsolation` 断言 settings；版本升级流程仍缺 | verified（integration） |
| R6 | 对象变更事件的可靠捕获 | 提交产生事件、回滚不产生事件、事件写入失败即整体失败、删除产生删除事件 | objects repo DB-gated 测试已跑（`ONTOAXIS_TEST_DATABASE_URL`）；消费者集成测试真实读取 outbox；三写路径回滚/注入失败未在本次集成中重跑 | partial（unit，DB-gated 已跑） |
| R7 | 异步索引与最终一致窗口 | 新增对象可检索、更新生效、删除消失、写路径不依赖搜索服务 | 集成：`TestIntegrationConsumerAppliesAndPublishes` 真实 Meili 验证 create/update/delete 收敛；原生 HTTP 与隔离 Compose E2E 的 CRUD/停机/追平均通过，p95 SLO 与主 Compose 交付仍待 | partial（本地功能已验证） |
| R8 | 重复与乱序投递的幂等性 | 重复投递、乱序事件、删除后重复投递 | 消费者单测；真实集成未单列重复/乱序用例 | verified（unit） |
| R9 | 增量同步与全量重建收敛 | 重建可复现、重建期间写入、暂存失败隔离、增量与重建对账 | [真实后端与 CLI](evidence/2026-11-24-object-search-verification.md#1-真实-postgresqlmeilisearch-与-cli)：整租户交换/窗口重放、稳定摘要和等数量漂移拒绝均通过；单类型保留其他类型，但原地修复不证明原子切换 | verified（整租户集成/CLI）；单类型语义待评审 |
| R10 | 搜索授权过滤 | 授权通过、越权拒绝、授权守卫不可用、跨租户过滤被拒绝 | `TestSearchObjectTypeAuthorizationUsesTenantFacts` 覆盖本租户允许/拒绝、跨租户/混合类型 403、依赖故障 503 及不调用索引；真实本地 Keycloak/OpenFGA/Cedar HTTP 通过，拒绝与守卫故障不暴露 `items` / `total` | verified（本地功能）；Compose/外部 IdP 待验 |
| R11 | 失败重试、死信与重放 | 暂时失败后追平、超过上限进入死信、死信重放、毒事件隔离 | 注入失败集成测试（attempts/next_attempt_at/dead_at）；`replay` 后收敛断言；未知事件计数 | verified（unit） |
| R12 | 搜索可观测性与健康隔离 | 消费滞后可观测、核心健康不受搜索影响、trace 串联且不泄漏、worker 就绪反映依赖 | 原生 HTTP 实录：消费滞后直方图、core health/ready 200、worker 停机 ready 503、恢复 200；完整 tracing 与 SLO 待验 | partial（本地功能） |

## 未覆盖 / 后续

- **搜索授权部署证据**：允许、拒绝、跨租户和依赖故障均已用真实本地 Keycloak/OpenFGA/Cedar 验证，`search` profile 已在隔离 Compose E2E 通过；仍需主 Compose 交付与 Stage 的外部 IdP 证据。
- **Kafka/Debezium 传输**：本 change 直接消费 `jobs.outbox`；事件契约已按 `<domain>.<entity>.<verb>.v1` 预留，切换到 Debezium→Kafka 属后续 change（依赖 `connector-runtime`）。
- **逐行授权与 markings**：对象 markings 强制过滤、项目/工作空间收窄在授权边界中 defer，本 change 不实现，索引也不引入逐行判定。
- **租户注销索引清理、索引访问审计**：运维项，后续 change 或运维手册承接。
- **outbox 行清理策略**：本 change 不删除 outbox 行（重放与证据需要），清理/归档另开 change。
- **Meilisearch 生产 HA、备份恢复、资源规格、SLO/告警规则**：归 `production-delivery`；本 change 只提供指标契约与 Compose 开发片段。
- **搜索 UI、图/链路搜索、向量检索、同义词/建议**：非本 change 范围。
- **生产容量与压测基线**：归 Stage 2 可观测/交付工作，本 change 只验证功能与滞后。

## 证据索引

| 证据文件 | 覆盖 | 状态 |
| --- | --- | --- |
| `evidence/2026-11-23-object-search-unit.md` | unit 切片（实现期） | 已存在 |
| `evidence/2026-11-23-object-search-integration.md` | 1.4/4.1/5.1–5.7 真实 PG16+Meilisearch 集成、缺陷修复、go.mod 未改、compose config、评测基线；含 not executed 清单 | 已采集（2026-11-23） |
| [2026-11-24 隔离验证](evidence/2026-11-24-object-search-verification.md) | 5.5/5.7 真实 CLI、后端集成（12 项）、原生 HTTP 68 项、中文合成基线和隔离 Compose E2E 73 项 | 已采集 |
| [`scripts/object-search-e2e.sh`](../../../scripts/object-search-e2e.sh) | 8.1 E2E 矩阵 | 预检与离线回归通过；隔离 Compose E2E 73/73（2026-11-24 21:07）|
| `.github/workflows/ci.yml` `search-integration` | 8.2 CI 必跑 | workflow 已配置；CI 同参本地实跑 exit 0、0 skip（[日志](evidence/2026-11-24-search-integration-ci-local.log)）；**真实 CI run 已执行**：[run 45405474349](https://github.com/klb3713/OntoAxis/actions/runs/45405474349) 的 [`search-integration` job](https://github.com/klb3713/OntoAxis/actions/runs/45405474349/job/138484907178) success（集成测试 PASS + 检索回归门 PASS，0 SKIP） |
| `fixtures/search/dataset.json`、`fixtures/search/README.md`、`scripts/search-quality-baseline.sh` | 8.3 中文评测 | 已实跑 exit 0；工程回归门 `engineering_gate` 已签并 `--strict` PASS（接 CI）；业务阈值 `pending-owner-signoff` |
| `deployment-requirements.md` | 7.6 Helm/ArgoCD 要求 | 文档完成；`helm template`/kind **not executed** |
| `docs/operations/object-search-runbook.md` | 9.1 运维手册 | 文档完成；命令本地复现（[记录](evidence/2026-11-24-object-search-runbook-repro.md)）；**评审通过（klb3713，2026-11-24）** |
| [交付包 production-delivery](handoff/production-delivery.md)、[交付包 stage-2-integration](handoff/stage-2-integration.md) | 7.5/7.6/8.4 交接 | 交接包已备；合并/收录归对应 owner |
| [安全与数据治理](security-data-governance-review.md)、[Wave 2 准入核对](review-wave2-admission.md) | 9.2/9.3 | **评审通过（klb3713，2026-11-24）**，见各 §4/§6；G1 已登记，G2 未执行 |

## 归档检查

- [ ] `tasks.md` 全部勾选，且每项有可复核证据（当前 52/52 全勾；7.5/7.6 产物已交付、其合并与 kind 评审在 `production-delivery` tasks §11 跟踪；8.4 已由 `stage-2-integration` 收录，见 `docs/stages/evidence/stage-2-integration.md` §2）
- [ ] 所有 Requirement 的 traceability 状态从 planned 更新为 actual，并链接证据文件
- [ ] R10（搜索授权过滤）证据可用，或由负责人书面接受延后并记录在 G2/风险
- [ ] `openspec validate --all --strict` 通过
- [ ] `openspec status --change object-search-meilisearch` 无异常
- [ ] 主 spec 已并入 `openspec/specs/object-search/spec.md`（归档动作，当前未授权）
- [ ] `stage-2-change-map.md` 的 Wave 2/G2 记录与 `docs/stages/evidence/` 证据索引已更新
- [ ] 共享文件 patch（`internal/app/`、`deploy/compose/`、`deploy/helm/`）已由对应 owner 合并并复核
- [ ] change 评审与负责人确认完成（不自动归档）
