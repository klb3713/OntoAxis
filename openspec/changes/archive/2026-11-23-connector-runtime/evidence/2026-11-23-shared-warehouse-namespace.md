# 共享 warehouse 根 + dataset 命名空间隔离（2026-11-23）

## 结论

- **仓库方案已落地并验证到数据层**：`iceberg.catalog.warehouse` 改为共享根，表按 `<dataset namespace>.<table>` 隔离；namespace 由 `ds_<dataset uuid 前 8 位>_<schema>` 生成，形如 `ds_01a20741_public`。
- 实测结果：表位置 `/warehouse/ds_01a20741_public/customers`，列 `country/full_name/created_at/id/email`，snapshot 1 个（`added-records=5`），warehouse 生成 parquet，sink `TOTAL-LAG=0`，Connect 日志 `committed to 1 table(s)`。
- **Trino 仍读不到 Connect 写入的表**：`select count(*) from iceberg.ds_01a20741_public.customers` 报 `Metadata not found in metadata location for table ...`（`CorruptedIcebergTableHandle`）。
- 已排除的原因：文件确实在 Trino 容器可见（`/warehouse/ds_.../customers/metadata/*.json`，0644 可读）；`iceberg_tables` 行存在且 `metadata_location` 非空；URI 单/三斜杠改写无效；`catalog_name` 必须为 `ontoaxis`，改成 `iceberg` 会让表直接从 `SHOW TABLES` 消失（说明 Trino 按配置的 catalog-name 过滤，配置本身生效）。
- 对照实验：Trino 自己在 `iceberg.test_ns.t` 建表 → 插入 2 行 → 查询返回 2，说明 Trino 的 JDBC catalog 通路可用；差异集中在"Connect 写入的表"与 Trino 期望的表句柄/位置约定。

## 已知差异（待定位）

| 项 | Trino 自己建表 | Connect 写入 |
| --- | --- | --- |
| 表位置 | `<warehouse>/<ns>/<table>-<uuid>` | `<warehouse>/<ns>/<table>` |
| catalog_name | ontoaxis | ontoaxis |
| 可见性 | SHOW TABLES 可见 | SHOW TABLES 可见 |
| 查询 | 正常 | `CorruptedIcebergTableHandle` |

推测：Trino 484 的 `IcebergMetadata.checkValidTableHandle` 对表句柄的 metadata location 做了额外校验（例如与 catalog 侧位置推导不一致），需要读 Trino 源码或调整 Connect 的 `iceberg.catalog.warehouse`/表位置生成方式。**未验证，不得当作结论。**

## 本轮代码改动

- `internal/modules/connectors/runtime/sink.go`：
  - 新增 `DatasetNamespace(datasetID, schema)`（`ds_<8hex>_<schema>`）与 `Warehouse.root()`；
  - `iceberg.catalog.warehouse` 绑定共享 warehouse 根；
  - `iceberg.tables` 与 RegexRouter replacement 使用 `<dataset namespace>.<table>`，表标识唯一化，解决多 dataset 同名表串写。
- `sink_test.go` 同步更新（warehouse 根、namespace 表名、replacement）。
- 保留了 `DatasetLocation`（Dataset 记录的 `storage_location` 仍为描述性字段，sink 不再使用）。

## 证据

- `dist/connector-e2e/ns-metadata.json`（表列、snapshot）
- `dist/connector-e2e/sync9.json`（作业 id）
- Connect 日志：`Successfully committed to table ds_01a20741_public.customers`、`committed to 1 table(s)`
- Trino 对照：`iceberg.test_ns.t` 的 CREATE/INSERT/SELECT 成功

## 未完成

- Trino 读取 Connect 表的兼容性未解决 → S2-AC-06 的"Dataset 可查询"仍不能判定通过。
- 作业终态：`SnapshotVerifier` 未接线，job 仍 `running`。
- 提交间隔默认 5 分钟，本地 E2E 每次验证需等两个窗口（第一次 START_COMMIT 通常超时，第二次提交成功）；后续可考虑把 `iceberg.control.commit.interval-ms` 做成可配置（未做）。
