# Trino Iceberg 读取路径（可选，本地证据 profile）

本目录提供把 **核心 compose 的 Trino** 接到 connector-runtime 产生的 Iceberg 表上的 catalog
片段与接线说明。它是**可选**路径，且**未做端到端查询验证**（任务禁止启动服务）。

## 1. 组件关系

```
source-postgres (Debezium source)
  └─> redpanda topics oa.<tenanthash8>.<connector8>.<schema>.<table>
        └─> Kafka Connect + Iceberg sink (JdbcCatalog)
              ├─ 元数据: 核心 postgres 的 iceberg_catalog schema (iceberg_tables / iceberg_namespace_properties)
              └─ 数据:   ontoaxis_iceberg_warehouse 卷 (/warehouse)
                    └─> Trino iceberg catalog (JDBC) 读取
```

两侧必须一致的四件事：

1. **catalog 名称**：Connect connector 配置 `iceberg.catalog=ontoaxis`（Iceberg sink 的
   catalog name 属性，默认 `iceberg`），Trino 的 `iceberg.jdbc-catalog.catalog-name=ontoaxis`。
   Iceberg JdbcCatalog 的表行按 `catalog_name` 过滤，名称不一致会“看不到表”。
2. **JDBC URL**：两边都用
   `jdbc:postgresql://postgres:5432/ontoaxis?currentSchema=iceberg_catalog`。
3. **warehouse**：两边都指向 `file:///warehouse`（容器内路径），共享同一命名卷。
4. **schema-version**：Trino 默认 V1；建议 Connect 侧显式设置
   `iceberg.catalog.jdbc.schema-version=V1` 后再由 Trino 读取，避免 Iceberg JdbcCatalog
   首次建表版本不一致。

## 2. 前置步骤（ops 准备，本 change 不写迁移）

```bash
# 1) 创建 Iceberg JDBC catalog 的 schema（只在核心 postgres 中建 schema，不动业务表）
docker compose -f deploy/compose/compose.yaml exec postgres \
  psql -U ontoaxis -d ontoaxis -c 'CREATE SCHEMA IF NOT EXISTS iceberg_catalog'

# 2) 下载 Postgres JDBC 驱动到 trino/plugins/（校验 SHA256；Maven 无 .sha256，值见 README 表格）
./deploy/connector-runtime/trino/fetch-plugin.sh

# 3) 带三个 -f 文件启动（第三个文件启用 Trino catalog 挂载）
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  -f deploy/connector-runtime/compose.trino-iceberg.yaml \
  --profile connector-runtime up -d --build
```

对应挂载（见 `compose.trino-iceberg.yaml`）：

| 宿主/卷 | 容器路径 | 作用 |
| --- | --- | --- |
| `trino/iceberg.properties` | `/etc/trino/catalog/iceberg.properties` | catalog 配置（只读） |
| `trino/plugins/postgresql-42.7.7.jar` | `/usr/lib/trino/plugin/iceberg/postgresql-42.7.7.jar` | JDBC 驱动（只读） |
| `ontoaxis_iceberg_warehouse` 卷 | `/warehouse` | 共享 Iceberg warehouse |

## 3. 使用示例（假设已成功启动且有 connector 写入）

```bash
docker compose -f deploy/compose/compose.yaml exec trino trino --execute 'SHOW CATALOGS'
docker compose -f deploy/compose/compose.yaml exec trino trino --execute 'SHOW SCHEMAS FROM iceberg'
docker compose -f deploy/compose/compose.yaml exec trino trino --execute 'SHOW TABLES FROM iceberg.<schema>'
docker compose -f deploy/compose/compose.yaml exec trino trino \
  --execute 'SELECT count(*) FROM iceberg.<schema>.<table>'
```

具体 schema/table 名由 core 应用生成的 sink connector 配置（design D3：`<schema>.<table>`）
决定；本目录不预设固定表名。

## 4. 配置依据（已核对源码）

- `IcebergJdbcCatalogConfig`（trinodb/trino tag 478）：
  `iceberg.jdbc-catalog.driver-class`（@NotNull）、`connection-url`（@NotNull、敏感值）、
  `connection-user`、`connection-password`、`catalog-name`（@NotEmpty）、
  `default-warehouse-dir`（@NotEmpty）、`schema-version`（默认 V1）。
- `FileSystemConfig`（tag 478）：`fs.native-local.enabled`；
  `FileSystemModule` 第 124–127 行在启用时把 `"local"` 与 `"file"` 两个 scheme 绑定到
  `LocalFileSystemFactory`；`LocalFileSystem` 对绝对路径直接映射到容器内文件系统。
  因此 Connect 写入的 `file:///warehouse/...` 在 Trino 挂载同一路径后可达。
- Iceberg JDBC catalog（iceberg-core 1.12.0 `JdbcCatalog.class`）内部使用固定表
  `iceberg_tables` / `iceberg_namespace_properties`，没有独立 schema 属性；所以用 Postgres
  JDBC URL 的 `currentSchema` 参数把表放进 `iceberg_catalog`。

## 5. 验证状态与已知风险（诚实记录）

**已核对**

- 属性名、必填项与默认值来自 Trino 478 源码。
- 本地文件系统 `file` scheme 的绑定方式来自 Trino 478 源码。
- 驱动 SHA256 已由 `fetch-plugin.sh` 校验（见 connect/README.md 版本表）。

**未验证（不得声称通过）**

- Trino 容器实际加载 PostgreSQL 驱动、JDBC catalog 初始化、`SELECT` 查询。
- Trino 478 内置 Iceberg 库版本与 Iceberg 1.12.0（Connect 侧）之间的 JDBC catalog
  schema/元数据兼容性；若不兼容，需要对齐版本或在 Trino 侧固定同版本镜像。
- Iceberg sink 在 Kafka 4.3.1 worker 上的行为（见 connect/README.md）。
- 权限/只读挂载：Trino 需要能读 `/warehouse`（本方案为 0777，仅本地证据可用）。

**如果 Trino 路径不可用**：Trino 没有 HadoopCatalog 读取路径可用（其 `iceberg.catalog.type`
支持 hive_metastore/glue/jdbc/rest/nessie/snowflake，不支持 hadoop），因此不要用“本地文件
catalog”来宣称 Trino 可读。可选降级：
1) 用 DuckDB/Spark 本地读取 warehouse 中的 Iceberg 文件，记录为“非 Trino 证据”；
2) 改用 design 允许的 JDBC sink fallback，明确标注 D1/Trino 未验证；
3) 报告限制并停止，不伪造成功。