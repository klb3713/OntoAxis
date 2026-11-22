# connector-runtime 本地证据 profile（deploy/connector-runtime）

本目录是 OpenSpec change `connector-runtime` 的部署资产：用 Compose **叠加文件（overlay）**
在现有本地栈上增加一条最小数据链路

```
source-postgres (Debezium source)
  → redpanda (Kafka 兼容 broker)
  → kafka-connect (Debezium source + Iceberg sink)
  → iceberg-warehouse 共享卷
  → 可选：Trino Iceberg catalog 读取
```

> ⚠️ **定位**：这是**本地/合成数据的证据 profile**，不是生产部署，也不是高可用方案。
> 本文所有“预期/应”均指设计目标；本仓库在编写时**没有构建镜像、没有启动容器、没有跑 E2E**，
> 验证状态见第 9 节。真实数据不得用于此 profile；不得把“配置校验通过”写成“链路已验证”。

---

## 1. 目录内容

| 路径 | 用途 |
| --- | --- |
| `compose.connector-runtime.yaml` | 主 overlay：新增 `redpanda` / `kafka-connect` / `source-postgres` / `iceberg-warehouse`，全部带 `profiles: ["connector-runtime"]`，不重定义 base 服务 |
| `compose.trino-iceberg.yaml` | 可选 overlay：给 base `trino` 挂 Iceberg catalog 配置、JDBC 驱动与 warehouse 卷 |
| `connect/Dockerfile` + `connect/README.md` | 构建 `kafka-connect` 镜像（Debezium Connect 固定 digest + 从源码构建的 Iceberg 1.12.0 sink 插件），含版本表和未验证说明 |
| `source-postgres/init/01_schema.sql` | 合成夹具：`public.customers` / `public.orders`，主键、`REPLICA IDENTITY FULL`、幂等 publication `oa_publication` |
| `trino/` | `iceberg.properties` catalog 片段、`fetch-plugin.sh`（下载并校验 PostgreSQL JDBC 驱动）、`README.md` 接线说明 |
| `scripts/verify-connector-runtime.sh` | 默认只读健康检查（Connect REST、插件、publication 等）；`--submit-smoke` 才写一个临时 connector |
| `scripts/cleanup.sh` | 按前缀停删 connector、只清理本 profile 服务；**必须显式 `--yes`**，默认不 `down` 整个 project |
| `.gitignore` | 忽略本地下载/构建的插件 jar |

本目录**不修改** `deploy/compose/compose.yaml`、`internal/`、`cmd/`、`go.mod` 或 OpenSpec 文档。

---

## 2. 前置条件

- Docker Engine / Docker Desktop 已安装，Compose v2（本仓库验证使用 `Docker 27.0.3` /
  `Compose v2.28.1-desktop.1`）。
- 资源建议：至少 **4 GB 可用内存、2 vCPU、15 GB 磁盘**（4 个新服务 + Connect 构建缓存；
  Redpanda 限制 `--memory=1G`、Connect `-Xmx1g`）。
- 首次构建 `kafka-connect` 需要网络：Debezium 镜像（Quay）、Apache Iceberg 源码 tarball、
  Maven Central（Gradle 依赖 + PostgreSQL JDBC）；构建时间明显长于普通镜像（Gradle 全量构建）。
- 宿主端口 `19092`、`18086`、`15434` 默认未被占用；如冲突用第 3 节的环境变量覆盖。
- 不需要真实数据/生产连接信息；所有口令都是本地合成默认值。

---

## 3. 启动与停止

### 3.1 启动（基础文件必须在第一条 `-f`，相对路径按 `deploy/compose` 解析）

```bash
cd /Users/klb3713/work/OntoAxis

# 建议先构建 Connect 镜像（首次较慢；构建的是 Debezium + Iceberg 插件）
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime build kafka-connect

# 启动 base 服务 + connector-runtime profile
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime up -d
```

说明：

- 加 `--profile connector-runtime` **同时会启动 base compose 的服务**（postgres、trino、
  ontoaxis-core 等），因为 profile 只控制新增服务是否加入，不会停用无 profile 的服务。
  这是有意设计（Connect/Trino 需要 base 的 postgres/trino）；不需要 base 服务时请单独评估。
- 若宿主端口冲突：

  ```bash
  ONTOAXIS_REDPANDA_HOST_PORT=29092 \
  ONTOAXIS_CONNECT_HOST_PORT=28086 \
  ONTOAXIS_SOURCE_POSTGRES_HOST_PORT=25434 \
  docker compose -f deploy/compose/compose.yaml \
    -f deploy/connector-runtime/compose.connector-runtime.yaml \
    --profile connector-runtime up -d
  ```

- 首次 `source-postgres` 初始化会在**数据卷第一次创建时**执行 `source-postgres/init/01_schema.sql`；
  已有卷不会重跑。需要重建时见第 7 节清理（`--purge-volumes` 后重新 up）。

### 3.2 停止

```bash
# 只停 connector-runtime 的 4 个服务（保留 base 服务与数据卷）
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime stop redpanda kafka-connect source-postgres

# 或使用清理脚本（推荐；包含 connector 删除与容器移除）
./deploy/connector-runtime/scripts/cleanup.sh --yes
```

**不要**直接对包含本 overlay 的 project 执行 `docker compose down -v`：它会连带删除 base
postgres 数据卷与 `ontoaxis_postgres_data`，影响本地主栈。`cleanup.sh` 默认只处理本 profile。

---

## 4. 端口与预期健康

| 服务 | 宿主端口 | 容器端口 | 说明 |
| --- | --- | --- | --- |
| redpanda | `19092`（`ONTOAXIS_REDPANDA_HOST_PORT`） | 19092 | Kafka 兼容 EXTERNAL listener；容器内请用 `redpanda:9092` |
| kafka-connect | `18086`（`ONTOAXIS_CONNECT_HOST_PORT`） | 8083 | Connect REST；容器内 URL `http://kafka-connect:8083` |
| source-postgres | `15434`（`ONTOAXIS_SOURCE_POSTGRES_HOST_PORT`） | 5432 | 合成源库（已开 `wal_level=logical`） |
| iceberg-warehouse | — | — | 一次性 init 容器，创建 `/warehouse` 后 `exit 0` |
| base 栈端口不变 | 18080 / 19090 / 18082 / 18083 / 18081 / 15433 | — | 见 `deploy/README.md` |

健康检查与预期：

```bash
docker compose -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime ps

# 期望：redpanda healthy、kafka-connect healthy（首次约 40–60s）、source-postgres healthy，
# iceberg-warehouse 为 exited (0)。

curl -sS http://127.0.0.1:18086/connector-plugins | head -c 500
# 期望至少包含：io.debezium.connector.postgresql.PostgresConnector
# 构建成功时还应包含：org.apache.iceberg.connect.IcebergSinkConnector

curl -sS http://127.0.0.1:18086/connectors
# 期望：[]（本 profile 不自动创建业务 connector；由 core 应用按 design D3/D5 提交）

./deploy/connector-runtime/scripts/verify-connector-runtime.sh
```

源库逻辑复制参数/夹具核对：

```bash
docker compose -f deploy/compose/compose.yaml exec source-postgres \
  psql -U ontoaxis_source -d ontoaxis_source -c 'SHOW wal_level'   # logical
# publication: oa_publication 覆盖 public.customers、public.orders
```

---

## 5. 核心应用如何启用 runtime（`ONTOAXIS_CONNECT_*`）

connector-runtime 的运行时模块通过 Connect REST 提交/停止/查询 connector（design D5/D9）。
部署资产本身不设置 core 环境变量（overlay 不做 base 服务重定义）；请按你的运行方式注入：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ONTOAXIS_CONNECT_URL` | 空 | Connect REST 地址。宿主进程用 `http://127.0.0.1:18086`；同 compose 网络内的 core 用 `http://kafka-connect:8083` |
| `ONTOAXIS_CONNECT_TIMEOUT` | 模块默认（见 OpenSpec design/spec） | 单次 HTTP 超时 |
| `ONTOAXIS_CONNECT_RETRY_MAX` | 模块默认 | 重试次数 |
| `ONTOAXIS_CONNECT_VERIFY_MAX_ROWS` | `1000000` | 同步后行数校验上限 |

行为边界（design D9）：

- `ONTOAXIS_CONNECT_URL` 为空时，core 模块**仍可启动**，但 StartSync/TestConnector 返回
  unavailable；这是 fail-closed 设计，不是“已启用”。
- 本仓库当前 core 是否已包含该集成取决于 change 的实施进度；**若代码尚未合入，设置变量不会生效**，
  不要据此宣称 runtime 已启用。

给宿主进程的示例（不要提交到仓库）：

```bash
export ONTOAXIS_CONNECT_URL=http://127.0.0.1:18086
# 然后按 core 的启动方式重启；验证 StartSync/TestConnector 不再返回 unavailable
```

> 安全提示：Connect REST 默认**没有认证/TLS**，仅绑定本机给本地开发使用。不要把 18086 暴露到
> 局域网/公网；Connect 内部 topic 会持久化 connector 配置（含数据库口令，见第 8 节）。

---

## 6. 可选：Trino 读取 Iceberg 表

默认启动**不**启用 Trino 的 Iceberg catalog（base `trino` 无 catalog/卷挂载）。启用步骤：

```bash
# 1) ops 准备：在 base postgres 中创建 Iceberg JDBC catalog 的 schema（不动业务迁移）
docker compose -f deploy/compose/compose.yaml exec postgres \
  psql -U ontoaxis -d ontoaxis -c 'CREATE SCHEMA IF NOT EXISTS iceberg_catalog'

# 2) 下载 PostgreSQL JDBC 驱动（SHA256 校验）到 trino/plugins/
./deploy/connector-runtime/trino/fetch-plugin.sh

# 3) 使用第三个 -f 文件启动
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  -f deploy/connector-runtime/compose.trino-iceberg.yaml \
  --profile connector-runtime up -d
```

wiring、catalog 名称对齐（Connect `iceberg.catalog=ontoaxis` ↔ Trino
`iceberg.jdbc-catalog.catalog-name=ontoaxis`）、查询示例和未验证项见
[`trino/README.md`](trino/README.md)。该路径**未验证**；失败时按 design 记录限制或改用 fallback，
不得伪造 Trino 查询证据。

---

## 7. 验证与清理

### 7.1 只读验证

```bash
./deploy/connector-runtime/scripts/verify-connector-runtime.sh
```

检查：Connect REST 可达、必需插件、`GET /connectors`、source-postgres 的
`wal_level=logical` 与 `oa_publication` 表清单。默认**不写**任何东西；`--submit-smoke` 会提交一个
临时 source connector 并在退出时删除（复制槽可能残留，脚本会提示）。

### 7.2 清理（必须显式 `--yes`）

```bash
# 1) 打印计划（不执行）
./deploy/connector-runtime/scripts/cleanup.sh

# 2) 删除以 oa- 开头的 connector，并 stop/rm 本 profile 的 4 个服务
./deploy/connector-runtime/scripts/cleanup.sh --yes

# 3) 只删 connector，不动容器
./deploy/connector-runtime/scripts/cleanup.sh --yes --connectors-only

# 4) 连本 profile 的命名卷一起删除（source-postgres 数据会丢，init SQL 下次重新执行）
./deploy/connector-runtime/scripts/cleanup.sh --yes --purge-volumes

# 5) 明确对整个 project 执行 down（会影响 base 服务；谨慎）
./deploy/connector-runtime/scripts/cleanup.sh --yes --down-stack
```

前缀由 `ONTOAXIS_CONNECTOR_PREFIX`（默认 `oa-`）控制；`ONTOAXIS_KEEP_CONNECTORS=1` 可跳过
connector 删除。Debezium 复制槽不随 connector 自动删除，确认无消费者后手动清理：

```bash
docker compose -f deploy/compose/compose.yaml exec source-postgres \
  psql -U ontoaxis_source -d ontoaxis_source \
  -Atc "SELECT slot_name FROM pg_replication_slots"
# 确认 slot 无 active 后：SELECT pg_drop_replication_slot('<slot_name>');
```

---

## 8. 局限与已知风险（必须如实记录）

1. **Iceberg 插件没有官方二进制**：Iceberg 1.12.0 的 Kafka Connect runtime 只能从源码构建；
   本仓库 Dockerfile 的阶段未在本会话执行过。构建失败/插件不出现时，只能按
   `connect/README.md` 的 fallback 使用 Debezium JDBC sink，并在证据中写明
   “Iceberg 不可用、D1 未验证”。
2. **Kafka 版本错配风险（未验证）**：Debezium 3.7.1.Final worker 内 Kafka 为 4.3.1，而 Iceberg
   1.12.0 的 Kafka Connect 模块按 Kafka 3.x 构建。插件加载/运行兼容性未验证。
3. **Trino JDBC 路径未验证**：PostgreSQL 驱动需挂载到 Trino iceberg 插件目录；Trino 内置
   Iceberg 版本与 Iceberg 1.12.0 的 JDBC catalog schema 兼容性未验证。Trino 没有 HadoopCatalog
   类型，不能把“本地文件 catalog”当作 Trino 可读的替代方案。
4. **Connect 持久化凭据**：connector 配置（含数据库口令）会写入 Connect 内部 topic
   （`oa-connect-configs` 等）与命名卷，删除容器不会清除；清理需删除 connector + 卷，并注意
   本地 Connect REST 无认证、不要对外暴露（design D4 已知残留风险）。
5. **单节点/本地文件 warehouse**：Redpanda 单节点、内部 topic RF=1、Iceberg warehouse 为本地
   命名卷；不具备生产可用性、灾备、多租户隔离能力。生产应使用对象存储、TLS/认证和托管服务。
6. **资源占用**：4 个新服务 + Connect JVM + Redpanda 约需 3–4 GB 内存；Gradle 构建额外占用
   磁盘与 CPU。
7. **base 服务未固定版本**：base compose 中 `trinodb/trino:latest` 等 tag 不在本目录写权限内，
   版本漂移可能影响 Trino 可选路径；如需要请在 base compose 的 owner 处处理。
8. **合成数据**：`source-postgres` 只有 `example.invalid` 虚构数据；不得写入真实客户数据。

---

## 9. 本次交付的验证状态（诚实记录）

| 验证项 | 结果 |
| --- | --- |
| `docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime config -q` | ✅ 通过（编写会话实际执行） |
| 再加 `-f deploy/connector-runtime/compose.trino-iceberg.yaml` 的 `config -q` | ✅ 通过（编写会话实际执行） |
| 解析后的 4 个新服务均带 `profiles: ["connector-runtime"]`，base 服务未被重定义 | ✅ 通过 `config --format json` 检查 |
| 端口默认值插值（19092/18086/15434）与 `ONTOAXIS_*_HOST_PORT` 覆盖 | ✅ 默认值解析正确；覆盖变量未逐一实测启动 |
| 镜像 `docker compose build` / `up` / E2E 数据一致性 | ❌ 未执行（任务禁止启动服务/拉取大镜像） |
| `/connector-plugins` 实际包含 Iceberg sink 插件 | ❌ 未验证（需先构建镜像并启动） |
| Trino 查询 Iceberg 表 | ❌ 未验证（可选路径） |

**插值要求**：overlay 只使用带默认值的 `${ONTOAXIS_*_HOST_PORT:-NNNN}` 与
`ONTOAXIS_SOURCE_POSTGRES_*`（均带默认值），因此 `docker compose config` 不需要额外 `.env`
即可解析；未设置变量时不会产生空值。`source-postgres` 的 healthcheck 使用容器内
`$$POSTGRES_USER`/`$$POSTGRES_DB` 转义，依赖 compose 在运行时展开。

**镜像/校验和锁定**：Debezium Connect `quay.io/debezium/connect:3.7.1.Final@sha256:0c64bbe…`、
Redpanda `redpandadata/redpanda:v26.3.1@sha256:260532f…`、Postgres `postgres:16-alpine@sha256:f2f9872…`、
构建 JDK `eclipse-temurin:21-jdk@sha256:8ad118b…`、Iceberg 1.12.0 源码 SHA512、PostgreSQL JDBC
42.7.7 SHA256。完整表格与“已核对/未验证”拆分见 [`connect/README.md`](connect/README.md)。

---

## 10. 相关文档

- 基础本地栈：[`deploy/README.md`](../README.md)、[`deploy/compose/compose.yaml`](../compose/compose.yaml)
- Connect 镜像与插件版本：[`connect/README.md`](connect/README.md)
- Trino 可选路径：[`trino/README.md`](trino/README.md)
- 设计依据：`openspec/changes/connector-runtime/design.md`（D1/D2/D3/D4/D5/D9）
- 任务与验收：`openspec/changes/connector-runtime/tasks.md`（5.1–5.3）