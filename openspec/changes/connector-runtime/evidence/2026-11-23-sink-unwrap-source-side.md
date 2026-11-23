# CDC 信封拆解改为源端执行（2026-11-23）

## 结论

- **拆信封改在 source 连接器执行后生效**：topic 里是扁平业务 JSON（`{"id":1,"email":...}`），Iceberg 表列是 `country/full_name/created_at/id/email`，无 `_cdc`。
- **提交成功**：`snapshot` 1 个，`added-records=5`、`total-records=5`；warehouse 生成 1 个 parquet；sink 消费组 `TOTAL-LAG=0`。
- **作业仍为 `running`**：`SnapshotVerifier` 仍未接线（`internal/app` 传 nil），作业无法进入 `succeeded`。
- **Trino 查询仍不通**：`Metadata not found in metadata location for table public.customers`，属目录仓库路径与 Iceberg JDBC catalog 期望位置不一致（见下）。

## 为什么改到 source

- 最早的 sink 侧 `DebeziumTransform`（Iceberg 自带）会写 `_cdc` 列，把每条记录标记成 CDC 事件；Iceberg 对 CDC 记录要求表有 identifier fields，而 `SyncConfig` 没有主键 → 提交一直等到超时（`committed to 0 table(s)`）。
- 换成 Debezium 的 `ExtractNewRecordState` 放在 **sink** 侧时，配置能通过（201），但表列仍是信封字段——实测 sink 侧 SMT 未生效（probe 表 `public.probe_customers` 列为 `op/ts_us/after/source/...`）。
- 同一 SMT 放在 **source** 侧实测有效：`probe.test.public.customers` 的 payload 为扁平业务 JSON。
- 结论：当前 Debezium 3.6.1 + Iceberg 1.12.0 组合下，信封在源端展开；sink 只保留 topic 前缀剥离。

## 关键证据

| 项 | 结果 |
| --- | --- |
| source 配置 | `transforms=unwrapDebezium` + `io.debezium.transforms.ExtractNewRecordState` |
| sink 配置 | `transforms=stripTopicPrefix` + `RegexRouter`（`$1.$2`） |
| Iceberg 表列 | `['country','full_name','created_at','id','email']` |
| snapshot | `added-records=5`、`total-records=5` |
| parquet | `.../public/customers/data/00001-*.parquet` |
| sink lag | `TOTAL-LAG 0` |
| job | `running`（verifier 未接线） |
| Trino | 失败：`Metadata not found in metadata location for table public.customers` |

## Trino 读取问题（新发现）

- Connect 把表写在 `<dataset location>/public/customers`，即 `file:/warehouse/ontoaxis/<dataset>/public/customers`。
- Trino 的 Iceberg JDBC catalog 以 `default-warehouse-dir` + `<schema>/<table>` 推断期望位置（`file:/warehouse/public/customers`），与 Connect 的 per-dataset warehouse 路径不一致，故报 `Metadata not found`。
- 这不是配置笔误，是**设计层冲突**：dataset 级 warehouse（隔离）与 Trino JdbcCatalog 的单一 warehouse 根假设不可同时成立。可选方向：
  1. Connect 的 `iceberg.catalog.warehouse` 改为共享根 `/warehouse`，用 namespace 承载 dataset 隔离（同时解掉下面的表名冲突）；
  2. 每个 dataset 一个 Trino catalog（不可扩展，仅本地可用）；
  3. 查询走别的引擎/路径（例如直接读 parquet 或后续 Trino 原生文件系统 catalog）。
- 未选择方向前，S2-AC-06 的"Dataset 可查询"不能判定通过。

## 本轮顺带发现

1. **日志被环境变量打坏**：Debezium 入口脚本把 `CONNECT_LOG4J_LOGGERS` 直接写进 `log4j.rootLogger`，overlay 里的 `...=WARN,...=ERROR` 让日志全部丢失；已从 `deploy/connector-runtime/compose.connector-runtime.yaml` 移除该变量。
2. **取消作业 503**：`ingestion:cancel` 返回 `connector runtime stopper is not configured`，运行中的同步无法通过 API 停止（影响 6.3 取消传播）。
3. **表标识不含 dataset**：两个 dataset 用同名 `<schema>.<table>` 时会写进同一张 Iceberg 表（实测 metadata 落到前一个 dataset 目录），多 dataset 隔离不成立。
4. **构建限制**：`docker compose build` 因 macOS keychain 不可用失败，本轮用交叉编译二进制 `docker cp` 进容器验证；正式证据仍需镜像构建。

## 证据文件

- `dist/connector-e2e/final-metadata.json`（表列、snapshot 计数）
- `dist/connector-e2e/sync8.json`（作业 id）
- 源端探测 payload：`probe.test.public.customers` 消费输出（见本文件正文引用）
