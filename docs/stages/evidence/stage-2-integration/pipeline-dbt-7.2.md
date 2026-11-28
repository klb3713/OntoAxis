# E2E-06：Kestra + dbt → dataset（任务 7.2）

- 日期：2026-11-28
- 环境：本地 Compose（孤立项目 `oa-e2e06`，`ONTOAXIS_KESTRA_HOST_PORT=18096` 避开既有 connector 栈的 18086）
- 脚本：`scripts/pipeline-dbt-e2e.sh`
- 结果：**11 passed / 0 failed**，exit 0
- 日志：[`pipeline-dbt-7.2.log`](pipeline-dbt-7.2.log)

## 断言（全部通过）

1. `core /healthz` 200、`core /readyz` 200、Keycloak realm discovery 200
2. 创建 dataset `e2e06_ds_<RUN_ID>`（format `iceberg`）
3. 创建含 dbt 任务的 pipeline（`type=dbt`、`model=pipeline_dbt_model`、`materialized=table`、`sql=select 42 as answer`，outputs 指向 dataset）
4. `POST /api/v1/pipelines/{id}/publish` 200
5. `POST /api/v1/pipelines/{id}/runs` 触发成功
6. run 终态 `succeeded`（core 解析 dbt `run_results`，全部通过才成功）
7. Trino `SHOW CATALOGS` 含 `memory`
8. `SHOW SCHEMAS FROM memory` 出现 dbt 派生 schema `oa_*_p01a22277`
9. `SELECT answer FROM memory."<schema>".pipeline_dbt_model` 返回 `[[42]]`

## 证据边界

- 本地 Compose + 合成数据；**不是生产验收**。
- 本机 Docker 构建 `ontoaxis-core` 镜像较慢，本次使用同源码已构建镜像（`oa-obs-e2e-ontoaxis-core`）复用到 `oa-e2e06`；CI `e2e-pipeline-dbt` job 会从源码重建，待真实 runner 跑绿后另行记录 run URL。
- Kestra Basic Auth 使用本地合成凭据（`admin@ontoaxis.local`），未使用真实凭据。
