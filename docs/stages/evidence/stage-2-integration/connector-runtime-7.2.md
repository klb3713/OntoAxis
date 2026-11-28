# E2E-05：CDC → dataset → 查询（任务 7.2）

- 日期：2026-11-28
- 环境：本地 Compose（孤立项目 `oa-e2e05`；base + search + authz + connector-runtime + `compose.trino-iceberg.yaml`）
- 脚本：`scripts/connector-runtime-e2e.sh --with-success`
- 结果：**23 passed / 0 failed**，exit 0
- 日志：[`connector-runtime-7.2.log`](connector-runtime-7.2.log)

## 1. 栈与端口

| 服务 | 宿主端口 |
| --- | --- |
| ontoaxis-core | 18080→8080、19090→9090 |
| trino（含 iceberg catalog） | 18081→8080 |
| postgres | 15433→5432 |
| keycloak | 18083→8080 |
| meilisearch | 17700→7700 |
| search-worker | 18088→8081 |
| kafka-connect | 18089→8083 |
| redpanda | 19099→19092（避让既有 19092） |
| source-postgres | 15444→5432（避让既有 15434） |

Trino 通过 `deploy/connector-runtime/compose.trino-iceberg.yaml` 挂载 `iceberg.properties` 与 PostgreSQL JDBC 驱动，并在核心 postgres 预建 `iceberg_catalog` schema。

## 2. 断言

- 前置：core `/readyz` 200、Kafka Connect REST 200。
- 负向矩阵 16/16：未知 connector 404、未知同步配置键 400、暂停/恢复、错误态禁止同步、坏 host 提交失败且文案脱敏、重复同步幂等、运行中取消终态 `cancelled`、伪造租户头 403。
- 成功路径 5/5：
  - `POST /api/v1/connectors/{id}/sync` → 201
  - job 终态 `succeeded`（`01a2228f-dfa2-71ca-99d1-505df1804b42`）
  - `verified_rows` = 5（来自 `ingestion.sync_runs.stats`）
  - Trino `select count(*) from iceberg.ds_01a2228f_public.customers` = 5
  - source-postgres `public.customers` = 5，三方一致
  - dataset `01a2228f-df40-71c8-b5e1-096b8294109c`，runtime_ref `oa-bd7662a5-32655ac7-sink`

## 3. 过程中发现的环境问题（已处理，非产品失败）

- 首次运行只做负向矩阵时，Trino 栈没有 iceberg catalog，`select from iceberg...` 失败；按 `deploy/connector-runtime/trino/README.md` 叠加 `compose.trino-iceberg.yaml` 并预建 `iceberg_catalog` schema 后，成功路径 5/5 通过。
- 两次非干净重跑中，Kafka Connect 里残留的前序运行 connector 触发 rebalance / sink 重放，sink 表出现 10 行（source 5 行）；清空所有残留 connector 后重跑，23/23 通过。建议后续把 `cleanup_inactive_slots` 扩展到运行态孤儿 connector（测试卫生，不影响本次 Stage 2 结论）。

## 4. 证据边界

- 本地 Compose + 合成数据；不接触真实数据/生产凭据。
- CI `e2e-connector` 因 Iceberg 插件扫描 >45 分钟仍为 opt-in（默认 skipped）；本证据为本地补证，CI runner 如需全绿可另行开启 `vars.ENABLE_CONNECTOR_E2E=true`。
