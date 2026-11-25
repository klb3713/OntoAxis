# Kafka Connect 插件与镜像（connector-runtime 本地证据 profile）

本目录产出 `kafka-connect` 服务使用的镜像：**Debezium Connect（固定版本/digest）+ Apache Iceberg
Kafka Connect sink 运行时**。它是本地/合成数据的证据环境，**不是生产部署**，未经端到端验证。

## 1. 版本与校验和

| 组件 | 锁定版本 | 校验 | 证据状态 |
| --- | --- | --- | --- |
| Debezium Connect 基础镜像 | `quay.io/debezium/connect:3.7.1.Final` | OCI index digest `sha256:0c64bbe0164f2c189db2e622944cafee1c54d29dbf73d4ca2bdd3c41921e1d7a` | 已核对：Quay API tag 查询 + `docker manifest inspect`（含 amd64/arm64 子 manifest） |
| Debezium 连接器（源码插件集） | `3.7.1.Final` | 镜像自带，见下方插件清单 | 已核对：`debezium/container-images` 的 `connect/3.7/Dockerfile` 中 `DEBEZIUM_VERSION="3.7.1.Final"` |
| Apache Iceberg | `1.12.0` | 源码 tarball SHA512 `505b4d92baf9a2b55c3893bfa1b50289a0bf0051109d81f0a92825f406c28be9096be31d5c22ed68a2d7abd39fcfb0888729bb1917216b06ac200ac0fbf11a28` | 已核对：下载 `apache-iceberg-1.12.0.tar.gz` 后本地计算 SHA512；Iceberg 未发布可直接下载的 runtime 二进制，必须从源码构建 |
| Gradle（wrapper） | `8.14.5` | wrapper 自带 `distributionSha256Sum=6f74b601422d6d6fc4e1f9a1ab6522f642c2fdcbc15ae33ebd30ba3d7198e854` | 已核对：源码树 `gradle/wrapper/gradle-wrapper.properties` |
| JDK 构建镜像 | `eclipse-temurin:21-jdk` | Docker Hub tag digest `sha256:8ad118b146aa92da815b576441acd6b42b38236797ad0b6edc8c200b85bb0734` | 已核对：Docker Hub API（2026-11-20 推送） |
| PostgreSQL JDBC 驱动 | `org.postgresql:postgresql:42.7.7` | SHA256 `157963d60ae66d607e09466e8c0cdf8087e9cb20d0159899ffca96bca2528460` | 已核对：下载 jar 后本地计算；Maven Central 只发布 `.sha1`（`67f8093e8d8104c74bbf588392ac3229803f5d17`，与计算值匹配），`.sha256` 返回 404，因此 SHA256 是本仓库记录的本地计算结果 |

> ⚠️ 已知跨版本风险（未验证，不得当作已解决）：Debezium 3.7.1.Final 的 worker 镜像内 Kafka 为
> **4.3.1**（来源：`debezium/container-images` 的 `kafka/3.7/Dockerfile` `ARG KAFKA_VERSION=4.3.1`，
> 与 Debezium `v3.7.1.Final` 根 `pom.xml` 的 `<version.kafka>4.3.1</version.kafka>` 一致），而
> Apache Iceberg 1.12.0 的 Kafka Connect 模块按 **Kafka 3.x** 构建（`gradle.properties` 中
> `systemProp.defaultKafkaVersions=3`、`knownKafkaVersions=3`）。插件是否能在 Kafka 4.x worker
> 上正常加载/运行 **没有验证**；若失败，见第 5 节 fallback，并且必须在证据里如实记录限制。

## 2. 镜像内容与插件清单

`Dockerfile` 分两阶段：

1. `iceberg-builder`（`eclipse-temurin:21-jdk`，固定 digest）：
   - 下载固定版本的 Iceberg 源码 tarball 并校验 SHA512；
   - 执行 Gradle wrapper 的 `:iceberg-kafka-connect:iceberg-kafka-connect-runtime:distZip`
     （`-x test -x integrationTest`，不跑测试）；
   - 解压分发 zip，收集 `lib/*.jar`；
   - 下载 PostgreSQL JDBC 42.7.7 并校验 SHA256，一并放入插件目录。
2. 运行阶段（`quay.io/debezium/connect:3.7.1.Final`，固定 digest）：
   - 把上一步的 jar 复制到 `/kafka/connect/iceberg/`（属主 `kafka:kafka`）；
   - 设置 `CONNECT_PLUGIN_PATH=/kafka/connect,/kafka/connect/iceberg,/kafka/connect/extra-plugins`。

预期可见的插件（以 `GET /connector-plugins` 为准）：

| 插件类 | 来源 | 用途 |
| --- | --- | --- |
| `io.debezium.connector.postgresql.PostgresConnector` | Debezium 基础镜像 | source：PostgreSQL 逻辑复制 → Kafka |
| `io.debezium.connector.jdbc.JdbcSinkConnector` | Debezium 基础镜像 | **fallback** sink：写关系库（见第 5 节） |
| `org.apache.iceberg.connect.IcebergSinkConnector` | 本镜像构建 | sink：Kafka → Iceberg 表 |
| `org.apache.iceberg.connect.transforms.*`（CopyValue / DebeziumTransform / JsonToMapTransform 等） | Iceberg runtime 分发件 | sink 的路由/转换 SMT |

说明：
- Iceberg sink 的“dynamic routing”通过 `iceberg.tables.dynamic-enabled=true` +
  `iceberg.tables.route-field` 按记录字段值选择表（或按 `route-regex`），**不是**直接按 topic 名
  路由；具体 connector config 由 core 应用生成（design D3），本目录不复制该生成逻辑。
- 基础镜像自带的插件清单以 Debezium 3.7 官方镜像为准；本仓库不逐个复述其完整列表。

## 3. 构建

### 方式 A：compose 构建（推荐）

基础 compose 文件必须排在第一，保证相对路径按 `deploy/compose` 解析：

```bash
cd /Users/klb3713/work/OntoAxis
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime build kafka-connect
```

### 方式 B：直接 docker build

```bash
cd /Users/klb3713/work/OntoAxis
docker build \
  --build-arg DEBEZIUM_IMAGE="quay.io/debezium/connect:3.7.1.Final@sha256:0c64bbe0164f2c189db2e622944cafee1c54d29dbf73d4ca2bdd3c41921e1d7a" \
  -t ontoaxis/connector-runtime-connect:3.7.1-iceberg1.12.0 \
  deploy/connector-runtime/connect
```

构建需要网络（Apache 源码镜像、Maven Central、Debezium 镜像）。首次构建耗时取决于网络与
Gradle 依赖下载，通常明显长于普通镜像构建；构建失败时按第 6 节排查。

### 方式 C：手工构建 Iceberg 分发件后挂载（不重建镜像）

在已安装 JDK 21 的主机上（或临时 Gradle 容器）按 Iceberg 官方文档构建：

```bash
curl -fsSL -o /tmp/apache-iceberg-1.12.0.tar.gz \
  https://downloads.apache.org/iceberg/apache-iceberg-1.12.0/apache-iceberg-1.12.0.tar.gz
echo "505b4d92baf9a2b55c3893bfa1b50289a0bf0051109d81f0a92825f406c28be9096be31d5c22ed68a2d7abd39fcfb0888729bb1917216b06ac200ac0fbf11a28  /tmp/apache-iceberg-1.12.0.tar.gz" | sha512sum -c -
tar -xzf /tmp/apache-iceberg-1.12.0.tar.gz -C /tmp
cd /tmp/apache-iceberg-1.12.0
./gradlew --no-daemon -x test -x integrationTest \
  :iceberg-kafka-connect:iceberg-kafka-connect-runtime:distZip

# 找到分发 zip 并解压到插件的额外目录（compose 中的 ontoaxis_connect_extra_plugins 卷）
ZIP=$(find . -type f -path '*/distributions/*.zip' ! -iname '*hive*' | head -n 1)
test -n "$ZIP"
unzip -q "$ZIP" -d /tmp/iceberg-runtime
# 把 lib/*.jar 与 postgresql-42.7.7.jar 放入正在运行的 Connect 容器（容器名由 compose project 决定）
CID=$(docker compose -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime ps -q kafka-connect)
test -n "$CID"
docker cp /tmp/iceberg-runtime/*/lib/. "$CID":/kafka/connect/extra-plugins/iceberg/
# 新增插件需要重启 Connect 才会被 /connector-plugins 发现
docker compose -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime restart kafka-connect
```

> 该手工路径同样未在本会话执行过；`docker cp` 后的容器重启会保留文件（可写层），但重建容器
> 会丢失，生产/长期环境应使用镜像或持久卷。

## 4. 验证（只读）

```bash
# 1) Connect REST 可达
curl -sS http://127.0.0.1:18089/connector-plugins | python3 -m json.tool

# 2) 必需插件是否出现（脚本化检查）
./deploy/connector-runtime/scripts/verify-connector-runtime.sh

# 3) 已提交的 connector 与状态
curl -sS http://127.0.0.1:18089/connectors | python3 -m json.tool
```

`verify-connector-runtime.sh` 默认只读；只有显式传 `--submit-smoke` 才会提交一个临时 source
connector，并在退出时清理（详见脚本 `--help`）。

## 5. Fallback：Iceberg 插件不可用时的 JDBC sink

如果 Iceberg sink 插件无法加载（构建失败、Kafka 4.x 兼容性问题等），可以使用 Debezium 基础镜像
自带的 **Debezium JDBC sink** 作为 fallback：

- 插件类：`io.debezium.connector.jdbc.JdbcSinkConnector`（镜像内自带）
- 行为：把 topic 记录写入关系数据库表（upsert/append），**不会**产生 Iceberg 表，也**不能**通过
  Trino Iceberg catalog 查询。
- 适用：仅用于验证 source → Kafka → sink 链路本身可跑通；**不能**替代 design D1 的 Iceberg 证据，
  使用时必须在验证报告里明确标注“Iceberg 不可用，采用 JDBC sink fallback，D1 未验证”。

如果连 JDBC sink 也不可用（例如需要自定义转换），再退一步只验证 source connector 与 topic 数据，
并在证据里记录 sink 缺失。

## 6. 排查

| 现象 | 可能原因 | 处理 |
| --- | --- | --- |
| `GET /connector-plugins` 里没有 Iceberg 插件 | 镜像构建失败 / `CONNECT_PLUGIN_PATH` 未包含插件目录 / jar 被放在错误子目录 | `docker compose exec kafka-connect ls -l /kafka/connect/iceberg`；确认 `CONNECT_PLUGIN_PATH` 包含该目录 |
| Connect 容器 unhealthy | 启动慢（JVM + 首次创建内部 topic）或 curl 缺失 | 查看 `docker compose logs kafka-connect`；healthcheck 有 40s `start_period`、30 次重试 |
| 构建阶段 Gradle 任务找不到 | Iceberg 版本升级后项目路径变化 | 本文件 `find` 已兼容产物路径；任务路径需按实际源码 `settings.gradle` 修正 |
| `Unable to load class org.apache.iceberg...` | Iceberg runtime 分发包与 worker Kafka 版本不匹配 | 记录为已知限制并转 fallback；不要伪造通过 |

## 7. 验证状态（诚实记录）

**已核对（静态/网络证据）**

- Quay `3.7.1.Final` tag 的 manifest digest；Docker Hub/Quay 镜像 manifest 存在 amd64/arm64。
- Debezium 3.7.1.Final 对应 Kafka 4.3.1（Dockerfile + pom）。
- Iceberg 1.12.0 源码 tarball 可下载，SHA512 与 Dockerfile 一致；Gradle wrapper 8.14.5。
- PostgreSQL JDBC 42.7.7 jar SHA256/SHA1 交叉核对。
- `docker compose config` 能解析 overlay（见仓库根验证记录），但本会话**没有**构建镜像、
  **没有**启动容器、**没有**调用 `up`。

**未验证（必须在实际执行后补充证据）**

- 镜像完整构建成功（Gradle 任务名、产物 zip 布局、jar 数量）。
- `/connector-plugins` 中实际出现 `org.apache.iceberg.connect.IcebergSinkConnector`。
- Iceberg sink 在 Kafka 4.3.1 worker 上的运行时兼容性。
- source → topic → Iceberg 快照/行数一致性（属于任务 5.3 E2E，本目录只提供资产）。