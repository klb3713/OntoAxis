# Design: connector-runtime

## Context

- `connectors` 模块已具备 Connector CRUD、类型/配置校验、`secret_ref` 拒绝明文、测试/Schema 推断/同步启动端口；`ingestion` 模块已具备同步任务状态机、运行记录、取消、监听与租户隔离，但同步端口只有占位实现：`StartSync` 只创建 `queued` 作业，没有任何运行时提交或状态回写，作业不会离开 `queued`。
- Stage 2 要求 Kafka Connect/Debezium CDC 至少一条真实链路并最终生成可查询 Dataset；Meltano 后移 Stage 3。现有数据面只有 Postgres（核心元数据）、Trino（查询）与 web/edge，没有 Kafka 或 Kafka Connect。
- 既有公开契约不能破坏：`connectors`/`ingestion` 的 proto RPC、HTTP 路由与错误语义（未找到/冲突/不可用）保持不变；本 change 只新增行为（delta specs 全部为 `## ADDED Requirements`），不改既有 requirement。
- 共享文件由 change map 指定 owner：`internal/app`、`cmd/ontoaxis-core` 归 `stage-2-integration`；`deploy/compose`、`deploy/helm`、`deploy/argocd` 归 `production-delivery`；`go.mod` 依赖变更由需要方提补丁、集成 change 统一 tidy；`docs/stages/evidence` 归 `stage-2-integration`。本 change 在 `deploy/connector-runtime/` 下提供独立 profile 片段与脚本，由 `production-delivery` 合并（与 `deploy/edge/` 的既有做法一致）。

## Goals

1. 为 `postgres` Connector 生成并提交 Debezium CDC source 与 Iceberg sink 两类 Kafka Connect 连接器，把源表初始快照物化为 Iceberg 表（Dataset）。
2. 用 ingestion 调度器与对账器把 `queued` 作业推进到 `running`/`succeeded`/`failed`/`cancelled`，并在任务/运行记录中反映可审计的运行时状态与统计。
3. 凭据只在提交时解析注入；数据库、日志、指标、trace、HTTP 响应中不出现明文。
4. 幂等、有限重试、取消传播、租户命名空间隔离，且失败不伪造成功。
5. 提供真实容器 profile 与合成数据 E2E，产出 S2-AC-06 可复核证据。

## Non-Goals

- Meltano 接入（Stage 3）；MySQL/S3/REST API/Kafka 的 CDC 映射。
- 同步自动注册 Dataset 目录记录（由 `dataset-catalog` 既有 API 预创建并在 Connector 配置中绑定；自动注册列为后续决策）。
- 持续增量/流式长期运行的运维语义（本 Stage 以"初始快照完成并可查询"作为一次同步的成功判定；持续订阅、Compaction、保留策略属 Stage 3）。
- 多租户共享 Kafka Connect 集群的硬隔离（Stage 2 用命名空间 + 凭据边界；专用集群/Connect 权限模型属后续 hardening）。
- 生产部署、真实客户数据、告警阈值签核（后者归 `production-delivery` 与运维评审）。

## Decisions

### D1: 运行时拓扑

```
源 Postgres (logical replication, synthetic data)
   └─ Debezium PostgreSQL source connector ──► Redpanda topic (oa.<tenanthash>.<connector8>.<schema>.<table>)
                                                      └─ Iceberg sink connector ──► Iceberg warehouse（共享卷）
                                                                                         └─ Trino Iceberg catalog ──► 查询/Dataset
```

- Redpanda（KRaft 单节点）替代完整 Kafka，降低本地 profile 的资源与运维面；Kafka Connect worker 使用 Debezium Connect 镜像。
- Iceberg sink 把 topic 逐表写成 Iceberg 表；warehouse 目录挂载为共享卷，Trino 通过 Iceberg catalog 查询。Dataset 记录由 `dataset-catalog` 既有 API 预创建（`format=iceberg`，`storage_location` 指向 warehouse 下该 Dataset 的目录），Connector 配置中的 `dataset_id` MUST 指向该记录。
- 备选：JDBC sink 写回核心 Postgres 再查询——被拒，因为把接入数据放进元数据库，会和 RLS/迁移/备份边界混在一起，且 Dataset 契约是 `iceberg`/`parquet`；备选：Debezium Server + 自定义消费者——被拒，引入额外 Go 运行时与本 change 的模块边界冲突。
- 备选：复用核心 Postgres 的 logical replication——被拒，核心库不承担演示源库职责，且 RLS 与 WAL 配置会污染生产路径。

### D2: 部署资产边界

- 新增 `deploy/connector-runtime/`：`compose.connector-runtime.yaml`（overlay，服务均带 `profiles: ["connector-runtime"]`）、`connect/`（镜像与插件清单、Dockerfile 或插件校验）、`source-postgres/init/`（合成数据与 `wal_level=logical`、publication、REPLICA IDENTITY）、`trino/`（Iceberg catalog 片段）、`scripts/`（E2E/清理）、`README.md`。
- 本地启动：`docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime up -d`。overlay 只为 Trino 增加 catalog 挂载、为 core 增加运行时相关 env（若 integration 未合并装配则跳过 core 相关部分）。不修改 `deploy/compose/compose.yaml`。
- 端口沿用 `${ONTOAXIS_X_HOST_PORT:-NNNN}` 约定：Redpanda 19092、Kafka Connect 18086、源 Postgres 15434；避免与既有 18080–18085/19090 冲突。
- 镜像与插件固定 digest/版本并记录在 `connect/README.md`；插件可用性在实现阶段验证，若 Iceberg sink 插件不可用则回退 JDBC sink 并把限制写入 design 开放问题与证据限制，不得在证据中掩盖。

### D3: 运行时配置生成

- Connector 的 `postgres` 同步配置白名单（在同步启动时严格校验，不改变创建时的宽松校验）：`host`、`port`、`database`、`schema`（默认 `public`）、`tables`（字符串数组，元素 MUST 匹配 `^[a-z_][a-z0-9_]*$`）、`slot_name`（可选，默认由确定性规则生成）、`publication_name`（可选）、`dataset_id`（必填，UUID）、`topic_prefix`（禁止覆盖，由系统生成）。
- 运行时命名：`source` 连接器 `oa-<tenanthash8>-<connector8>-source`，`sink` 为 `...-sink`；`tenanthash8 = first8(sha256(tenant_id))`，`connector8 = first8(sha256(connector_id))`（实现评审修正：UUIDv7 前 32 位是时间戳，直接截取会在同一时间窗内碰撞；以完整 UUID 做 SHA-256 前缀）；topic 前缀 `oa.<tenanthash8>.<connector8>`。连接器名只含 `[a-z0-9.-]`，满足 Kafka Connect 约束；确定性保证重复提交命中同一运行时对象。
- sink 使用动态表路由：topic `oa.<th>.<c8>.<schema>.<table>` → Iceberg 表 `<schema>.<table>` 位于 warehouse 的 Dataset 目录，Iceberg JDBC catalog 复用核心 Postgres 的独立 `iceberg_catalog` schema（由运维/迁移准备，不在本 change 修改核心迁移）。
- 备选：每租户独立 Connect 集群——被拒（Stage 2 本地成本高，命名空间 + 凭据边界已可验证隔离）。

### D4: 秘密边界与注入

- 新增 `SecretResolver` 端口：`Resolve(ctx, tenantID, secretRef) (map[string]string, error)`；默认实现读取进程环境变量 `ONTOAXIS_SECRET_<NORMALIZED_REF>`，生产可替换为外部秘密后端。解析结果只在组装 Kafka Connect 请求时使用。
- 明文凭据 MUST NOT 写入 `connector.connectors`、`ingestion.sync_jobs/sync_runs`、错误文本、指标 label、span 属性或 HTTP 响应；错误统一映射为"凭据不可用/认证失败"，原始异常只保留脱敏片段。
- 残余风险（写入 Risks）：Kafka Connect 自身会把连接器配置（含密码）持久化到其内部 config topic/storage。缓解：专用 Connect 集群/本地一次性 profile、文件权限与网络边界、API/日志脱敏；长期方案（外部化 secret + Connect config provider）列为 Stage 3 开放问题。

### D5: 提交与任务状态机

- 复用既有状态机（`queued→running→succeeded|failed`；`queued|running→cancelled`；终态不可变）。新增内部调度信息，不扩展公开状态枚举：
  - `ingestion.sync_jobs` 增列（additive）：`runtime_ref text`、`attempt int default 0`、`lease_owner text`、`lease_until timestamptz`；`sync_runs.stats` 记录 `runtime_ref`、`attempt`、`phase`（`submitting|snapshotting|verifying|stopping`）、`verified_rows`、`cleanup_pending`。
- 调度（dispatcher，模块内 goroutine，可多实例）：条件更新领取 `status='queued' AND (lease_until IS NULL OR lease_until < now())`，写入 `lease_owner/lease_until`；在租户上下文中生成配置、解析凭据、通过 Kafka Connect REST `PUT /connectors/<name>/config`（upsert 幂等）提交 source 与 sink；成功后 `UpdateJobAndRun(running)` 并记录 `runtime_ref`。
- 提交失败分类：`4xx` 参数/认证/冲突 → 永久失败立即 `failed`；`5xx`/超时/连接错误 → 有限指数退避重试（1s/2s/4s/8s/16s，最多 5 次，抖动 ±20%），耗尽 → `failed`。所有路径都释放租约并保留脱敏错误。
- 提交前预检（`StartSync` 同步完成）：类型支持、必要配置、Kafka Connect 可达；失败保持既有错误映射（参数错误/冲突/不可用），不创建作业。这与既有 connectors 契约一致，也避免异步创建后才发现不可用。
- 备选：公开状态增加 `submitting`——被拒，会破坏既有 proto 枚举与前端契约；改用运行统计的 `phase` 表达。

### D6: 状态对账与成功确认

- 对账器（默认 5s 周期，批量 50，按租户上下文）：只扫描 `running` 作业，读取 Connect 中 source/sink 状态并归一（`running`/`failed`/`paused|stopped|missing`/`unknown`）。
  - 运行时失败 → 作业 `failed`，错误取 task trace 的脱敏首行；运行时在作业 `running` 期间缺失/停止 → `failed`（`runtime_not_running`）；`unknown` 保持 `running` 但不判定成功。
- 初始快照完成判定（成功条件，全部满足）：
  1. source 与 sink 连接器均为 RUNNING，且无失败 task；
  2. sink 对应 Iceberg 表存在已提交 snapshot，且 `SELECT count(*)` 可执行；
  3. sink 行数与派发前记录在 `sync_runs.stats.expected_rows` 的源表行数一致（预检用同一 `SecretResolver` 解析的凭据对源库做有界计数，`ONTOAXIS_CONNECT_VERIFY_MAX_ROWS` 默认 1,000,000，超过上限则跳过计数、以"表+snapshot 存在"为部分证据并明确记录限制）；
  4. 连续两次对账（≥5s）行数稳定。
- 满足后：作业 `succeeded`，记录 `verified_rows`、`dataset_id`、完成时间与 `runtime_ref`；随后尝试删除 sink 与停止 source，失败记录 `cleanup_pending`，不撤销成功、不谎报已停止。
- 备选：Debezium JMX 指标判定 snapshot 完成——被拒（需要 JMX 桥接与额外依赖，REST 无该信息）；备选：固定等待时间——被拒（不可靠、易产生假成功）。
- 限制与开放问题：大表或持续写入源表下，行数比对可能昂贵或误判；Stage 3 用 offset/watermark（如 sink 提交的 source LSN 与 source slot confirmed_flush_lsn 比较）替代。

### D7: 取消传播

- `CancelJob`：`queued` 且租约未持有 → 直接 `cancelled`；`queued` 正在提交或 `running` → 调 `StopRuntime`（停止 task、可选删除连接器），确认后 `cancelled` 并释放租约；未确认 → 返回错误、保持 `running`（可重试），不得报告已取消。
- 终态取消返回冲突；对账不覆盖终态。

### D8: 可观测性

- 指标（遵循 `runtime-observability` 基线，前缀与标签白名单）：复用 `ontoaxis_jobs_total{type,result}` 记录同步作业结果；新增 `ontoaxis_connector_runtime_requests_total{dependency="kafka_connect",operation,result}`、`ontoaxis_connector_submit_seconds{operation,result}`、`ontoaxis_ingestion_reconcile_duration_seconds{result}`、`ontoaxis_ingestion_retries_total{operation,result}`；标签不含 tenant/job/connector/topic。
- span：出站 `HTTP <method> <route>` 语义下补 `dependency=kafka_connect`、`operation`、`result`；模块 span `ingestion.dispatch`、`ingestion.reconcile`、`connectors.submit_runtime`；不记录凭据、请求体、原始异常。
- dashboard/告警片段放在 `deploy/connector-runtime/observability/`，移交 `production-delivery` 合并；告警阈值列为开放项。

### D9: 失败、降级与回滚

- 运行时未配置（`ONTOAXIS_CONNECT_URL` 为空）→ 模块仍可启动，`StartSync`/`TestConnector` 返回不可用，不创建作业；这与 Stage 1 的 fail-closed 占位行为一致。
- 模块装配通过 Option 注入：`WithSyncRuntime(...)`、`WithSecretResolver(...)`、`WithRuntimeNaming(...)`；`internal/app` 装配 patch 交 `stage-2-integration` 合并；未合并时默认禁用（不影响既有测试与启动）。
- 回滚：关闭 profile 与配置键即回到占位适配器行为；迁移逆序 DROP 新增列（既有数据不受影响）；已 `running` 作业在回滚后因运行时不可达进入 `failed`（可审计），不伪造成功。

## Risks / Trade-offs

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| Iceberg sink 插件/镜像版本兼容性未在规划期验证 | 真实链路可能无法落地 | 实现任务先做 `GET /connector-plugins` 验证并固定 digest；备选 JDBC sink，限制写入证据 |
| Kafka Connect 持久化含密码的连接器配置 | 凭据在基础设施层留存 | 专用本地 profile、文件权限/网络边界、脱敏；外部化列为 Stage 3 |
| 行数比对判定快照完成 | 大表成本高、并发写可能误判 | 有界计数 + 稳定两次；限制与后续 watermark 方案入开放问题 |
| 引入 Redpanda/Connect 新基础设施 | 本地资源与 CI 时长上升 | profile 隔离、容器 healthcheck、集成测试加超时与显式 skip 开关 |
| 多实例调度竞态 | 重复提交/双写 | 条件更新 + 租约；运行时 upsert 幂等双保险 |
| `deploy/compose`、`internal/app` 装配未合并时 E2E 不可跑 | 门禁受阻 | 先模块内集成验证；E2E 任务标 blocked_by integration 合并 |
| Trino catalog overlay 与 delivery 合并冲突 | 查询路径不通 | overlay 只增不改；以片段+说明移交，冲突由 delivery 决定 |
| 合成数据被误当真实验收 | 证据失实 | 证据文件显著标注"合成数据/本地 profile"，真实数据验收另列 |

## Migration Plan

1. 迁移（additive）：`connector`/`ingestion` schema 内新增列与索引；Down 逆序删除。无数据回填。
2. 配置：新增 `ONTOAXIS_CONNECT_*` 键，默认空/禁用；未经 integration 合并前模块行为不变。
3. 部署：先发布模块代码（默认禁用），再合并装配与 profile，最后在本地 profile 跑 E2E；生产部署不在本 change 范围。
4. 回滚：关闭配置 → 恢复占位行为；必要时执行 Down 迁移；运行时容器与数据卷独立清理（`scripts/cleanup.sh`）。
5. 文档：ARCHITECTURE §7/§15.2 状态更新建议与模块 README 片段由 `stage-2-integration` 合并；`docs/stages/evidence` 由 integration 归档本 change 提供的证据源文件。

## Open Questions

1. Dataset 自动注册：是否需要在同步成功后由 ingestion 通过端口创建 Dataset 记录（需 `ingestion→datasets` 依赖与 integration 装配）？当前由 `dataset-catalog` API 预创建。
2. 快照完成判定：大表/持续写入场景是否改为 offset/watermark 判定？Stage 2 先接受行数比对限制。
3. 秘密后端：Stage 3 是否引入 Vault/Connect config provider 外部化凭据？
4. 多租户运行时隔离：是否按租户独立 Kafka Connect 集群或 namespace 级 ACL？
5. 告警阈值与 dashboard：由 `production-delivery` 还是 Connector 运维评审签核？
6. 本地端口 18086/19092/15434 是否与团队其他 profile 冲突（由负责人确认）。

## Test Strategy

- 单元（默认 CI）：`httptest` 假 Kafka Connect（提交/状态/停止/4xx/5xx/超时/非法 JSON/幂等）；配置生成 golden（确定性、租户命名空间、非法表名/配置拒绝、无凭据泄漏）；SecretResolver（成功/缺失/注入与脱敏）；调度器（条件领取并发、重复调度、重试退避、失败分类）；对账器（状态映射矩阵、终态保护、快照判定、稳定两次）；取消传播；租户隔离（内存 store 模拟 RLS 语义）。测试断言日志/错误/指标/span 无明文。
- 集成（profile `connector-runtime`，`go test -tags=integration`，按 env 开关跳过）：真实 Redpanda + Connect + 源 Postgres + Iceberg warehouse + Trino；真实提交、状态读取、停止、幂等；真实失败（Connect 停止、错误凭据、非法配置）。
- 全栈 E2E（依赖 integration 合并装配）：core HTTP API → 创建 Dataset/Connector → 启动同步 → 轮询作业 → Trino 查询行数；负向（Connect down 503、非法配置 400、paused 409、跨租户 404、取消传播、重试耗尽）。
- 合成数据：`source-postgres/init/*.sql` 建 `demo` schema 两张表（约 50–100 行、主键、`REPLICA IDENTITY FULL`）与 publication；不使用任何真实客户数据。
- 证据分层：单元输出 → 集成测试日志 → E2E 脚本输出（job id、runtime connector 名、源/sink/查询行数、脱敏错误、指标抓取样例）→ 证据文件（由 `stage-2-integration` 归档到 `docs/stages/evidence/`）。
