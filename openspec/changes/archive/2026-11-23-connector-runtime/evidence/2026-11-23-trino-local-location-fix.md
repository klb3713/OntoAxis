# Trino 读取 Connect 表的根因与修复（2026-11-23）

## 现象

`select count(*) from iceberg.<dataset namespace>.customers` 报：

```
CorruptedIcebergTableHandle: Metadata not found in metadata location for table ds_xxx_public.customers
```

`SHOW TABLES` 能看到表、catalog 行与 metadata 文件存在且可读（以 trino uid 1000 `cat` 验证）。

## 根因

不是 catalog 行、URI 斜杠或权限问题，而是 Trino Iceberg catalog 的本地 FileIO 配置：

- `deploy/connector-runtime/trino/iceberg.properties` 里 `local.location=/warehouse`；
- Trino 的本地 FileIO 会把 `local.location` 作为前缀拼接 `file:` URI，于是 `file:/warehouse/ds_xxx_public/...` 被解析成 `/warehouse/warehouse/ds_xxx_public/...`；
- 该路径不存在，Iceberg 抛 `NotFoundException`，Trino 的 `IcebergExceptions.translateMetadataException` 将其包装成 `ICEBERG_MISSING_METADATA`，即最初看到的 "Metadata not found in metadata location"。

旁证：Trino 自己建的表实际写在 `/warehouse/warehouse/test_ns/...`（同一双重拼接行为），而 Connect 写在 `/warehouse/<ns>/...`，两者布局不一致，所以"Trino 自建表能查、Connect 表不能查"。

## 修复

`deploy/connector-runtime/trino/iceberg.properties`：

```properties
fs.local.enabled=true
local.location=/
```

修复后验证：`select count(*) from iceberg.ds_01a20774_public.customers` 返回 `15`（该表有 3 个各 5 行的 snapshot，与 metadata `total-records` 一致），不再报 metadata 缺失。

## 边界与后续

- 该修复只影响本地 connector-runtime 的 Trino overlay；生产/kind 环境需要各自确认真实 FileIO（S3 等）配置，不能用 `local.location=/` 照搬。
- 既有 Trino 自建测试表（`test_ns.*`）写在 `/warehouse/warehouse/...`，修复后不再可读，属于本地测试数据，可直接删除。
- `scripts/connector-runtime-e2e.sh --with-success` 已加入三方行数断言（源表 = sink total-records = Trino 查询）。
