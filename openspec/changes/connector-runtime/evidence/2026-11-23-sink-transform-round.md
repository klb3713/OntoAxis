# connector-runtime sink 变换链一轮验证（2026-11-23）

## 结论

- **拆信封（H0）确认成立且已修复**：修复后新建的 Iceberg 表列是业务字段 `country/created_at/full_name/id/email` + `_cdc`；修复前同一链路的表列是信封字段 `op/ts_us/after/source/ts_ns/ts_ms`。
- **提交仍未发生**：修复后 sink task `RUNNING`，但 `snapshots: []`、warehouse 无 parquet、control topic 积压 32 条且 control 消费组未提交任何 offset。
- **但"从没提交过"这个判断是错的**：修复前的旧 sink 在约 10 分钟后确实提交过一版 snapshot（`added-records=5`、task-id `oa-bd7662a5-f090ea3d-sink-0`），只是写入的是信封表。这说明**提交链路本身能工作**，也说明"Kafka 消费组 lag 不下降"不能当作失败判据——Iceberg sink 把 offset 写进表 metadata，不写 Kafka 消费组。
- 因此本轮之后，**版本不匹配（H2）不再是首选解释**；更可能是新变换链（`DebeziumTransform` 的 `_cdc` 列或标识字段缺失）导致写入/提交阻塞。规则上"一轮不成转版本对齐"仍成立，但应先做一次**范围更窄的复测**再决定，避免把回归误判成版本问题。

## 环境

- 栈：`deploy/compose/compose.yaml` + `authz/compose.authz.yaml` + `connector-runtime/compose.connector-runtime.yaml`，`--profile connector-runtime`。
- 镜像：`ontoaxis/connector-runtime-connect:3.7.1-iceberg1.12.0`（基础 Debezium 3.6.1，worker 内 Kafka 4.3.0；Iceberg 1.12.0）。
- 二进制补丁：`ontoaxis-core` 镜像无法重建（Docker Desktop 构建需要 keychain 凭据，当前会话不可用），改用 `GOOS=linux GOARCH=amd64` 交叉编译后 `docker cp` 进容器并重启；**该限制必须写进证据边界**，正式证据仍应来自镜像构建。

## 关键观察

| 项 | 修复前（旧 connector） | 修复后（新 connector） |
| --- | --- | --- |
| sink 变换 | 仅 `stripTopicPrefix`（replacement 空串） | `unwrapDebezium`（`DebeziumTransform`）+ `stripTopicPrefix`（`$1.$2`） |
| 表列 | `op, ts_us, after, source, ts_ns, ts_ms` | `country, created_at, full_name, id, email, _cdc` |
| snapshot | 有（`added-records=5`，约 10 分钟后） | 无（等待 > 8 分钟） |
| warehouse 数据文件 | 1 个 parquet | 无 parquet |
| control topic 积压 | 6（随后被消费，提交成功） | 32 且持续增长，control 消费组 `CURRENT-OFFSET=-` |

## 现象解释（当前最可能）

1. `DebeziumTransform` 生效（表列已变成业务字段 + `_cdc`），但 sink 在写入阶段未产出 parquet。
2. `_cdc` 表示 CDC 事件，Iceberg sink 对 CDC 记录通常要求表具备 identifier fields；当前 `SyncConfig` 不含主键，`iceberg.tables.default-id-columns` 未生成（design D3 已记录为开放问题）。缺少标识字段时写入会停滞或反复失败，而错误没有出现在容器日志里（Debezium 镜像的 log4j 输出未落到 stdout，`org.apache.iceberg=DEBUG` 也未产生可见日志）。
3. 另一条可能：control 协调器在本轮未消费事件（control lag 增长），与"写入阻塞"可能互为因果。

## 修复前的错误尝试（不计入结论）

- 直接删除 connector 后重建，因 `oa-connect-offsets` 里保留了旧 LSN，source task 报 `LSN ... no longer available`；`DELETE /connectors/{name}/offsets` 在 Debezium 3.6.1 返回 404，只能通过新建 connector（新 hash）绕开。

## 建议下一步（二选一，需负责人定）

1. **窄范围复测（推荐，成本约 10 分钟）**：把 unwrap 从 `DebeziumTransform` 换成 Debezium 的 `ExtractNewRecordState`（不写 `_cdc`），重跑一次干净快照；若有 snapshot 且行数为 5，说明问题在 `_cdc`/标识字段，版本无责。
2. **直接转版本对齐**：用 Kafka 3.x 的独立 Connect worker + 现有 Iceberg 插件做隔离实验（需拉取 `apache/kafka:3.9.x` 镜像，无需本地构建）。

## 证据文件

- `dist/connector-e2e/baseline-sink-config.json`、`baseline-metadata.json`、`baseline-lag.txt`（修复前）
- `dist/connector-e2e/fix-sink-config.json`、`fix-metadata.json`、`control-lag.txt`（修复后）
- `dist/connector-e2e/ontoaxis-core-linux`（本轮补丁二进制，不入库）
