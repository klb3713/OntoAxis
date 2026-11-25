# Design: object-search-meilisearch

## Context

- 对象实例当前只存 PostgreSQL JSONB（`object_data.objects`，含 `tenant_id`、`object_type_id`、`properties`、`version`、`deleted_at`），列表接口只做类型过滤与分页；仓库没有搜索运行时，也没有任何 outbox 生产者或消费者（`jobs.outbox` 仅有表定义）。
- ARCHITECTURE §5.3 指定 Meilisearch 承载对象搜索；§14.4 要求业务表与 `jobs.outbox` 同事务、消费端按事件幂等、至少一次、重试上限与退避、死信保留 ≥14 天、跨模块副作用不在请求线程同步调用外部系统。
- `runtime-observability` 已归档：`observability` 包提供 telemetry 初始化、HTTP RED 指标、DB tracing；`httpserver` 已内建 request ID、metrics、trace 中间件。
- `authorization-openfga-cedar` 已归档。业务模块必须调用平台授权守卫，不自写角色/同租户放行判断，不得 fail-open。本 change 只接入守卫并提供租户内对象类型事实，不实现授权引擎。
- `module.Module` 接口只有 `Name/Dependencies/Migrate/RegisterRoutes/Ready`，没有后台任务生命周期钩子；`internal/app/` 与 `cmd/` 由 `stage-2-integration` 拥有，`deploy/compose/`、`deploy/helm/` 由 `production-delivery` 拥有。
- [G1 已登记通过](../../../../docs/stages/evidence/stage-2-g1-gate-record.md)。当前实施范围为隔离本地环境与合成数据验证；不包含外部 IdP、交付验收或 G2 签核。

## Goals

- 建立可验证的租户隔离索引形态、文档映射与映射版本策略。
- 建立事务性 outbox 事件、幂等消费者、增量同步与全量重建的完整链路。
- 定义搜索查询 API 的契约（分页、过滤、排序、错误语义），并留下唯一的授权接缝。
- 让索引故障与搜索故障不进入核心读写路径，同时提供足够的指标与 trace 支撑 G2 证据。
- 明确跨 owner 文件交接与回滚路径，使 Wave 2 可并行推进。

## Non-Goals

- 不实现 Kafka/Debezium 传输（后续可替换消费者传输层，事件契约不变）。
- 不实现逐行授权、对象 markings 强制、项目/工作空间收窄（授权边界已 defer，不得假执行）。
- 不做链接/图搜索、欧氏/向量检索、跨租户平台级搜索、搜索建议与同义词管理。
- 不做前端搜索页（`web/` 默认不改）。
- 不直接改 `deploy/compose/`、`deploy/helm/`、`internal/app/`、`docs/stages/evidence/`，只提供交接 patch/片段。
- 不定义 outbox 行清理策略与生产容量/SLO；这些属于后续 change 或 `production-delivery`。

## Decisions

### D1 索引形态：按租户派生的独立索引 + 文档内租户过滤（defense in depth）

- 索引名：`objects_<indexVersion>_<hex16(sha256(tenantUUID))>`，例如 `objects_v1_3f2a…`。映射版本进入索引名，升级映射即新索引。
- 每个租户独立索引；索引设置（searchable/filterable/sortable/displayed、`primaryKey`、`pagination.maxTotalHits`）由消费者/重建幂等初始化。
- 每次查询同时带 `tenant_id = <认证租户>` 过滤条件，即使索引已按租户隔离。这样单点过滤漏写不会直接跨租户泄漏，只会返回空集。
- 备选：单一共享索引 + `tenant_id` filter（实现最简，但任何过滤遗漏即跨租户泄漏，且设置变更/重建影响所有租户）；每租户独立 Meilisearch 实例（隔离最强，但资源与运维成本不可接受）。选择 per-tenant index 换取"索引边界即租户边界"。
- 代价：索引数量随租户增长，租户注销/清理需要运维动作（列入后续运维手册，本 change 不实现）。

### D2 客户端：`net/http` 轻量封装，不引入 Go 依赖

- `internal/platform/search/client.go` 只实现必需面：创建/删除索引、更新设置、批量写入/删除文档、搜索、索引任务等待、`swap-indexes`、`health`。
- 统一超时（请求级 context）、错误类型（`ErrUnavailable`、`ErrInvalid`、`ErrNotFound`）、请求 ID 与 trace 传播；不做自动重试（重试语义由消费者/调用方决定）。
- 备选：官方 `meilisearch-go` SDK（能力全，但新增依赖、版本耦合、CI 许可证与 tidy 归属跨 owner；`go.mod` 由集成 change 统一 tidy）。选择薄客户端，把可控性留在本模块。

### D3 传输：直接消费 `jobs.outbox`，不上 Kafka

- 消费者用 `SELECT … FROM jobs.outbox WHERE published_at IS NULL … ORDER BY created_at, id LIMIT n FOR UPDATE SKIP LOCKED` 在单事务内 claim 一批事件；逐条处理后在同一事务内把成功事件标记 `published_at=now()`，失败事件写入 `search.outbox_attempt` 后提交。
- 备选：Debezium→Kafka→消费者（ARCHITECTURE 目标路径，但依赖未完成的 `connector-runtime`，且增加运行面与联调成本）；`LISTEN/NOTIFY`（可能丢事件，不满足至少一次）。
- 演进：事件命名已按 `<domain>.<entity>.<verb>.v1`（`objects.object.created.v1` 等）预留，后续切换传输层不改事件契约与消费者幂等逻辑。

### D4 事件最小化 + read-through 应用

- 事件载荷只含 `object_id`、`object_type_id`、`version`、`operation`、`occurred_at`；租户与聚合 ID 用 `jobs.outbox` 既有列。
- 消费者按 `tenant_id + object_id` 读取对象当前行（`deleted_at IS NULL`）构建文档；行不存在或已软删则删除索引文档。事件只是"该对象可能变了"的提示。
- 这样重复、乱序、甚至陈旧事件都收敛到数据库当前状态，天然幂等，无需逐对象版本比对表。
- 备选：事件携带完整属性快照（事件大、schema 耦合对象属性、乱序时会写回旧值）。

### D5 文档映射与投影

| 字段 | 类型 | 用途 |
|---|---|---|
| `id` | string(UUID)，primaryKey | 文档主键 |
| `tenant_id` | string(UUID) | filterable；defense in depth，不 displayed |
| `object_type_id` | string(UUID) | filterable |
| `display_name` | string | searchable；展示名 |
| `properties_text` | string | searchable；属性文本投影 |
| `version` | number | 版本，便于排查 |
| `created_at` / `updated_at` | RFC3339 string | sortable |

- `display_name`：属性键按 `name`→`title`→`label`→`display_name` 顺序取第一个非空字符串，缺失时回退对象 ID。
- `properties_text`：对 `properties` 做确定性扁平化，仅取标量（string/number/bool），深度 ≤2，键按字典序拼接，总长截断到 4096 字符；排除键名匹配 `password|passwd|secret|token|api_key|credential|private_key`（大小写不敏感）的子树。
- 不索引 `owner_id`、`markings`、原始 `properties` 结构；不做逐行 markings 过滤（授权边界同步 defer）。
- 索引设置：`searchableAttributes: ["display_name","properties_text"]`、`filterableAttributes: ["tenant_id","object_type_id","updated_at"]`、`sortableAttributes: ["updated_at","created_at"]`、`displayedAttributes: ["id","object_type_id","display_name","version","updated_at"]`、`pagination.maxTotalHits: 10000`。
- 映射版本升级：新版本索引名 + 全量重建 + 切换；旧索引保留一版用于回退。

### D6 一致性模型：异步最终一致，不做双写

- 写路径只写 PostgreSQL + outbox，提交即返回；索引刷新在 worker 异步完成。缺省同步窗口 5s（可配置 1–60s），p95 指标为证据；不承诺 read-your-writes。
- 备选：请求内同步写索引（违反 §14.4，Meilisearch 故障会拖垮写路径，且无法原子）；"写完立即重建单文档"（双重写放大与失败处理复杂）。
- 降级：Meilisearch 不可用 → 搜索 API 返回 503 `unavailable`+`retryable`；对象 CRUD 与 outbox 生产不受影响；核心 `/healthz`、`/readyz` 不因搜索故障失败。

### D7 查询 API：`GET /api/v1/objects/search`

- 由 objects 模块注册（chi 静态段优先于 `/{id}`），避免新增顶层业务前缀与模块注册；请求参数：`q`（≤512 字符，trim）、`object_type_id`（可重复，≤10 个）、`updated_after`/`updated_before`（RFC3339）、`limit`（1..100，缺省 20）、`offset`（≥0，≤ `maxTotalHits-1`）、`sort`（`updated_at`/`created_at` × `asc`/`desc`）。
- `q` 与 `object_type_id` 至少一个非空，否则 400 `bad_request`；响应 `{"items":[…],"total":<estimated>,"limit":…,"offset":…,"truncated":…,"request_id":…}`，错误沿用统一信封（与 `objects` 一致）。
- 租户只取自 `authn.RequireTenant(ctx)`，**不使用** `X-Tenant-Id` 回退（比既有 `objects` handler 更严格，属安全加固）；无租户上下文即 401。
- 授权接缝（唯一判定点）：调用平台授权守卫，带 `object_type_id` 时判定对象类型作用域 read 动作（动作目录中的 `object:read`），否则判定租户作用域 read；守卫缺失即视为配置错误——搜索路由不启用（启动校验失败或 503），**不得**注入允许一切的替身。守卫接口签名以 `authorization-openfga-cedar` 契约为准（示意：`Authorize(ctx, action string, resource Resource) error`），本 change 只声明窄接口与调用点。
- 备选：`GET /api/v1/search/objects` 新前缀——需要新模块注册与 `internal/app/` 装配变更更多，收益仅是命名空间更干净；不采用。

### D8 全量重建：暂存索引 + 原子切换 + 窗口重放

1. 记录 `rebuild_started_at`，创建暂存索引 `objects_<v>_<hash>_r<timestamp>` 并应用设置。
2. 按 `id` keyset 扫描 PostgreSQL（UUIDv7 有序），每批 500 条构建文档并 upsert；每批等待 Meilisearch 任务完成。
3. 暂存索引与线上索引原子 `swap-indexes`。
4. 重放 `created_at >= rebuild_started_at` 的租户事件（read-through），补齐重建期间写入；写 `search.index_state.rebuilt_at`。
5. 旧索引保留一版（供回退），下一次成功重建后清理；失败则删除暂存索引，线上索引不受影响。
- 备选：原地清空重建（搜索抖动、半成品对外）；离线导出重建（停机）。选择暂存+swap，搜索持续可用。
- `--verify` 子命令：比较数据库快照与索引文档数量/摘要（排序后逐文档哈希），增量同步后重建也应一致；作为 G2 证据。

### D9 Worker 形态：独立二进制

- `cmd/ontoaxis-search/`：子命令 `consume`（默认）、`rebuild`、`replay`、`migrate`；启动时执行自身迁移、初始化 telemetry、起 health/ready/metrics 端点，支持优雅关闭。
- 备选：core 内 goroutine（需要在 `module` 接口/`internal/app` 增加 Start/Stop 生命周期，属于他人所有权；搜索故障与 core 耦合；无法独立重启/扩容）。
- worker 依赖：PostgreSQL、Meilisearch、OTLP endpoint（可选）。它不服务用户请求、不接触 JWT。

### D10 消费者状态表（worker 自管迁移）

- 新建 schema `search`（由 worker 的 goose 迁移创建，不影响 `jobs`/`object_data`）：
  - `search.outbox_attempt(event_id uuid PK, attempts int, next_attempt_at timestamptz, last_error text, dead_at timestamptz NULL, updated_at timestamptz)`
  - `search.index_state(tenant_id uuid, index_version text, index_uid text, last_event_at timestamptz, last_success_at timestamptz, rebuilt_at timestamptz, PRIMARY KEY(tenant_id,index_version))`
- 成功事件沿用 `jobs.outbox.published_at` 标记；`search.outbox_attempt` 只记录失败/退避/死信，重放 = 清空 `dead_at` 与 `next_attempt_at` 并重置 attempts。
- 不改 `jobs.outbox` 表结构，避免触碰共享平台迁移历史。

### D11 配置与开关（`internal/platform/config/`）

| 变量 | 缺省 | 校验 |
|---|---|---|
| `ONTOAXIS_SEARCH_ENABLED` | `false` | 关闭时不注册搜索路由（未注册路由返回 404） |
| `ONTOAXIS_SEARCH_URL` | 空 | enabled 时必填、合法 URL |
| `ONTOAXIS_SEARCH_API_KEY` | 空 | enabled 时必填；只来自环境/Secret，无内置默认值 |
| `ONTOAXIS_SEARCH_TIMEOUT` | `1500ms` | >0 且 ≤10s |
| `ONTOAXIS_SEARCH_INDEX_VERSION` | `v1` | 匹配 `^v[0-9]+$` |
| `ONTOAXIS_SEARCH_SYNC_WINDOW` | `5s` | 1–60s |
| `ONTOAXIS_SEARCH_POLL_INTERVAL` | `500ms` | 100ms–10s |
| `ONTOAXIS_SEARCH_CONSUMER_BATCH` | `50` | 1–500 |
| `ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS` | `5` | 1–20 |
| `ONTOAXIS_SEARCH_HTTP_ADDR` / `METRICS_ADDR` | `:8081` / `:9094`（worker） | 合法地址 |

### D12 可观测性

- worker 自有 Prometheus registry：`ontoaxis_search_index_events_total{event_type,result}`、`ontoaxis_search_index_lag_seconds`（消费时的 outbox 年龄直方图）、`ontoaxis_search_index_batch_duration_seconds{result}`、`ontoaxis_search_dead_letters_total{event_type}`、`ontoaxis_search_rebuild_documents_total{result}`、`ontoaxis_search_rebuild_duration_seconds{result}`。
- core 搜索查询复用既有 HTTP RED 指标（`ontoaxis_http_*`）与统一错误信封，不新增高基数标签；不修改 `internal/platform/observability/`（他人所有权）。
- trace：core `search.query` → `meilisearch.search`；worker `search.consume.batch` → `search.consume.event` → `meilisearch.<operation>`、`search.rebuild`。属性只用低基数项（`search.index_version`、`search.event_type`、`search.result`、`meilisearch.operation`）；不写租户 UUID、对象属性、查询原文、凭证。
- 告警规则与面板由 `production-delivery` 的 Prometheus rules 承接；本 change 提供指标契约与建议阈值（滞后 >30s、死信 >0、重建失败）。

### D13 测试与证据策略

- 静态：`go build ./...`、`go vet ./...`、`go test ./...`、`go test -race ./...`。
- 单元：映射确定性（含中文、嵌套、超长、敏感键）、索引名派生、参数校验矩阵、错误映射、退避/死信状态机（可注入的 applier）、事件载荷序列化。
- 集成（真实 PostgreSQL + 真实 Meilisearch，`ONTOAXIS_SEARCH_TEST_*` 环境变量注入；未注入时 skip 并在 CI 中必跑）：outbox 同事务/回滚、claim 并发不重复、重复/乱序/未知事件、Meilisearch 停机与恢复、死信与重放、两租户隔离、重建可复现与窗口重放、swap 后旧索引回退。
- E2E：`scripts/object-search-e2e.sh` 在 `search` profile 下跑通 create→search→update→search→delete→search、重建、租户隔离、搜索服务停机降级；输出矩阵作为证据源。
- 中文检索评测：固定评测集（`fixtures/search/`）断言关键词召回与排序基线；阈值需负责人签核，未签核不得判"合格"。
- 真实数据：本 change 不使用真实业务数据；证据全部基于合成 fixture + 真实实例运行记录。

### D14 数据治理与安全边界

- 索引只放展示名与受控文本投影，不放原始属性 JSON；敏感键排除、长度上限、按租户隔离索引、Meilisearch 仅集群内可达且由 master key 保护（密钥来自 Secret）。
- 对象属性的可索引范围属于数据治理决策：本轮先按敏感键排除执行，若安全/数据治理评审要求更严的白名单投影，另开 change 收紧（索引映射版本升级 + 重建）。
- 租户注销时删除索引、索引访问审计：记为后续运维手册与 change 的输入，不在本 change 实现。

### D15 单类型重建：原地收敛（非原子）决策记录

- 现象：`Rebuild(tenant, objectTypeID!=nil)` 在线上索引原地 upsert 并删除 stale（`internal/platform/search/rebuild.go:170-232`），明确不使用暂存/swap——因为按类型构建的暂存索引只含该类型，swap 会丢掉其他类型。
- 决策（2026-11-24，负责人批准）：把原「按租户（可选对象类型）…原子切换」拆为两条——**租户级全量重建**保持暂存+swap 原子；**单类型重建**定义为修复式收敛：允许短暂部分可见、失败可幂等重跑、MUST 不影响其他类型、超分页窗口 fail-closed。规格 delta 已同步拆分（`specs/object-search/spec.md`「租户级全量重建收敛」「单类型修复收敛」）。
- 理由：按类型原子需改成按 (租户,类型) 索引 + 别名扇出（成本高、要迁移），或每次借道整租户 swap（大租户代价高）；单类型修复属低频运维操作，原地收敛可接受且零业务代码改动。
- 触发重新评估的条件：出现「某类型必须秒级原子生效、且租户索引巨大不能整租户重建」的硬需求 → 开新 change 实现按类型原子（推荐 `_gen` 代际过滤或按类型索引别名）。
- 运维要求：单类型命令在手册中标明「修复/收敛」而非「全量重建」；执行前确认 outbox 已追平，执行后跑 `--verify`。

## 部署片段（交 `production-delivery` 合并）

`deploy/compose/compose.yaml` 新增 `search` profile（缺省不启动）：

```yaml
meilisearch:
  image: getmeili/meilisearch:v1.12   # 固定版本，禁止 latest；最终版本由 production-delivery 评审确定
  profiles: ["search"]
  ports: ["18086:7700"]
  environment:
    MEILI_ENV: development
    MEILI_NO_ANALYTICS: "true"
    MEILI_MASTER_KEY: ${ONTOAXIS_SEARCH_MASTER_KEY:?}
  volumes: [ontoaxis_meili_data:/meili_data]
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:7700/health"]
    interval: 5s
    timeout: 2s
    retries: 10

ontoaxis-search:
  profiles: ["search"]
  build: {context: ../.., dockerfile: Dockerfile}
  command: ["/ontoaxis-search", "consume"]
  environment:
    ONTOAXIS_DATABASE_URL: postgres://ontoaxis:ontoaxis@postgres:5432/ontoaxis?sslmode=disable
    ONTOAXIS_SEARCH_ENABLED: "true"
    ONTOAXIS_SEARCH_URL: http://meilisearch:7700
    ONTOAXIS_SEARCH_API_KEY: ${ONTOAXIS_SEARCH_MASTER_KEY:?}
    ONTOAXIS_OTEL_ENDPOINT: http://otel-collector:4317
  ports: ["18087:8081", "19094:9094"]
  depends_on:
    postgres: {condition: service_healthy}
    meilisearch: {condition: service_healthy}
```

core 服务增加 `ONTOAXIS_SEARCH_ENABLED=${ONTOAXIS_SEARCH_ENABLED:-false}`、`ONTOAXIS_SEARCH_URL`、`ONTOAXIS_SEARCH_API_KEY`（enabled 时才需要）。`MEILI_MASTER_KEY` 只来自操作者环境变量，不提交真实密钥；`search` profile 与现有 `edge`/`observability` profile 可叠加。

## 共享文件交接

| 文件/目录 | Owner | 本 change 提供 | 验收 |
|---|---|---|---|
| `internal/app/modules.go`、`app.go` | `stage-2-integration` | 装配 patch：构造 search 客户端/服务并注入 objects；注入授权守卫接缝；worker 不需要 core 生命周期改造 | owner 合并后 `go build ./...`、`make test` 通过，启用时搜索路由可用 |
| `deploy/compose/compose.yaml` | `production-delivery` | 上节 `search` profile 片段与 core 环境变量 | `docker compose -f deploy/compose/compose.yaml --profile search config` 通过，`up` 后 meilisearch/worker healthy |
| `deploy/helm/`、`deploy/argocd/` | `production-delivery` | Meilisearch 与 worker 的 Deployment/Service/PVC/Secret/Probe/资源要求 | helm template 渲染 + kind 部署评审（G2） |
| `docs/stages/evidence/` | `stage-2-integration` | 本 change 的证据源文件（命令、输出摘要、提交/CI 记录） | 证据被收录并链接到 Stage 2 证据索引 |
| `Makefile` | 未登记 owner（建议集成合并） | 可选 `search-e2e` target 调用脚本 | patch 评审通过 |
| `go.mod`、`go.sum` | 需要新依赖的 change 提补丁 | 无改动（D2 不引入依赖） | `git diff go.mod go.sum` 为空 |

## Risks / Trade-offs

- **本地授权结果不覆盖交付环境** → 原生进程已验证 Keycloak/OpenFGA/Cedar，但不能替代 Compose 与外部 IdP 验证；G2 前另行取证且不得 fail-open。
- **授权范围扩大** → G1 通过并不授权生产、真实数据或共享环境变更。当前以隔离本地合成验证为限，超出范围须另行确认。
- **跨 owner 文件依赖**（`internal/app`、`deploy/compose`、evidence）→ 全链路 E2E 需他人合并。缓解：提前提交 patch/片段，G2 前核对交接清单。
- **索引数据可能包含敏感属性** → 属性原文进入搜索引擎有泄漏面。缓解：不索引原始属性、敏感键排除、长度上限、按租户索引、仅集群内访问；数据治理评审未通过前不上线；后续可收紧为白名单投影。
- **中文检索质量未验证** → 召回/排序可能不达业务预期。缓解：固定评测集与基线，阈值签核后才判合格；不达标可按映射/排序规则迭代，不改契约。
- **outbox 表无限增长** → 磁盘与查询性能风险。缓解：监控行数与时间跨度；v1 不删除（重放与证据需要），清理策略另开 change。
- **Meilisearch 单实例** → 搜索 503 降级，无 HA。缓解：核心不依赖搜索；重建与旧索引回退兜底；生产 HA/备份归 `production-delivery`。
- **深分页窗口上限**（`maxTotalHits=10000`）→ 大结果集只能截断。缓解：`truncated` 显式标记 + 文档化；后续如需全量导出走数据集/查询面，而非搜索。
- **事件顺序竞态** → read-through 依赖"每次变更必发事件"；若某次变更漏发会漂移。缓解：全部写路径统一走同一事务辅助函数；`--verify` 对账与定期重建发现漂移。

## Migration Plan

1. 落地 worker 迁移（`search` schema 表）与客户端/映射；此时无消费者，无对外行为变化。
2. `objects` 写路径接入 outbox 生产（同事务）；事件在无消费者时安全堆积。
3. 部署 Meilisearch（`search` profile），启动 worker `migrate` 后 `consume`；先对目标租户执行 `rebuild` 回填。
4. 由 `stage-2-integration` 合并装配 patch，开启 `ONTOAXIS_SEARCH_ENABLED=true`；搜索路由生效。
5. E2E 与证据采集（create/update/delete、重建、隔离、降级），补齐 G2 证据。
- 回滚：关闭 `ONTOAXIS_SEARCH_ENABLED`（搜索路由消失为 404，对象 CRUD 与 outbox 生产不受影响）→ 停止 worker → 删除/保留 `search` schema 与索引；`objects` 表结构与既有 API 无变更，无需数据回滚。映射版本升级回滚 = 切回旧索引（保留一版）并恢复旧 `ONTOAXIS_SEARCH_INDEX_VERSION`。

## Open Questions

- 授权证据取舍：在授权守卫实现前，Stage 2 是否接受「租户隔离 + 一致性」证据、把授权过滤标记为 blocked？需负责人决定，直接影响 G2 与归档条件。
- 授权守卫的最终形态与签名（中间件 vs 服务接口）以 `authorization-openfga-cedar` 契约为准；若守卫以中间件提供，搜索路由的调用点需相应调整。
- 中文检索验收阈值与评测集来源需业务/负责人签核（召回、排序偏好）；未签核前不得判定合格。2026-11-24：工程回归门 `engineering_gate` 已签（冻结语料 + 不低于基线，CI `--strict`），业务阈值与代表性语料仍待签核。
- Meilisearch 固定版本、许可证、资源规格与备份策略需 `production-delivery` 与安全评审确认（本 change 只固定契约测试版本）。
- 对象属性进入全文索引的数据治理边界需安全/数据治理评审确认；本轮按 D5/D14 的敏感键排除与长度上限执行。
- worker 是否纳入 Helm/ArgoCD、备份恢复与生产 HA 范围，由 `production-delivery` 在 G2 前确认。
