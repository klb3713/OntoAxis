# connector-runtime Profile 证据（2026-11-23）

- 启动组合：`deploy/compose/compose.yaml` + `deploy/compose/authz/compose.authz.yaml` + `deploy/connector-runtime/compose.connector-runtime.yaml`（profile `connector-runtime`）。
- 容器：source-postgres/redpanda/kafka-connect 均 healthy；core `/readyz` 200。
- 插件：19 个，含 Debezium Postgres source 与 Iceberg sink。
- 作业：`01a20491-cc38-774e-b136-91bc48fad7af` 处于 `running`；runtime connectors `oa-bd7662a5-67069195-source|-sink` 均 RUNNING。
- 未完成：SnapshotVerifier（D6 行数+快照双校验）仍为 nil，作业无法进入 `succeeded`；Trino 行数三方断言与 5.3 全量 E2E/负向矩阵未执行。因此 **2.4 未通过**，仅记录 profile 已可启动的真实证据。
