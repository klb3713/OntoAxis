# 依赖版本审计报告

- 生成时间：2026-11-23T09:12:08+0800
- 来源：`deploy/deps/policy.yaml` + `deploy/deps/lock.yaml`
- 说明：上游最新来自 Docker Hub / Quay 公开 API；无法查询记为 unknown，不推测。

| 组件 | 当前 | digest | 上游最新 | 差距 | tier | owner | review_by | EOL | 备注 |
|---|---|---|---|---|---|---|---|---|---|
| alertmanager | `v0.27.0` | 有 | v0.35.0 | 落后 minor | p0 | stage-2-integration | 2027-03-31 |  | 例外 helm-placeholder-digests |
| apicurio | `（无 tag）` | 有 | 3.3.5 | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| debezium | `（无 tag）` | 有 | 3.8 | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| jaeger | `1.62.0` | 有 | 1.76.0 | 落后 minor | p0 | stage-2-integration | 2026-12-31 |  | 例外 helm-placeholder-digests |
| kafka | `（无 tag）` | 有 | 4.4.0 | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| kestra | `v2.0.12` | 有 | v2.0.12 | 一致 | p0 | pipeline-kestra-dbt | 2027-03-31 |  | 例外 helm-placeholder-digests |
| keycloak | `26.0` | 有 | 26.8.2 | 落后 minor | p0 | production-delivery | 2027-01-31 |  | 例外 helm-placeholder-digests |
| meilisearch | `v1.11` | 有 | v1.57.0 | 落后 minor | p0 | object-search-meilisearch | 2027-03-31 |  | 例外 helm-placeholder-digests |
| nessie | `（无 tag）` | 有 | 0.76.6 | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| oa-edge | `v3.3` | 无 | v3.7.19 | 落后 minor | p0 | production-delivery | 2027-03-31 |  |  |
| openfga | `v1.23.0` | 有 | v1.23.0 | 一致 | p0 | stage-2-integration | 2027-03-31 |  | 例外 helm-placeholder-digests |
| postgres | `16-alpine` | 有 | 19.0 | 落后 major | p0 | production-delivery | 2027-03-31 | 2028-11-09 | 例外 helm-placeholder-digests |
| prometheus | `v3.1.0` | 有 | v3.16.0 | 落后 minor | p0 | stage-2-integration | 2027-03-31 |  | 例外 helm-placeholder-digests |
| seaweedfs | `（无 tag）` | 有 | unknown | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| temporal | `1.26.2` | 有 | 1.29.7 | 落后 minor | p0 | action-temporal | 2027-02-28 |  | 例外 helm-placeholder-digests |
| temporal-init | `1.26.2` | 无 | 1.32.2 | 落后 minor | p0 | action-temporal | 2027-02-28 |  |  |
| trino | `484` | 有 | 484 | 一致 | p0 | pipeline-kestra-dbt | 2027-03-31 |  | 例外 helm-placeholder-digests |
| valkey | `（无 tag）` | 有 | 9.2.0 | unknown | p0 | connector-runtime | 2027-03-31 |  | 例外 helm-placeholder-digests |
| alpine | `3.20` | 无 | 3.24.4 | 落后 minor | p1 | production-delivery | 2027-03-31 |  |  |
| buf | `1.75.0` | 无 | 1.75.0 | 一致 | p1 | production-delivery | 2027-03-31 |  |  |
| eclipse-temurin | `21-jdk` | 有 | 21 | 一致 | p1 | connector-runtime | 2027-03-31 |  |  |
| golang | `1.26.3` | 无 | 1.27.4 | 落后 minor | p1 | production-delivery | 2027-03-31 |  |  |
| otel-collector | `0.120.0` | 有 | 0.165.0 | 落后 minor | p1 | stage-2-integration | 2027-03-31 |  | 例外 helm-placeholder-digests |
| perses | `v0.50.0` | 有 | unknown | unknown | p1 | stage-2-integration | 2027-03-31 |  | 例外 helm-placeholder-digests |
| temporal-ui | `2.31.2` | 无 | 2.58.1 | 落后 minor | p1 | action-temporal | 2027-02-28 |  |  |
| authz-init | `8.10.1` | 无 | 8.23.0 | 落后 minor | p2 | stage-2-integration | 2027-03-31 |  |  |
| ontoaxis-core | `（无 tag）` | 有 | unknown | unknown | p2 | stage-2-integration | 2027-03-31 |  | 自有镜像（CI 构建），不查上游; 例外 helm-placeholder-digests |
| ontoaxis-search | `（无 tag）` | 有 | unknown | unknown | p2 | stage-2-integration | 2027-03-31 |  | 自有镜像（CI 构建），不查上游; 例外 helm-placeholder-digests |
| web | `（无 tag）` | 有 | unknown | unknown | p2 | stage-2-integration | 2027-03-31 |  | 自有镜像（CI 构建），不查上游; 例外 helm-placeholder-digests |
