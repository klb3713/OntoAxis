# connector-runtime 本地 Connect 证据（2026-11-23）

## 环境
- Docker 27.0.3；Compose 2.28.1；宿主 macOS x86_64
- 启动：`docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime up -d --build`
- 镜像：`ontoaxis/connector-runtime-connect:3.7.1-iceberg1.12.0`（Debezium Connect 3.7.1.Final digest `sha256:0c64bbe...` + Iceberg 1.12.0）

## 已修复的实际阻塞
1. Debezium 官方镜像入口脚本读取无前缀变量 `BOOTSTRAP_SERVERS` / `CONFIG_STORAGE_TOPIC` / `OFFSET_STORAGE_TOPIC` / `STATUS_STORAGE_TOPIC` / `GROUP_ID` / `REST_PORT` / `ADVERTISED_HOST_NAME` / `ADVERTISED_PORT` / `KEY_CONVERTER` / `VALUE_CONVERTER`；已改 overlay。
2. Core 需要 `ONTOAXIS_CONNECT_URL`、`ONTOAXIS_ICEBERG_CATALOG_URI/WAREHOUSE/NAMESPACE/USER`、`ONTOAXIS_SECRET_SOURCEPG`；已在 overlay 注入。
3. `connectors.Module.SetSyncRuntime` 只注册 preflight，未注册 `SyncStarter`；新增 `SetSyncStarter` 并在 `internal/app/runtime.go` 的 PostMigrate 中把 ingestion service 作为 starter 注入，`StartSync` 现在创建 job。
4. `ingestion.Module.SetConnectorReader` 只存字段未写回 service option，导致 API 报 `ingestion dependencies are not configured`；已追加 `service.WithConnectorReader` 并重建 service。
5. Iceberg 1.12 不再支持 `iceberg.tables.route-regex/route-replace`，改为显式 `iceberg.tables` + `dynamic-enabled=false`；源/sink 的 JSON converter 显式 `schemas.enable=false`。
6. 插件隔离类加载缺少 `commons-configuration2`（及 `commons-lang3`、`commons-text`），已在 `connect/Dockerfile` 补入插件目录。

## 验证结果
- `docker compose ... config -q` 通过；`deploy/compose/compose.yaml` 未修改。
- `source-postgres` / `redpanda` / `kafka-connect` 均 healthy；core `/readyz` 200。
- `GET /connector-plugins`（容器内 8083）：19 个插件，含 `io.debezium.connector.postgresql.PostgresConnector`、`org.apache.iceberg.connect.IcebergSinkConnector`。
- Dataset `01a20488-fa82-7cc2-848e-c2fb8aee97f9`；Connector `01a20491-cc15-7100-875a-285075ed16e0`；`POST /connectors/{id}/sync` 返回 job `01a20491-cc38-774e-b136-91bc48fad7af`，状态 `running`。
- 运行时连接器 `oa-bd7662a5-67069195-source` 与 `-sink`：connector/task 均 `RUNNING`；source snapshot 完成，sink 消费正常。

## 未完成/限制
- 作业尚未进入 `succeeded`：`internal/app/runtime.go` 仍向 ingestion runtime 传 `nil` SnapshotVerifier；终态需要按 D6 实现源/sink 行数与 Iceberg snapshot 双重校验。
- 未执行 Trino `SELECT count(*)` 行数三方一致断言；5.3 全量 E2E 脚本与负向矩阵仍未完成。
- 本次只证明真实 Debezium + Iceberg Connect 链路可启动并保持 RUNNING，不等同于 S2-AC-06 完成。

## Trino 查询尝试（未通过）
- 已安装 `deploy/connector-runtime/trino/plugins/postgresql-42.7.7.jar` 并用 `compose.trino-iceberg.yaml` 启动 Trino，`SHOW CATALOGS` 可见 `iceberg`。
- `SHOW SCHEMAS FROM iceberg` / `SHOW TABLES` 报 `Cannot check and eventually update SQL schema`；说明 Trino JDBC Iceberg catalog 与 Connect 使用的 `iceberg_catalog` schema 版本/初始化仍不一致，行数三方断言未完成。
- 结论：connector-runtime 真实链路已 RUNNING，但 S2-AC-06 仍未完成，不能勾选 5.3/6.2。

## Trino JDBC catalog 修复记录
- 根因：Kafka Connect 的 Iceberg JDBC catalog 将 `iceberg_tables`/`iceberg_namespace_properties` 写入 Postgres `public` schema，而 `trino/iceberg.properties` 使用 `currentSchema=iceberg_catalog`，导致 `Cannot check and eventually update SQL schema`。
- 修复：`deploy/connector-runtime/trino/iceberg.properties` 的 connection-url 改为 `...?currentSchema=public`。
- 修复后验证：`SHOW SCHEMAS FROM iceberg` 返回 `information_schema`/`public`/`system`；`SHOW TABLES FROM iceberg.public` 可执行（本次测试栈已停止，未再写新表）。
- 说明：行数三方断言仍需在重新启动完整 connector 栈并完成一次 snapshot 后执行；5.3/6.2 仍为未完成。

## Iceberg sink 环境阻塞（Java 25 / Hadoop FileIO）
- 补入 `hadoop-client-api`/`hadoop-client-runtime` 3.4.2 后，sink 从 `NoClassDefFoundError: org/apache/hadoop/util/PlatformName` 变为：
  `UnsupportedOperationException: getSubject is not supported`（`javax.security.auth.Subject.getSubject`，Hadoop `UserGroupInformation.getCurrentUser`）。
- 根因：Debezium Connect 运行镜像使用 **Java 25**，Java 24+ 已移除 Security Manager/`Subject.getSubject` 支持；尝试 `KAFKA_OPTS=-Djava.security.manager=allow` 直接启动失败：`Enabling a Security Manager is not supported`。
- 结论：本地 connector profile 的 Iceberg sink 需要 Java 17/21 运行时（或改用不依赖 Hadoop FileIO 的 FileIO），当前 pinned Debezium 镜像不满足；S2-AC-06 真实验收仍未完成，5.3/6.2 不勾选。

## Java 21 运行时尝试（未采用）
- 尝试把运行阶段改为 `eclipse-temurin:21-jre` 并复制 Debezium `/kafka` 与 `/docker-entrypoint.sh`，构建成功，但 Debezium 入口脚本按 RHEL 路径编写（`/etc/redhat-release`、`/config.orig` 等），在 Ubuntu/Temurin 基础镜像上启动失败（`cat: /etc/redhat-release: No such file or directory`、`cp: cannot stat '/config.orig/*'`）。
- 已回退运行阶段为原 pinned `quay.io/debezium/connect:3.7.1.Final` 镜像，并重建恢复镜像；该尝试不改变已记录结论：本地 E2E 仍被 Java 25 / Hadoop FileIO 阻塞。

## Debezium 3.6.1（Java 21）验证
- 将基础镜像从 `quay.io/debezium/connect:3.7.1.Final`（Java 25）切换为 `3.6.1.Final`（Java 21），Iceberg sink task 不再因 `Subject.getSubject` 失败。
- 真实结果：source connector `RUNNING`；sink connector/task `RUNNING`；Kafka topic `oa.<tenant>.<conn>.public.customers` 中确认有 Debezium snapshot 记录（customers 5 行）。
- Trino `SHOW SCHEMAS FROM iceberg` / `SHOW TABLES FROM iceberg.public` 可看到 `customers`，但 Iceberg metadata 仍为 `snapshots: []`、`current-snapshot-id: -1`，即 sink 尚未提交数据文件；job 保持 `running`。
- 已尝试把 sink `iceberg.control.commit.interval-ms` 降为 10000 并 PUT 更新 connector config，仍无 snapshot 提交。
- 结论：Java 25 阻塞已解除，但 Iceberg sink 的 commit/control 链路未完成；5.3/6.2 仍不能勾选。

## 清理旧 slot 后的 sink 路由观察
- 清理 `pg_replication_slots` 与旧 connectors 后重建：source/sink task 均 `RUNNING`。
- Kafka consumer group `connect-oa-<tenant>-<conn>.sink` 为 Stable，但 `TOTAL-LAG=5` 未下降；Kafka topic 已有 5 条 Debezium 记录。
- Iceberg metadata 只有 `00000-*.metadata.json`，`snapshots: []`；推断静态 `iceberg.tables=public.customers` 无法把带前缀 topic `oa.<tenant>.<conn>.public.customers` 路由到目标表，sink 建表但不消费数据。
- 已在 `BuildSinkConfig` 增加 `iceberg.control.commit.interval-ms=10000` 并重建 core，仍无 snapshot。5.3/6.2 未通过。

## RegexRouter SMT 尝试
- 在 `BuildSinkConfig` 增加 `transforms=route` + `RegexRouter`，把 `oa.<tenant>.<conn>.public.customers` 重命名为 `public.customers`，并同步 golden test。
- 重建 core 后新建 Job；sink consumer group Stable 但 `TOTAL-LAG=5` 仍不下降，Iceberg metadata 仍只有 `00000-*.json`、`snapshots=[]`。
- 说明问题不只是 topic 名称映射；sink task 虽 RUNNING，但未实际 poll/commit。该路径仍未通过，5.3/6.2 保持未勾选。

## dynamic table 路由试验
- 去掉静态 `iceberg.tables`、设置 `iceberg.tables.dynamic-enabled=true` 后，sink task 立即失败：
  `ConfigException: Must specify a route field if using dynamic table names`。
- 说明 1.12.0 的动态表路由需要 `iceberg.tables.route-field`，不能仅靠主题前缀；静态路由下 consumer group `Stable` 但 lag 不下降。SMT RegexRouter 也未解决消费停滞，已回退以避免保留未验证配置。
- 当前 blocker：Kafka Connect Iceberg 1.12 sink 在本环境未实际 poll/submit。5.3/6.2 未通过。

## 隔离控制 topic/group 试验
- 为 sink 增加独立 `iceberg.control.topic=control-iceberg-<connector8>`、`iceberg.control.group-id-prefix=<sink>`、`iceberg.control.commit.interval-ms=10000` 后重建 core 并新建 Job。
- 结果：sink task 线程确实在 `WorkerSinkTask.pollConsumer`，consumer group Stable 但 `TOTAL-LAG=5`；`control-iceberg-<connector8>` 中可见 `StartCommit` 与 `CommitComplete` 事件，但 `public.iceberg_tables.metadata_location` 仍指向 `00000-*.json`，`snapshots=[]`。
- 说明 commit 事件与 catalog/metadata 更新未对齐，仍非有效提交。相关实验性配置已回退，`sink.go`/`sink_test.go` 回到基线，单测通过。
- 5.3/6.2 保持未通过。
