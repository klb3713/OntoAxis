# Stage 2 全栈 E2E（7.1 / 7.2 / 7.3 / 7.4）证据

- 工作流：`.github/workflows/fullstack-e2e.yml`（`workflow_dispatch` 与 `push stage2-int/**`）
- 分支 / head：`stage2-e2e/fullstack-7x` @ `4509052`
- 绿灯运行：[run 45626955337](https://github.com/klb3713/OntoAxis/actions/runs/45626955337)（2026-11-25 13:44–13:51 UTC）
- 环境：GitHub Actions `ubuntu-latest`（真实 runner，公共镜像可拉取）

## 1. Job 结果

| job | 结论 | job id | 覆盖 |
|---|---|---|---|
| `e2e-foundation` | success | 139249580000 | E2E-01 基础 health/readyz |
| `e2e-workbench` | success | 139249580609 | **E2E-01 全量 16/16**（见 §2） |
| `e2e-search-outbox` | success | 139249580581 | E2E-04 outbox→Meilisearch→搜索/重建（隔离 Compose 73/73） |
| `e2e-actions` | success | 139249580503 | E2E-07 Temporal 重试/补偿/审批（7 断言） |
| `e2e-pipeline` | success | 139249580394 | Kestra 307 + Health 日志 + Trino `/v1/info`（E2E-06 部分） |
| `e2e-identity` | success | 139249580233 | **E2E-02/03/12 edge 矩阵 12/12**（见 §3） |
| `e2e-observability` | success | 139249580213 | E2E-09 部分：1 metric series、1 trace、Perses 面板导入 |
| `e2e-connector` | skipped | 139249583010 | E2E-05 opt-in（`vars.ENABLE_CONNECTOR_E2E=true`；Iceberg 插件扫描 >45 分钟） |

## 2. E2E-01（`e2e-workbench`，16 passed / 0 failed）

`PASS core /healthz (200)`、`PASS core /readyz (200)`、`PASS web entry (200)`、`PASS keycloak realm discovery (200)`、`PASS core /metrics (200)`、`PASS metrics exposes ontoaxis_http_requests_total (ok)`、`PASS web serves SPA shell (ok)`、`PASS unknown route 404 (404)`、`PASS Keycloak password grant (200)`、`PASS issued JWT well-formed (ok)`、`PASS create object type (201)`、`PASS object type has id (ok)`、`PASS create object (201)`、`PASS get object (200)`、`PASS patch object (200)`、`PASS delete object (204)`。

脚本：`scripts/workbench-e2e.sh`。只使用隔离 Compose 栈与合成数据，结束清理。

## 3. E2E-02/03/12（`e2e-identity`，12 passed / 0 failed）

`PASS public /readyz (200)`、`PASS internal /metrics hidden (404)`、`PASS unknown route 404 (404)`、`PASS missing JWT 401`、`PASS bogus JWT 401`、`PASS valid JWT 200`、`PASS forged identity header 403`、`PASS public unsupported method 405`、`PASS protected unsupported method 405`、`PASS rate limit returns 429 (68 of 200)`、`PASS auth backend down fail-closed 5xx (500)`。

以 `docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml --profile edge` + `EDGE_TEST_FAIL_CLOSED=1` + `deploy/edge/kind/verify.sh` 执行。

## 4. 覆盖判据与缺口

| 任务 | 状态 | 说明 |
|---|---|---|
| 7.1 | 部分 | E2E-01 已全量；E2E-08（对象→action→pipeline→dataset→查询/搜索组合链）未覆盖，仍不勾选 |
| 7.2 | 部分 | E2E-04/E2E-07 覆盖；E2E-05 opt-in 未跑；E2E-06 仅验证 Kestra/Trino 启动，dbt 产 dataset 未覆盖 |
| 7.3 | **满足** | E2E-02 矩阵 + E2E-03 allow/deny/跨租户/暂停 503（OSM 隔离 E2E 73/73 + `authz-integration`）+ E2E-12 安全负路径均通过 |
| 7.4 | 部分 | trace 贯通、RED 指标、Perses 面板导入已通过；SLO 面板截图、告警触发、负载基线复跑缺失（R12 partial） |

## 5. 过程中修复

- **D-08**：`deploy/compose/compose.yaml:55` 把 web 发布到容器端口 `80`，但 `web/nginx.conf:2` 监听 `8080`、`web/Dockerfile` `EXPOSE 8080`，宿主机 18082 永远不可达 → 改为 `"${ONTOAXIS_WEB_HOST_PORT:-18082}:8080"`（待 production-delivery 复核）。
- `scripts/workbench-e2e.sh:36-40` `wait_code()` 同一 `local` 声明内引用 `timeout` 导致空值、算术失败：改为逐行 `local`。
- 首次 run 45619759777 因 GitHub Actions 计费（`recent account payments have failed or your spending limit needs to be increased`）0 step 失败，非代码问题；重跑后恢复。
