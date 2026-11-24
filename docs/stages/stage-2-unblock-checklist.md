# Stage 2 阻塞点可执行清单（D-03 / D-04 / D-05）

> 日期：2026-11-23 · 编制：lead · 状态：**待执行（plan）**，不代表任何 AC 通过<br>
> 范围：`connector-runtime`（CDC，D-03）、`object-search-meilisearch`（搜索，D-04）、`production-delivery`（交付，D-05）<br>
> 约定：每步写清「命令 → 期望 → 证据落盘」；未执行不勾选；失败重试与降级路径必须如实记录，adapter/fake 不能冒充真实实例。

## 0. 总览

| 缺陷 | change | 一句话现状 | 阻塞类型 | 完成判据 |
| --- | --- | --- | --- | --- |
| D-03 | `connector-runtime` | source/sink task `RUNNING`、topic 有 snapshot 记录，但 sink lag 不降、Iceberg `snapshots: []`，job 停在 `running` | 代码（路由/提交）+ 校验器缺失 | **已关闭（2026-11-23）**：源行数 = sink 行数 = Trino 查询行数（5=5=5）；job `succeeded`；5.3/6.1–6.4 已勾选 |
| D-04 | `object-search-meilisearch` | client/consumer/rebuild/worker 已在仓库，但 base compose 无 `search` profile、E2E 脚本与 CI job 缺失 | 配置合并 + 测试/证据 | 对象增删改后搜索结果一致；重建可复现；7.5/8.1–8.4 勾选 |
| D-05 | `production-delivery` | kind 集群与 ArgoCD 已存在，root app 因 repoURL 占位报 `authentication required`；镜像 digest 落后 3 个提交 | 环境（GitOps 仓库）+ 镜像 | **已关闭（2026-11-24，收缩范围）**：独立演练集群 9 应用 Synced/Healthy、探针/PDB/备份恢复 L1–L5、两轮回滚有证据；生产级 GitOps/k3d/CI/真实 IdP 移交 `production-delivery-hardening`、`production-idp-staging`，见 `evidence/stage-2-production-delivery-archive-review.md` |
| D-07 | 跨 change（`connector-runtime` / `pipeline-kestra-dbt`） | Kafka Connect 与 Kestra 的 host 端口默认值都是 **18086**，两个 profile 无法同时起栈 | 配置（端口冲突） | 端口改到互不冲突值，`services-and-ports.md` 登记，两个 profile 同栈起停通过 |

## 0.1 已核实的现场（2026-11-23 13:00 左右，只读检查）

- kind：`ontoaxis-kind` 1 control-plane + 2 worker，全部 `Ready`（8h）；`ontoaxis-edge` 集群也在跑。
- 命名空间：`argocd`、`envoy-gateway-system`、`external-secrets`、`oa-system`、`oa-data`、`oa-app`、`oa-observability` 均已创建，但 `oa-*` 下 **没有任何 Pod**。
- ArgoCD：CRD 已装，`Application ontoaxis-root` 状态 `Unknown/Healthy`，条件 `ComparisonError: ... authentication required`。
- Helm：只有 `external-secrets` 一个 release。
- 本地 registry `ontoaxis-registry`（`127.0.0.1:5055`）：只有 `ontoaxis-core`、`ontoaxis-web`，digest 对应 commit `a04ab4c`（比当前 HEAD 落后 3 个提交）。
- Compose 栈当前未运行（无 postgres/trino/core 容器）；有一个 `openfga` 容器在跑。
- 工具：`docker`、`kind`、`kubectl`、`helm`、`k3d`、`kubeconform`、`gitleaks` 可用；**缺** `argocd` CLI、`actionlint`、`shellcheck`、`jq`、`yq`、`kubeseal`（其中 `actionlint`/`shellcheck` 是 9.4/10.4 的必跑项）。

## 0.2 批次 0：先修一致性（不涉及行为，可走 Fast Track）

| # | 动作 | 文件 | 判据 |
| --- | --- | --- | --- |
| 0.1 | Debezium 版本口径对齐：overlay 用 `3.6.1.Final`，`connect/README.md` 与镜像 tag 仍写 3.7.1 | `deploy/connector-runtime/compose.connector-runtime.yaml`、`connect/README.md`、`connect/Dockerfile` | 三处版本一致；`make deps-lock && make deps-check` 通过 |
| 0.2 | 清理 `sinkRouteRegexp` 死代码（上一轮 RegexRouter 实验残留，未被 `BuildSinkConfig` 使用） | `internal/modules/connectors/runtime/sink.go` | `go build ./...` 通过；`grep -rn sinkRouteRegexp` 为空 |
| 0.3 | `trino/README.md` 仍写 `currentSchema=iceberg_catalog`，实际已改 `public` | `deploy/connector-runtime/trino/README.md` | 文档与 `iceberg.properties` 一致 |
| 0.4 ✅ | （2026-11-23 已完成）`defects/index.md` D-03/D-04/D-05 的「待处置」已指向本清单，并登记 D-07 | `docs/stages/evidence/stage-2-integration/defects/index.md` | 四个缺陷行都有可点开的下一步 |
| 0.5 | 修 D-07 端口冲突：Kafka Connect host 端口从 `18086` 改为空闲端口（建议 `18089`），Kestra 保持 `18086`；Meilisearch `17700`、source-postgres `15434`、Redpanda `19092` 一并补进端口表 | `deploy/connector-runtime/compose.connector-runtime.yaml`、`deploy/compose/search/compose.search.yaml`、`docs/architecture/services-and-ports.md` | 两个 profile 能同时 `up -d`；`docker compose ps` 无端口冲突；端口表与 compose 默认值一致 |

## 1. D-03｜CDC 卡在 sink 不提交（`connector-runtime`）

> **进展（2026-11-23）**：配置层修复已完成代码与单测（`DebeziumTransform` + `RegexRouter`），并跑了一轮真实栈验证，结果见 `openspec/changes/connector-runtime/evidence/2026-11-23-sink-transform-round.md`：
>
> - **H0 拆信封成立且已修复**：表列从 `op/after/source/...` 变为 `country/created_at/full_name/id/email/_cdc`。
> - **提交仍卡住**：`snapshots: []`、无 parquet、control 积压 32 且消费组未提交 offset。
> - **但"从未提交"是误判**：修复前的旧 sink 在约 10 分钟内提交过 `added-records=5` 的快照；"Kafka 消费组 lag 不降"不能作为失败判据（Iceberg 把 offset 写进表 metadata）。
> - 第二轮把拆信封改到 **source** 侧（Debezium `ExtractNewRecordState`）后成功：表列 `country/full_name/created_at/id/email`，`snapshot added-records=5`、`total-records=5`，warehouse 生成 parquet，sink `TOTAL-LAG=0`；见 `openspec/changes/connector-runtime/evidence/2026-11-23-sink-unwrap-source-side.md`。
> - 新的阻塞是 **Trino 读不到**：`Metadata not found in metadata location`，根因是 Connect 的 per-dataset warehouse 与 Trino JDBC catalog 的 `default-warehouse-dir` 推断冲突；另有表标识不含 dataset 导致同名表串写。需要设计层决策（共享 warehouse 根 + namespace 隔离 vs 每 dataset 单独 catalog）。
> - 按决策改为 **共享 warehouse 根 + dataset 命名空间隔离**（namespace `ds_<uuid8>_<schema>`）：表位置 `/warehouse/ds_01a20741_public/customers`，列正确，snapshot `added-records=5`，`committed to 1 table(s)`，lag 0；见 `openspec/changes/connector-runtime/evidence/2026-11-23-shared-warehouse-namespace.md`。
> - **Trino 仍读不到 Connect 写入的表**：`CorruptedIcebergTableHandle: Metadata not found in metadata location`。已排除文件可见性、URI 斜杠、catalog_name；Trino 自建表可正常读写，差异待定位。
> - **作业终态已打通**：`SnapshotVerifier` 读 catalog + Iceberg metadata 的 `total-records`，作业 `01a20741-…` 已 `succeeded`、`verified_rows=5`；见 `openspec/changes/connector-runtime/evidence/2026-11-23-snapshot-verifier.md`。限制：不是源/汇行数等式校验（Trino 未通），取消传播仍 503。
> - **取消与负向矩阵已打通**（2026-11-23）：修掉 stopper 未接线、`lease_until` 类型错误 500、提交后过早失败、重复启动不幂等；`scripts/connector-runtime-e2e.sh` 默认 8/8 通过；见 `openspec/changes/connector-runtime/evidence/2026-11-23-cancel-and-negative-matrix.md`。Trino 读取仍挂着，5.3 三方行数断言未完。
> - **6.1 集成测试与 6.4 遥测已闭合**（2026-11-23）：新增 `-tags=integration` 三个真实实例用例并通过；`ontoaxis_connector_*` 指标原先没接进 registry，已通过装配参数接线并在 Prometheus 抓到序列，Jaeger 采样到 `connectors.runtime.submit` / `kafka connect upsert`；详见 `openspec/changes/connector-runtime/evidence/2026-11-23-integration-and-telemetry.md`。
> - **Trino 读取已修复**（2026-11-23）：根因是 `iceberg.properties` 的 `local.location=/warehouse` 让本地 FileIO 把 `file:/warehouse/...` 解析成 `/warehouse/warehouse/...`；改为 `local.location=/` 后 `select count(*)` 正常返回；见 `openspec/changes/connector-runtime/evidence/2026-11-23-trino-local-location-fix.md`。三方行数断言已加入 E2E 脚本。
> - **6.2 三方行数已通过**（2026-11-23）：`--success-only` 干净同步，源表 5 = sink `verified_rows` 5 = Trino 5，作业 `succeeded`；见 `openspec/changes/connector-runtime/evidence/2026-11-23-cdc-e2e-three-way.md`。5.3 仍缺 paused/跨租户与 cleanup.sh 集成；重复写入（rebalance 后重放）仍是已知问题。
> - **更正（2026-11-23）**：上述「仍为待处置」已被同日后续执行关闭——5.3 三方行数与负向矩阵、6.1–6.4 均通过，D-03 已关闭；见 `docs/stages/evidence/stage-2-integration/defects/index.md` 的「D-03 关闭」。


### 1.1 已经排除的原因（不要重复排查）

- ~~Java 25 / `Subject.getSubject`~~：基础镜像已切 Debezium 3.6.1（Java 21），sink task 不再崩。
- ~~插件类加载缺 commons-configuration2~~：已在 `connect/Dockerfile` 补入。
- ~~Trino catalog schema 不一致~~：`iceberg.properties` 已改 `currentSchema=public`，`SHOW SCHEMAS FROM iceberg` 可见 `public`。
- ~~`route-regex` 参数废弃~~：已改静态 `iceberg.tables` + `dynamic-enabled=false`。

### 1.2 剩余根因假设（按可能性排序）

| id | 假设 | 支持证据 | 反证/待查 |
| --- | --- | --- | --- |
| H0 | **Debezium 信封未拆**：source 用 `JsonConverter`（`schemas.enable=false`），记录是 `{before,after,source,op,ts_ms}`；`BuildSinkConfig` 目前**没有任何 transforms**，Iceberg 会把信封当业务行（或根本映射不上），表现为"表建了、没数据" | sink config 无 `transforms`（已核实）；表由 `auto-create-enabled` 预先创建，所以"表存在"不代表写成功；这正是 Debezium→Iceberg 最常见的坑 | 需查表字段：`SHOW COLUMNS FROM iceberg.public.customers` 若出现 `before/after/op` 即坐实 |
| H1 | topic 名带确定性前缀（`oa.<tenant>.<conn>.public.customers`），Iceberg 静态路由按 topic 推导表标识，前缀导致写入表与 `iceberg.tables` 不匹配，sink 建表但丢弃/积压记录 | 消费组 Stable 但 lag 不降；`snapshots: []`；上一轮 RegexRouter 实验后有回退痕迹（`sinkRouteRegexp` 死代码） | 需在 Connect 日志确认 `NoSuchTable` / route 相关异常 |
| H2 | worker 内 Kafka 版本（4.x）与 Iceberg 1.12.0 的 Kafka Connect 模块（按 3.x 构建）不兼容，commit 阶段静默失败 | `connect/README.md` 已记录该跨版本风险 | 需查日志是否有 commit/control topic 异常 |
| H3 | control topic（`control-iceberg`）未创建或与 worker 序列化配置不一致，commit 永不触发 | 无 snapshot、无报错 | 需查 topic 列表与 worker 错误日志 |

### 1.2b 版本现状（2026-11-23 从镜像实测，不是抄文档）

| 组件 | 实际版本 | 来源 | 判断 |
| --- | --- | --- | --- |
| Kafka 客户端/运行时（Connect worker 内） | **4.3.0**（`kafka-clients-4.3.0.jar`、`connect-runtime-4.3.0.jar`） | `docker cp <image>:/kafka/libs` 实测 | 很新，不是"太老" |
| Kafka Connect API | 4.3.0 | 同上 | 同上 |
| Debezium Connect 基础镜像 | **3.6.1.Final** | `deploy/connector-runtime/connect/Dockerfile` 默认 ARG + compose build args | 相对 3.7.1 是降级（为拿 Java 21），但仍带 Kafka 4.3.0 |
| Apache Iceberg 运行时 | **1.12.0**（`iceberg-kafka-connect-1.12.0.jar` 等） | 插件目录实测 | 较新，不是"太老" |
| Iceberg 的 Kafka 编译目标 | **3.x**（`gradle.properties` 的 `defaultKafkaVersions=3`、`knownKafkaVersions=3`，见 `connect/README.md` 引用） | 仓库文档记录，未在镜像内验证 | 与 worker 的 4.3.0 存在**主版本错位** |
| Broker | Redpanda `v26.3.1` | compose overlay | 与插件版本问题无关（插件用的是 worker 自带 client） |
| Trino | `484`（digest 固定） | compose + deps lock | 正常 |

> 结论：**没有"版本太老"的问题，是"版本太新导致对不齐"**。而且镜像 tag 写的是 `3.7.1-iceberg1.12.0`，基础镜像实际是 3.6.1——tag 与内容不符，必须先修（见 §0.2 0.1），否则版本核对会直接失真。

**隔离 H2 的低成本实验（不碰现有栈）**

```bash
# 用 Kafka 3.9 的独立 Connect worker + 同一套 Iceberg 插件，投 5 条普通 JSON 记录
# （不带 Debezium 信封），看 Iceberg 是否产生 snapshot：
#   - 出现 snapshot  → H2 成立（worker Kafka 4.3 不兼容）
#   - 仍无 snapshot → H2 排除，回到 H0/H1 配置问题
```

### 1.3 诊断（只读优先，需先授权起栈）

```bash
# 起 connector 栈（会启动容器，执行前需负责人确认）
docker compose -f deploy/compose/compose.yaml \
  -f deploy/compose/authz/compose.authz.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime up -d --build

# 1) 插件与 connector/task 状态
curl -sS localhost:18086/connector-plugins | python3 -c 'import json,sys;print([p["class"] for p in json.load(sys.stdin)])'
curl -sS localhost:18086/connectors | python3 -m json.tool
curl -sS localhost:18086/connectors/<sink-name>/status | python3 -m json.tool

# 2) Connect 日志中的路由/提交异常（脱敏后入库）
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime logs --tail=300 kafka-connect \
  | grep -iE 'NoSuchTable|Table does not exist|route|commit|Iceberg|control'

# 3) 消费组 lag 是否真的不动
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime exec redpanda rpk group describe connect-<tenant>-<conn>.sink

# 4) Iceberg catalog 是否登记了目标表
#    （连接串与 overlay 里 ONTOAXIS_ICEBERG_CATALOG_URI 一致）
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime exec postgres psql -U ontoaxis -d ontoaxis \
  -c "select catalog_name, table_namespace, table_name, metadata_location from public.iceberg_tables;"

# 5) 表结构：是否把 Debezium 信封当成了业务列（关键判据）
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  -f deploy/connector-runtime/compose.trino-iceberg.yaml \
  --profile connector-runtime up -d trino
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime exec trino trino --execute 'SHOW COLUMNS FROM iceberg.public.customers'

# 6) warehouse 里 metadata 是否有 snapshot
docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime exec kafka-connect sh -c \
  'find /warehouse -name "*.metadata.json" | tail -1 | xargs tail -c 800'
```

期望与判读：

- 若 `SHOW COLUMNS` 里出现 `before` / `after` / `op` / `source` 之类字段 → **H0 成立**（信封没拆），走 1.4-A 的信封部分。
- 若日志出现 `Table ... does not exist` / 路由失败，且 `iceberg_tables` 与 topic 后缀表名不一致 → **H1 成立**，走 1.4-A 的路由部分。
- 若日志出现 Kafka/commit 类 `Exception`（例如序列化、control topic）→ **H3/H2**，走 1.4-B。
- 若三者都干净、lag 仍不降，把日志原文入库，再按 1.4-B 降级验证。

### 1.4 修复路径

**A（首选）配置层一轮修完：拆信封 + 剥 topic 前缀**（两处都是 `BuildSinkConfig`，一次改完、一次重跑）

信封（H0）：
- `transforms=unwrap,...`，`transforms.unwrap.type=io.debezium.transforms.ExtractNewRecordState`（Debezium 镜像自带），让 sink 看到的是业务行而不是 `{before,after,...}`。
- 删除/更新语义：按需设置 `iceberg.tables.default-id-columns=<schema>.<table>:<pk>` 走 upsert；`ExtractNewRecordState` 的 `delete.handling.mode` 与 `drop.tombstones` 要与删除语义一起定，不能默认值了事。
- 单测断言：records 经 SMT 后字段集合与表列一致；不得把 `before/after` 写进表结构。

路由（H1）：
- `BuildSinkConfig` 增加：
  - `transforms=stripTopicPrefix`
  - `transforms.stripTopicPrefix.type=org.apache.kafka.connect.transforms.RegexRouter`
  - `transforms.stripTopicPrefix.regex=^<topic_prefix>\.(.+)$`
  - `transforms.stripTopicPrefix.replacement=$1`
- 直接用已存在的 `sinkRouteRegexp`/`sinkRoutePattern` 生成 regex，消除死代码。
- 单测断言：生成的 config 含 transforms 且 regex 不含凭据；topic 示例 `oa.t1.c1.public.customers` 变换后为 `public.customers`。
- 重跑：删除旧 sink connector → 重建 → 观察 `rpk group describe` lag 归零、`iceberg_tables[].metadata_location` 变化、metadata json 出现 `snapshots`。
- 注意：`iceberg.tables` 仍保留 `public.customers`；若 RegexRouter 与静态路由不可叠加，改用 `iceberg.tables.dynamic-enabled=true` + `route-field`（需要记录数据里存在可用于路由的字段，否则放弃该分支）。

**B（次选）对齐 Kafka 与 Iceberg 版本**

- 确认 3.6.1 worker 内 Kafka 版本（`docker compose ... exec kafka-connect sh -c 'ls /kafka/libs | grep kafka_'`）。
- 若与 Iceberg 1.12.0（Kafka 3.x）不匹配，改用与 worker Kafka 主线匹配的 Iceberg 版本（在 `deploy/deps/policy.yaml` 登记并跑 `make deps-lock`）。
- 版本切换后必须重跑 1.3 全部诊断，不能只看 task `RUNNING`。

**C（最后退路）JDBC sink fallback，必须显式标注**

- 用基础镜像自带的 `io.debezium.connector.jdbc.JdbcSinkConnector`，写入 Postgres 独立 schema，用 Trino `postgresql` catalog 查询行数。
- 证据与 traceability 必须写「Iceberg 不可用，采用 JDBC sink fallback，design D1 未验证」，S2-AC-06 只能标 `partial`，不得标 `pass`。

### 1.5 作业终态（代码任务，否则 E2E 无法 `succeeded`）

- 实现 `SnapshotVerifier`：源表行数 = sink 表行数（Trino 查询）；不一致返回错误并让 job `failed`（不伪造成功）。
- 在 `internal/app/runtime.go` 的 PostMigrate 注入 ingestion runtime（当前传 `nil`）。
- 负向：verifier 不可用/Trino 不可达时 job 必须 `failed` 或保持 `running` 并写明原因，不能 `succeeded`。

### 1.6 收尾任务与证据

- [x] 5.3 `scripts/connector-runtime-e2e.sh`（创建 Dataset → Connector → 同步 → 轮询 → 三方行数；负向：Connect down/非法配置/paused/跨租户/取消）。结果：`--success-only` 7/7、默认 18/18、`--with-outage` 10/10；`cleanup.sh` 未在矩阵调用（销毁性 teardown），以幂等复跑与独立清理替代。
- [x] 6.1 `go test -tags=integration ./internal/modules/connectors/... ./internal/modules/ingestion/...`（3 个真实实例用例通过；`ingestion` 包无 integration 用例，由 E2E 覆盖）
- [x] 6.2 E2E 证据：源行数、sink 行数、Trino 查询行数（5=5=5）、dataset RID、job `succeeded`
- [x] 6.3 失败与负向矩阵（503/400/403/重试耗尽/取消/幂等/伪造租户头 403）
- [x] 6.4 指标/trace 样例移交 `production-delivery`（已抓取样例；告警 firing 未验证，保留为未覆盖）
- 证据路径：`docs/stages/evidence/stage-2-connector-runtime/README.md`（索引）+ `openspec/changes/connector-runtime/evidence/`

### 1.7 风险

- 修改 `BuildSinkConfig` 会同时影响 source/sink 契约测试，需补 `sink_test.go`。
- 若选 B 换版本，可能触发新一轮 Java/Kafka 兼容问题（已踩过 Java 25 的坑）。
- 任何 fallback 结论都必须保留在 traceability，不能被后续提交淡化成 `pass`。

## 2. D-04｜搜索缺 profile 与 E2E（`object-search-meilisearch`）

### 2.1 现状（实现比进度表看起来更完整）

已存在：

- `internal/platform/search/`：client、mapping、consumer（`FOR UPDATE SKIP LOCKED` claim）、rebuild（staging index → `swap-indexes` → 重放窗口事件 → `--verify` 计数对账）、worker（`consume|rebuild|replay|migrate`）。
- 迁移 `00001_search_schema.up.sql` 已含 `search.outbox_attempt`、`search.index_state`（任务 1.4 可能已实质完成，待验证后勾选）。
- `deploy/compose/search/compose.search.yaml`：meilisearch + search-worker + core 的 search 环境变量，`docker compose ... config --services` 可通过。

缺口：

- base compose **没有** `search` profile；overlay 服务也没写 `profiles: ["search"]`，所以任务 7.5 要求的 `docker compose -f deploy/compose/compose.yaml --profile search config` 会直接报 `no such service: search`（已实测）。
- 缺 `scripts/object-search-e2e.sh`（8.1）、CI job（8.2）、检索评测集（8.3）、运维手册与安全评审（9.1/9.2）。
- 5.x 的 claim/keyset/swap/replay/verify/清理/可复现，代码大多在，但**测试与证据未闭环**——先验证再勾选，不要重复实现。

### 2.2 步骤

**P0 合并 profile（两个 change 的边界）**

- 由 `object-search-meilisearch` 提供片段（meilisearch、search-worker、core 的 `ONTOAXIS_SEARCH_*`），由 `production-delivery`（`deploy/compose/` owner）合并进 `deploy/compose/compose.yaml`，`profiles: ["search"]`。
- 验收：`docker compose -f deploy/compose/compose.yaml --profile search config -q` 退出 0；`up -d` 后两个服务 healthy。

**P1 起栈与迁移**

```bash
docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml \
  -f deploy/compose/search/compose.search.yaml up -d --build meilisearch search-worker ontoaxis-core
docker compose -f deploy/compose/compose.yaml -f deploy/compose/search/compose.search.yaml \
  run --rm search-worker migrate
# goose down→up 再跑一次，验证 1.4 的前后兼容
```

**P2 逐条验证已在代码里的 5.x 能力**（能过就补证据勾选，不能过再改）

- 并发 claim：两个 consumer 同时跑，断言不重复处理；事务中断后事件可再领。
- keyset 分批（500）与逐批等待：文档数 = 行数，无重复/遗漏。
- staging → `swap-indexes`：切换期间查询可用，旧索引保留一版。
- 重建窗口重放（`created_at >= rebuild_started_at`）。
- `--verify` 摘要：一致退出 0；人为删改一条文档后非 0 并打印差异。
- 失败清理：注入中途失败，线上索引不变、staging 被删。
- 两次重建摘要一致。

**P3 写 `scripts/object-search-e2e.sh`（8.1）**

- 流程：create → search 命中 → update → search 新值 → delete → search 不再命中 → rebuild → 再次命中 → 租户隔离（A 租户搜不到 B）→ 停 meilisearch 后 API 降级（503 或明确错误，不返回空结果冒充成功）。
- 输出 `PASS/FAIL` 矩阵，退出码非 0 表示失败；证据落 `docs/stages/evidence/stage-2-object-search/`。

**P4 CI（8.2）**：真实 PostgreSQL + Meilisearch 的 integration job；本地未注入 `ONTOAXIS_SEARCH_TEST_*` 时 skip，CI 不允许 skip。

**P5 评测集（8.3）**：`fixtures/search/` 合成数据 + 中文检索基线；阈值需负责人签核后才能判「合格」，否则只输出技术结果。

**P6 文档与评审（9.1/9.2/9.4）**：运维手册（重建/重放/降级/清理/映射升级）、索引字段敏感边界评审、traceability 证据映射。

### 2.3 验收

- `docker compose --profile search config -q` 通过；服务 healthy。
- E2E 脚本全绿；重建两次摘要一致；verify 失败路径可复现。
- 对象增删改后搜索结果一致（S2-AC-07 的唯一判据）。
- 证据含命令、关键输出、时间、环境。

### 2.4 风险

- 搜索模块依赖 outbox 写入端（objects 模块），路径跨 change；E2E 里要显式验证 outbox 行确实写入。
- Meilisearch 单机 master key 只用于本地，证据里不得出现可复用凭据。
- 合并 `deploy/compose/` 属 delivery owner，需按其流程提片段，不要直接改。

## 3. D-05｜交付卡在 GitOps 仓库（`production-delivery`）

### 3.1 现状（比 change map 描述更靠前）

- kind 集群、2 个 worker、ArgoCD、ESO、Envoy Gateway、`oa-*` 命名空间都已存在（8h 前搭好）。
- `Application ontoaxis-root` 已创建，但 `repoURL` 是占位 `https://github.com/ontoaxis/ontoaxis.git`，repo-server 报 `authentication required` → 没有任何子 Application 同步，`oa-*` 里没 Pod。
- registry 里镜像停留在 commit `a04ab4c`；`deploy/releases/kind.yaml` 仍是全零 digest 模板（`delivery-release-check.sh` 会故意失败）。
- 结论：D-05 的第一阻塞是「ArgoCD 拿不到清单」，不是「没有集群」。

### 3.2 阻塞 1：给 ArgoCD 一个可达的 GitOps 源（需负责人选型）

| 方案 | 做法 | 优点 | 代价/限制 |
| --- | --- | --- | --- |
| A（推荐）in-cluster git daemon + kind extraMount | `kind-up.sh` 增加 `extraMounts` 把仓库挂到节点；起一个 `git daemon` Pod 暴露 `git://`；root-app `repoURL` 改为该地址 | 不依赖外网/凭据；可复现；离线 | 需改 `kind-up.sh`（重建集群才生效）或在现有节点用 `docker cp` 注入；`git daemon` 只读 |
| B host `git daemon` + `host.docker.internal` | 宿主起 `git daemon --base-path=<bare>`，repoURL 用 `git://host.docker.internal:9418/ontoaxis.git` | 不动集群 | macOS 上 kind 节点能否解析 `host.docker.internal` 需实测；不稳定 |
| C 真实仓库 + 凭据 | 推到私有 Git，配 repo 凭据 | 最贴近生产 | 涉及凭据边界与安全评审，本地证据不需要 |

选型判据：能在 `kind-down && kind-up` 后一条命令复现，且不引入任何凭据。A 满足；B 需实测；C 超出本地证据范围。

**A 的落地步骤（草案）**

1. `deploy/kind/kind-up.sh`：给 control-plane 加 `extraMounts: [{hostPath: <repo>, containerPath: /workspace/ontoaxis, readOnly: true}]`。
2. 新增 `deploy/kind/gitops-server.yaml`：`git daemon --base-path=/workspace --export-all --reuseaddr`（只读）。
3. `deploy/argocd/root-app.yaml`：`repoURL` 改成 `git://gitops-server.oa-system.svc:9418/ontoaxis`，`targetRevision` 用固定 commit。
4. 验证 `kubectl -n argocd logs deploy/argocd-repo-server | grep -i error` 为空，`ontoaxis-root` 变 `Synced/Healthy`。

### 3.3 阻塞 2：镜像与 release manifest

```bash
# 重建并推送到本地 registry（脚本默认 localhost:5000，本环境实际是 5055）
IMAGE_REGISTRY=localhost:5055 scripts/delivery-images.sh
# 校验 manifest 不再是全零 digest（集群可达时会比对运行中 Pod 的 imageID）
scripts/delivery-release-check.sh --env kind
# 需要跨环境晋升时再跑（kind -> staging-like），失败条件见脚本头部
scripts/delivery-promote.sh --from kind --to staging-like
```

判据：`deploy/releases/kind.yaml` 里不再是全零 digest；`delivery-release-check.sh` 退出 0。

### 3.4 其余任务分批（依赖上面的同步打通）

| 批次 | 任务 | 验收 |
| --- | --- | --- |
| B1 | 3.1 探针、3.2 requests/limits + OOMKilled、3.3 优雅终止、3.4 PDB | kind 中改坏 readiness path → Endpoint 摘除且不重启；`kubectl drain` 被 PDB 拦住 |
| B2 | 5.2 app-of-apps wave、5.3 同步策略、5.4 wave 依赖、5.5 漂移自愈 | 空集群从零部署，wave 顺序可查；手工 patch 后 OutOfSync → self-heal |
| B3 | 5.6 release manifest、5.7 promote、5.8 rollback、5.9 坏部署回滚演练（ImagePullBackOff / progress deadline） | 两次演练回滚成功，`argocd app history` + 烟测 JSON 入库 |
| B4 | 7.1 备份 CronJob、7.2 restore、7.4 RPO/RTO、7.6 runbook | 从最近备份完整恢复，`/readyz` 与只读 API 通过 |
| B5 | 8.3 IdP 检查脚本、8.4 负向、8.5 JWKS 轮换、8.6 fallback、8.7 安全评审 | audience/issuer/过期/坏签名/缺 claims 全部 fail-closed；轮换前后各跑一次 |
| B6 | 9.1 smoke、9.4 静态 CI、9.5 kind CI、9.6 make 目标、9.7 证据链、10.x 文档与门禁 | `scripts/delivery-smoke.sh --env kind` 退出 0；CI 全绿 |

### 3.5 环境前置

- 安装 `actionlint`、`shellcheck`（9.4/10.4 必跑）；`argocd` CLI 可选（用 `kubectl` 查 Application 状态即可）。
- `jq`/`yq` 缺失时脚本需回退到 `python3`（检查 `scripts/delivery-*.sh` 是否依赖）。
- `ontoaxis-edge` 集群是 edge change 的遗留，交付证据只用 `ontoaxis-kind`，不要混用（kind 为主证据栈，k3d 仅开发者回路）。

### 3.6 风险

- 现有集群是 8h 前按 `a04ab4c` 搭的，镜像与 values 都可能过期；**先重建镜像再验证**，否则烟测证明不了当前代码。
- `kind-up.sh` 改 extraMounts 需要重建集群；重建前确认没有需要保留的证据（当前 `oa-*` 为空，风险低）。
- 生产 IdP 验证若没有 IdP 管理权限，8.5 只能标 `not executed`，8.6 fallback 标 `partial`，不能凑 AC-10。

### 3.7 D-07｜Kafka Connect 与 Kestra 端口冲突（跨 change，顺带修）

- 现状：`deploy/compose/pipeline-kestra/compose.pipeline.yaml` 用 `18086:8080`（Kestra），`deploy/connector-runtime/compose.connector-runtime.yaml` 的 `ONTOAXIS_CONNECT_HOST_PORT` 默认也是 `18086`。两个 profile 同时起栈会 `port is already allocated`。
- 归属：端口是 `production-delivery`（`deploy/compose/`）与各 change 的边界；改动本身是 Fast Track（行为不变），但要在两个 change 的证据里各记一笔。
- 步骤：
  1. Connect 默认端口改 `18089`（保留 `ONTOAXIS_CONNECT_HOST_PORT` 覆盖能力）；
  2. `docs/architecture/services-and-ports.md` 补登记 Kafka Connect `18089`、Redpanda `19092`、source-postgres `15434`、Meilisearch `17700`、search worker metrics `19094`；
  3. 验证：`docker compose -f compose.yaml -f connector-runtime -f pipeline-kestra --profile connector-runtime --profile pipeline-kestra config -q` 通过，`up -d` 后两端健康。
- 判据：两个 profile 同栈起停无冲突；端口表与 compose 默认值逐项一致。

## 4. 执行顺序建议

```text
批次 0（一致性清理）
  └─> D-03 诊断 ──> D-03 路由修复 + verifier ──> D-03 E2E/负向 ──┐
  └─> D-04 P0 合并 profile ──> P1/P2 验证 ──> P3 E2E ──> P4 CI ──┤
                                                                  ├─> Wave 1/2 门禁 G1/G2 才有依据
  └─> D-05 3.2 GitOps 选型 ──> 3.3 镜像 ──> B1/B2 ──> B3 ──> B4/B5 ──┘
```

- D-03 与 D-04 互不阻塞，可并行；两者都不依赖 kind。
- D-05 的 B1 之后才需要 ArgoCD 真正同步，所以 GitOps 选型可以和 D-03/D-04 并行推进。
- G2（Wave 2 合并门）要求 8 个前置 change 归档，当前 4 个在途（`stage-2-dependency-governance` 已于 2026-11-23 归档），**即使 D-03/04/05 全部收口也还要等 `stage-2-integration` 与 `production-delivery` 的尾项**。
- 依赖治理的 3 项移交（Helm 22 条占位 digest 回填等）见 `docs/stages/stage-2-change-map.md` §11；其中 6.1 期限 2026-12-31，是 Stage 2 退出条件 6 的硬前置。

## 5. 决策记录（2026-11-23）

| 议题 | 结论 | 理由摘要 | 翻转子 |
| --- | --- | --- | --- |
| D-04 评测阈值 | **本轮不设质量阈值**：8.3 只出基线报告，标注"未签核，仅技术结果"；AC-07 只看一致性（增删改后与库一致、重建可复现、租户隔离）。需要质量门时另立 change（Stage 3），语料与阈值一起做 | AC-07 原文无质量条款；阈值属于产品输入，工程自签等于伪造阈值 | 若产品明确要求"搜得准"作为退出条件，则升级为独立 change 并补标注语料 |
| D-05 GitOps 源 | **方案 A**：`kind-up.sh` 加 `extraMounts`，集群内跑只读 `git daemon`，服务 **bare clone**（不服务工作区），`repoURL` 统一指向它，`targetRevision` 固定 commit SHA | 离线、无凭据、可复现；B 依赖 Docker Desktop 的 host 解析，C 越过安全边界 | 若 repo-server 因网络策略拿不到 ClusterIP，退回 B 并在证据里标注环境依赖 |
| D-03 修复路线 | **已拍板（2026-11-23）**：本轮只做配置层（拆信封 + 剥前缀）；**一轮不成就转版本对齐**；**先不考虑 JDBC**，实在不行再议 | H0/H1 都在 `BuildSinkConfig` 一处改动；JDBC 属数据语义变更，要 spec delta 且 AC-06 只能 `partial` | 加了 SMT 后 lag 仍不动且日志无表/路由异常 → 直接转版本对齐，不在配置上反复试 |

## 6. 边界提醒

- 本清单是计划，不是证据；任何任务勾选都必须有可复跑的命令输出。
- 不启动生产资源、不接真实客户数据、不提交密钥；本地口令只出现在合成 fixture。
- 归档与 Stage 退出仍需单独确认，不能因本清单完成而自动推进。
