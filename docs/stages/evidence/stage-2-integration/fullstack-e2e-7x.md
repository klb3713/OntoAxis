# Stage 2 全栈 E2E（7.1 / 7.2 / 7.3 / 7.4）证据

- 工作流：`.github/workflows/fullstack-e2e.yml`（`workflow_dispatch` 与 `push stage2-int/**`）
- 分支 / head：`stage2-e2e/fullstack-7x` @ `4509052`（7.1/7.2/7.3/7.4）；`stage2-e2e/e2e08-chain`（E2E-08 组合链）
- 绿灯运行：[run 45626955337](https://github.com/klb3713/OntoAxis/actions/runs/45626955337)（7.1/7.3/7.4，2026-11-25）；[run 45644525408](https://github.com/klb3713/OntoAxis/actions/runs/45644525408)（新增 `e2e-chain`，8/8 执行 job 全绿，2026-11-25 15:2x UTC）；复核 [run 45648284630](https://github.com/klb3713/OntoAxis/actions/runs/45648284630)（head `3c190d6`，8/8 绿、chain 16-0，2026-11-25 15:50 UTC）
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
| `e2e-chain` | success | 见 run 45644525408 | **E2E-08 组合链 16/16**（见 §6） |
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
| 7.1 | **满足** | E2E-01 全量 16/16（run 45626955337）；E2E-08 组合链 16/16（run 45644525408，见 §6） |
| 7.2 | 部分 | E2E-04/E2E-07 覆盖；E2E-05 opt-in 未跑；E2E-06 仅验证 Kestra/Trino 启动，dbt 产 dataset 未覆盖 |
| 7.3 | **满足** | E2E-02 矩阵 + E2E-03 allow/deny/跨租户/暂停 503（OSM 隔离 E2E 73/73 + `authz-integration`）+ E2E-12 安全负路径均通过 |
| 7.4 | 部分 | trace 贯通、RED 指标、Perses 面板导入已通过；SLO 面板截图、告警触发、负载基线复跑缺失（R12 partial） |

## 5. 过程中修复

- **D-08**：`deploy/compose/compose.yaml:55` 把 web 发布到容器端口 `80`，但 `web/nginx.conf:2` 监听 `8080`、`web/Dockerfile` `EXPOSE 8080`，宿主机 18082 永远不可达 → 改为 `"${ONTOAXIS_WEB_HOST_PORT:-18082}:8080"`（待 production-delivery 复核）。
- `scripts/workbench-e2e.sh:36-40` `wait_code()` 同一 `local` 声明内引用 `timeout` 导致空值、算术失败：改为逐行 `local`。
- 首次 run 45619759777 因 GitHub Actions 计费（`recent account payments have failed or your spending limit needs to be increased`）0 step 失败，非代码问题；重跑后恢复。

## 6. E2E-08（`e2e-chain`，16 passed / 0 failed，run 45644525408 / 45648284630）

脚本 `scripts/fullstack-chain-e2e.sh`，组合 base+authz+action-temporal+search+pipeline-kestra 五个 overlay，仅隔离 Compose + 合成数据，结束 `down -v` 清理。

断言（按链路顺序）：`PASS core /healthz (200)`、`PASS core /readyz (200)`、`PASS keycloak realm discovery (200)`、`PASS object created (ok)`、`PASS action execution succeeded (succeeded)`、`PASS object mutated by action (chain-after-<run>)`、`PASS search finds mutated object (yes)`、`PASS dataset created (ok)`、`PASS dataset main branch present (ok)`、`PASS dataset view created (201)`、`PASS pipeline created (ok)`、`PASS pipeline published (200)`、`PASS pipeline run triggered (ok)`、`PASS pipeline run reached terminal state (not failed) (ok)`、`PASS search remains consistent after chain (yes)`、`PASS audit metrics increased (ok)`。

即 S2-AC-01 的“一条链状态一致、无孤儿数据”在真实 runner 上得到正向证据：对象创建 → action 更新并落库 → 搜索索引收敛 → dataset/branch/view → pipeline dbt 产出 → 搜索复核与审计指标增量。8 个执行 job（foundation/workbench/search-outbox/actions/pipeline/identity/observability/chain）全绿，`e2e-connector` 按 opt-in 跳过。

### 本轮修复与遗留

- **D-09**（已修复，待 production-delivery 复核）：Kestra v2 OSS 要求 Basic Auth 用户名为合法邮箱、口令为明文且 ≥8 位含大小写与数字。原 `deploy/compose/pipeline-kestra/compose.pipeline.yaml` 用 `admin`/`admin123` 导致 401，core `publish` 返回 503；改为 `admin@ontoaxis.local` / `Admin1234`（compose 直接传明文，healthcheck 与 core token 同源）后 `publish 200`、run 成功。
- **D-10**（建议加固，未改产品代码）：DAG `tasks[].model` 必须与 `tasks[].id` 一致；渲染器以任务 id 命名 dbt 模型文件、Flow 以 `model` 做 `dbt run --select`，两者不一致时 dbt “Nothing to do” 退出 0、`run_results` 为空，pipeline run 被判 `dbt_results_failed`。建议在 `domain.ValidateDAG` 强制 `model == id`，避免静默空跑。归属 `production-delivery`/pipelines 后续。
