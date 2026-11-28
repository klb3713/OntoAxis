# Wave 1/2 运行时 profile：本地分组启动与资源占用（任务 5.4）

- 日期：2026-11-28
- 环境：macOS 本地 Docker Desktop，Compose；所有服务为本地合成环境，不是生产容量数据。

## 1. 同时启动组：connector + search + authz（E2E-05 栈）

项目 `oa-e2e05`，同时启动：`postgres keycloak openfga authz-init trino meilisearch search-worker ontoaxis-core redpanda kafka-connect source-postgres iceberg-warehouse`。

### 端口映射

| 服务 | 宿主端口 |
| --- | --- |
| ontoaxis-core | 18080→8080、19090→9090 |
| trino | 18081→8080 |
| postgres | 15433→5432 |
| keycloak | 18083→8080 |
| meilisearch | 17700→7700 |
| search-worker | 18088→8081 |
| kafka-connect | 18089→8083 |
| redpanda | 19099→19092（避让既有 19092） |
| source-postgres | 15444→5432（避让既有 15434） |
| openfga | 容器网络内 8081 |

### 资源占用快照（`docker stats --no-stream`）

| 容器 | CPU | 内存 |
| --- | --- | --- |
| kafka-connect | 3.46% | 1.042 GiB |
| keycloak | 2.62% | 553.7 MiB |
| trino | 5.60% | 813.2 MiB |
| redpanda | 1.53% | 202.4 MiB |
| postgres | 1.87% | 54.1 MiB |
| source-postgres | 1.60% | 43.5 MiB |
| ontoaxis-core | 0.09% | 19.3 MiB |
| openfga | 0.05% | 22.1 MiB |
| meilisearch | 0.66% | 13.8 MiB |
| search-worker | 0.12% | 7.3 MiB |

原始输出：`/tmp/e2e05-resources.txt`（本文件即其归档）。

## 2. 同时启动组：pipeline + search（E2E-06 栈）

项目 `oa-e2e06`：`postgres keycloak openfga authz-init trino kestra meilisearch search-worker ontoaxis-core` 全部启动；Kestra 宿主端口 18096（避让既有 18086）。E2E-06 11/11 通过，见 [`pipeline-dbt-7.2.md`](pipeline-dbt-7.2.md)。

## 3. 分组矩阵（真实 CI runner）

- Wave 1：run [45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) 3/3（connector-runtime / pipeline-kestra / action-temporal），见 [`wave1-stack-matrix.md`](wave1-stack-matrix.md)。
- fullstack-e2e：run [45607781575](https://github.com/klb3713/OntoAxis/actions/runs/45607781575) foundation/search-outbox/actions/pipeline 四组全绿。

## 4. 边界

- 本记录只证明本地/CI 合成环境的 profile 分组启动与资源快照，不代表生产容量规划。
- observability profile 单独验证见 [`observability-7.4.md`](observability-7.4.md)；未把所有 16 个服务放入同一 Compose 项目一次性启动。
