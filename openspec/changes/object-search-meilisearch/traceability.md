# Traceability: object-search-meilisearch

> 规划态 change：Wave 2 门禁 G1 尚无 `pass` 记录。2026-11-23 已实现 unit 切片，证据见 `openspec/changes/object-search-meilisearch/evidence/2026-11-23-object-search-unit.md`。下表仅纯 unit 条目改为 `verified（unit）`；真实 Meilisearch/PostgreSQL 集成、E2E、CI 与授权端到端仍为 planned/partial。禁止把 unit 结果冒充真实实例证据。

## Stage 2 AC 映射

| Stage AC | 验收要求 | 本 change 覆盖 | 证据（planned） |
| --- | --- | --- | --- |
| S2-AC-07 | Meilisearch 对象搜索：outbox 驱动索引更新，对象新增/修改/删除后搜索结果一致；支持全量重建 | 全覆盖（规划）：租户隔离索引、事务性 outbox 事件、异步消费者、增量与全量重建成败、搜索 API、失败重试/死信、可观测性 | `scripts/object-search-e2e.sh` 的 create/search/update/search/delete/search 矩阵 PASS；集成测试（真实 PostgreSQL + Meilisearch）与 `-race` 全绿；两次重建摘要一致输出；消费滞后指标截图/导出；G2 门记录 |
| S2-AC-03 | OpenFGA + Cedar：允许/拒绝、跨租户、默认拒绝、审计 | 部分覆盖、**阻塞**：搜索授权过滤需求（Requirement「搜索授权过滤」）已定义，但 `authorization-openfga-cedar` 未创建，无守卫可接 | 当前无证据；待授权 change 落地后补 403/503/跨租户拒绝用例，在此之前授权证据标记 blocked，不得用替身过关 |
| S2-AC-09 | 可观测：trace/指标/SLO/告警/负载基线 | 部分覆盖：索引事件结果、滞后、重建耗时、搜索 RED 指标与 trace 串接 | 指标端点抓取输出、trace 截图、`make observability-rules-test` 结果（告警规则本体归 `production-delivery`） |

S2-AC-07 拆分核对：

| AC07 要素 | 对应 Requirement | 预期证据 |
| --- | --- | --- |
| outbox 驱动索引更新 | 对象变更事件的可靠捕获；异步索引与最终一致窗口 | 集成测试断言同事务事件、回滚无事件、窗口内可检索 |
| 新增/修改/删除后搜索结果一致 | 异步索引与最终一致窗口；重复与乱序投递的幂等性；增量同步与全量重建收敛 | E2E 三步矩阵；`--verify` 对账退出码 0；重建窗口并发写入后收敛断言 |
| 支持全量重建 | 增量同步与全量重建收敛 | 连续两次重建摘要一致；重建期间搜索可用；失败不产生半成品索引 |
| 搜索可用性与授权边界 | 对象搜索查询契约；分页与结果窗口；过滤与排序；搜索授权过滤；租户隔离的索引与文档 | 参数校验矩阵单测；跨租户空集断言；403/503 用例（阻塞待授权） |
| 运维可诊断 | 搜索可观测性与健康隔离 | 滞后/死信/重建指标与 trace 证据；核心 ready 不受搜索影响断言 |

## Requirement 映射

| # | Requirement | 关键场景 | 预期证据（planned） | 状态 |
| --- | --- | --- | --- | --- |
| R1 | 租户隔离的索引与文档 | 跨租户不可见、租户不可由请求指定、缺少认证租户、索引名不含原始租户标识 | 集成测试两租户隔离断言；401 用例；索引名派生单元测试 | partial（unit） |
| R2 | 对象搜索查询契约 | 关键字命中、缺少检索条件、响应不含属性原文、非法对象类型标识 | handler 单测（400 矩阵、响应字段白名单）；E2E 关键字命中 | verified（unit） |
| R3 | 分页与结果窗口 | 缺省分页、非法分页参数、结果窗口截断标记、深分页上限 | handler 单测；集成测试 `truncated` 与 `offset` 上限 | verified（unit） |
| R4 | 过滤与排序 | 对象类型过滤、时间范围过滤、排序白名单、过滤器不得放宽租户边界 | 集成测试过滤/排序矩阵；跨租户类型过滤空集断言 | partial（unit） |
| R5 | 索引文档 schema 与映射版本 | 映射确定性、映射版本升级、敏感键不进入索引、软删除对象不索引 | 映射单元测试（确定性、敏感键、截断）；索引设置断言；版本升级流程集成测试 | verified（unit） |
| R6 | 对象变更事件的可靠捕获 | 提交产生事件、回滚不产生事件、事件写入失败即整体失败、删除产生删除事件 | 真实 PG 集成测试（提交/回滚/注入失败）；事件字段断言 | partial（unit，DB-gated 已跑） |
| R7 | 异步索引与最终一致窗口 | 新增对象可检索、更新生效、删除消失、写路径不依赖搜索服务 | E2E 三步矩阵与 p95 滞后测量；搜索停机时 CRUD 通过 | planned |
| R8 | 重复与乱序投递的幂等性 | 重复投递、乱序事件、删除后重复投递 | 消费者集成测试：单文档、最终态等于数据库、不复活 | verified（unit） |
| R9 | 增量同步与全量重建收敛 | 重建可复现、重建期间写入、重建失败不影响搜索、增量与重建对账 | `rebuild --verify` 退出码 0；两次重建摘要对比；并发写入集成测试；失败清理断言 | partial（unit） |
| R10 | 搜索授权过滤 | 授权通过、越权拒绝、授权守卫不可用、跨租户过滤被拒绝 | **blocked**：待 `authorization-openfga-cedar` 守卫落地后的契约与集成用例；当前禁止替身证据 | partial（handler guard 单测） |
| R11 | 失败重试、死信与重放 | 暂时失败后追平、超过上限进入死信、死信重放、毒事件隔离 | 注入失败集成测试（attempts/next_attempt_at/dead_at）；`replay` 后收敛断言；未知事件计数 | verified（unit） |
| R12 | 搜索可观测性与健康隔离 | 消费滞后可观测、核心健康不受搜索影响、trace 串联且不泄漏、worker 就绪反映依赖 | 指标抓取导出；trace 断言；搜索停机时 `/healthz`、`/readyz` 200 断言；worker ready 503 断言 | partial（unit） |

## 未覆盖 / 后续

- **授权实现与审计**：搜索授权过滤只接缝，OpenFGA/Cedar 判定、审计与跨租户拒绝的端到端证据归 `authorization-openfga-cedar`（本 change 依赖其落地）。
- **Kafka/Debezium 传输**：本 change 直接消费 `jobs.outbox`；事件契约已按 `<domain>.<entity>.<verb>.v1` 预留，切换到 Debezium→Kafka 属后续 change（依赖 `connector-runtime`）。
- **逐行授权与 markings**：对象 markings 强制过滤、项目/工作空间收窄在授权边界中 defer，本 change 不实现，索引也不引入逐行判定。
- **租户注销索引清理、索引访问审计**：运维项，后续 change 或运维手册承接。
- **outbox 行清理策略**：本 change 不删除 outbox 行（重放与证据需要），清理/归档另开 change。
- **Meilisearch 生产 HA、备份恢复、资源规格、SLO/告警规则**：归 `production-delivery`；本 change 只提供指标契约与 Compose 开发片段。
- **搜索 UI、图/链路搜索、向量检索、同义词/建议**：非本 change 范围。
- **生产容量与压测基线**：归 Stage 2 可观测/交付工作，本 change 只验证功能与滞后。

## 归档检查

- [ ] `tasks.md` 全部勾选，且每项有可复核证据（当前规划态，尚未勾选）
- [ ] 所有 Requirement 的 traceability 状态从 planned 更新为 actual，并链接证据文件
- [ ] R10（搜索授权过滤）证据可用，或由负责人书面接受延后并记录在 G2/风险
- [ ] `openspec validate --all --strict` 通过
- [ ] `openspec status --change object-search-meilisearch` 无异常
- [ ] 主 spec 已并入 `openspec/specs/object-search/spec.md`（归档动作，当前未授权）
- [ ] `stage-2-change-map.md` 的 Wave 2/G2 记录与 `docs/stages/evidence/` 证据索引已更新
- [ ] 共享文件 patch（`internal/app/`、`deploy/compose/`、`deploy/helm/`）已由对应 owner 合并并复核
- [ ] change 评审与负责人确认完成（不自动归档）
