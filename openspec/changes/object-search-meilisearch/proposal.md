# Proposal: object-search-meilisearch

## Why

Stage 1 的对象实例只存在于 PostgreSQL JSONB 中：`/api/v1/objects` 只能按 `object_type_id` 与分页做数据库过滤，没有全文检索、没有相关性排序，也没有对象新增/修改/删除后的派生读模型刷新机制。ARCHITECTURE §5.3/§14.4 要求对象搜索由 Meilisearch 承载并通过 outbox 异步刷新，Stage 2 brief 要求「outbox 驱动索引更新、写后搜索结果一致、支持全量重建」；而当前仓库的 outbox 只有 `jobs.outbox` 表定义，没有任何 Go 生产者或消费者，搜索运行时也完全缺失。因此需要一次成契约地建立对象变更事件、索引消费者、搜索查询面与全量重建。

## What Changes

- 新增 `object-search` 能力（New Capability）：索引文档 schema 与映射版本、租户隔离、outbox 驱动的异步索引、增量更新与全量重建、搜索查询/分页/过滤契约、授权过滤、失败重试与死信、可观测性。
- 对象写路径新增事务性 outbox 事件：创建/更新/软删除与 `jobs.outbox` 记录同一事务提交；事件只携带对象标识、对象类型、版本与操作类型，不携带完整属性快照。
- 新增 `internal/platform/search/`：轻量 Meilisearch HTTP 客户端（`net/http`，不新增 Go 依赖）、按租户派生的索引与设置、文档映射、outbox 消费者、全量重建与摘要对账。
- 新增独立索引 worker：消费 outbox、按租户写入索引、自带迁移/健康检查/指标，与 core 进程解耦，不阻塞对象写请求。
- 新增搜索查询 API：认证租户内的对象检索，返回 `items`/`total`/`limit`/`offset`/`truncated` 与统一错误信封；查询前调用平台授权守卫（与对象列表相同的 read 作用域判定）。
- 新增全量重建/回填命令：按租户（可选对象类型）从 PostgreSQL 快照构建暂存索引、原子切换、切换后重放窗口内事件保证收敛，并支持 `--verify` 摘要对账。
- 新增 `search` Compose profile 与 E2E 脚本规格：create/update/delete → 搜索命中/更新/消失，租户隔离，重建与增量收敛一致，故障注入下的降级与追平。
- **不修改** `objects` 能力的既有 HTTP 行为：对象 CRUD 的请求/响应、错误信封、租户边界保持不变。
- **不引入** Kafka/Debezium：本 change 直接消费 `jobs.outbox`；CDC 路径属于 `connector-runtime`，后续替换传输层不影响搜索契约。

## Capabilities

### New Capabilities

- `object-search`: 对象搜索索引与查询行为，包括索引映射与版本、租户隔离、outbox 驱动的最终一致索引、幂等与增量同步、全量重建、查询/分页/过滤、授权过滤、失败重试与死信、可观测性。

### Modified Capabilities

无。对象 CRUD 的可观察 HTTP 契约不变；对象写路径新增的 outbox 事件随本能力定义为索引管道的输入契约。若评审认为「对象写入会产生事件」应属于 `objects` 契约，可在 delta 中补一条 MODIFIED requirement，而不改变本提案的能力边界。

## Impact

- 本 change 负责目录：`internal/platform/search/`（客户端、映射、索引、消费者、重建、迁移、指标、测试）、`internal/modules/objects/`（outbox 生产、搜索路由与授权接缝）、新增 worker 入口、新增 E2E 脚本、`openspec/changes/object-search-meilisearch/`。
- 修改 `internal/platform/config/`（新增 `ONTOAXIS_SEARCH_*` 字段与校验；不改既有字段语义）。
- 不改 `objects` 表结构与既有迁移；新增仅属于搜索的 schema 表，由 worker 启动迁移。
- 共享文件交接（不直接改，由 owner 合并）：`internal/app/` 模块装配（owner `stage-2-integration`）、`deploy/compose/` 的 `search` profile 片段与 Helm 部署要求（owner `production-delivery`）、`docs/stages/evidence/` 证据收录（owner `stage-2-integration`）。
- 依赖 `runtime-observability`（已归档）：复用 telemetry 约定、HTTP RED 指标与 DB tracing，不修改其平台包。
- 依赖授权守卫接缝：`authorization-openfga-cedar` 已归档；对象搜索必须调用同一平台守卫，不自行实现角色/租户放行。租户内对象类型事实用于构造守卫输入，不替代授权判定。
- 无新增 Go 依赖，`go.mod`/`go.sum` 不变；无 `web/` 改动；新增本地端口 18086（Meilisearch）与 worker 健康/指标端口。
- 门禁：本 change 属 Stage 2 Wave 2；[G1 已登记通过](../../../docs/stages/evidence/stage-2-g1-gate-record.md)。当前仅获准在隔离本地环境使用合成数据补齐验证，change 评审和 G2 尚未通过。
