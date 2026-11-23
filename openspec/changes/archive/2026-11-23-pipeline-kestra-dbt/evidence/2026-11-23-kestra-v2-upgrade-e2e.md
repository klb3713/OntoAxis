# Kestra v2.0.12 升级与真实 E2E 证据

日期：2026-11-23
环境：macOS x86_64、Docker 27.0.3、Compose 2.28.1
变更：`pipeline-kestra-dbt` 的 Kestra 客户端从 v0.24.0 升级到 v2.0.12；不保留旧版本代码/配置。

## 版本与镜像

- Kestra：`kestra/kestra:v2.0.12`，digest `sha256:b521aff2847161c283450d3c224523b42ea97447d86bde9519576c393d914b76`
- 自定义镜像：`deploy/compose/pipeline-kestra/Dockerfile`，`kestra/kestra:v2.0.12` + `dbt-trino==1.8.0`
- 运行时 dbt：`dbt-core 1.12.7`、`dbt-trino 1.8.0`
- Trino catalog：`memory`，派生 schema `oa_t11111111_p01a205e4`

## API spike（v2.0.12，tenant=main）

所有 API 需要 Basic auth：`KESTRA_SERVER_BASIC_AUTH_ENABLED=true` + `KESTRA_SERVER_BASIC_AUTH_USERNAME/PASSWORD`。`/api/v1/configs` 也需要认证。

- Flow upsert：`POST /api/v1/main/flows/{namespace}`，`Content-Type: application/x-yaml`，body 为完整 Flow YAML；已存在同 id 时覆盖为下一 revision。`PUT` 返回 405。
- Flow 查询/删除：`GET`/`DELETE /api/v1/main/flows/{namespace}/{id}`。
- Namespace files：`POST /api/v1/main/namespaces/{namespace}/files?path=<uri>`，multipart 字段 `fileContent`；`DELETE ...?path=<uri>`。v2 将文件平铺到任务工作目录。
- 触发：`POST /api/v1/main/executions/{namespace}/{flowId}?labels=key:value`，无 body。
- 查询：`GET /api/v1/main/executions/{executionId}`，状态在 `state.current`。
- 按 label 搜索：`GET /api/v1/main/executions/search?filters[labels][EQUALS][ontoaxis.run_id]=...&size=N`，响应 `{"results":[...]}`。
- 取消：`DELETE /api/v1/main/executions/{executionId}/actions/kill`。

## 代码变更

- `internal/modules/pipelines/kestra/http_adapter.go`：tenant 前缀、v2 路径/查询/YAML body/文件上传字段、DELETE Flow、取消路径、search filters。
- `internal/modules/pipelines/kestra/flow.go`：dbt 任务使用 `io.kestra.plugin.core.runner.Process`（不依赖 Docker socket）并启用 `namespaceFiles.enabled=true`；命令使用 `--project-dir . --profiles-dir .`。
- `internal/modules/pipelines/dbt/render.go`：Trino profile `port: 8080`（dbt-trino 要求整数）。
- `deploy/compose/pipeline-kestra/{Dockerfile,compose.pipeline.yaml}`：v2.0.12、Basic auth env、healthcheck 带凭据、build context。

## 真实 E2E

启动：`docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml -f deploy/compose/pipeline-kestra/compose.pipeline.yaml up -d --build kestra ontoaxis-core`

- Kestra healthy；core `/readyz` 200。
- Pipeline `e2e-kestra-v2`：`01a205e4-d2a6-741a-a357-fe02457b29db`。
- Publish：`POST /api/v1/pipelines/{id}/publish` → 200，version 6。
- Trigger：`POST /api/v1/pipelines/{id}/runs` → 202，run `01a205ee-efef-77d6-85d5-757de73fda25`。
- Run 状态：`succeeded`（2026-11-23T01:05:05Z）。
- Kestra execution：`3Y0HuuiNvyln2p3QQAPjKS`，整体 `SUCCESS`，`build_orders` task `SUCCESS`（dbt run 成功）。
- Trino 验证：
  - `SHOW TABLES FROM memory.oa_t11111111_p01a205e4` → `build_orders`
  - `SELECT * FROM memory.oa_t11111111_p01a205e4.build_orders` → `1`

## 未覆盖 / 限制

- 未执行 dataset 创建、输出/血缘查询，13.2 未完整覆盖。
- 未执行坏 SQL、Kestra 停机重试、状态回写、幂等异体等负向 E2E（12.3/12.4、13.3–13.6 仍未勾选）。
- 未抓取 Prometheus/trace 证据（13.7 未勾选）。
- 19 个 Kestra 后端 API 与 dbt 实际执行已验证；未验证大表/并发/多租户隔离。

## 13.2 输出与血缘补充（2026-11-23）

- 合成 dataset：`pipeline-e2e-output`，id `01a205f5-6916-7985-9689-c7fb89a179c8`，format `iceberg`。
- Pipeline：`e2e-kestra-output`，id `01a205f5-7a43-7da5-8470-65a5e81c979e`，DAG `outputs[0]={dataset_id, model:build_orders}`。
- 发布 200，触发 run `01a205f5-9217-78c3-99f7-64614bd08168` → `succeeded`。
- `pipeline.run_outputs` 行：`dataset_id=01a205f5-6916-7985-9689-c7fb89a179c8, model=build_orders, relation_catalog=memory, relation_schema=oa_t11111111_p01a205f5, relation_table=build_orders`。
- Trino：`SHOW TABLES FROM memory.oa_t11111111_p01a205f5` → `build_orders`；`SELECT *` → `42`。
- `run_inputs` 为空（DAG 未声明输入）。

## 8.3 dbt 产物保存补充（2026-11-23）

- `flow.go` 的 dbt task 配置 `outputFiles: [target/run_results.json]`。
- 真实 execution `1vT72mqhKuHgfVz5aMSHlG` 的 task run `33A5H65isovH19LPuT095o` 返回：
  `outputFiles: {"target/run_results.json": "kestra:///.../run_results.json"}`，`exitCode: 0`。
- dbt 非零退出时 Kestra task 失败，run 进入 `failed`（见 13.3 坏 SQL 用例）。

## 13.3 坏 SQL 失败路径（2026-11-23）

- Pipeline `e2e-kestra-bad` id `01a205f7-394e-77c2-be95-2d548df17547`，DAG 仅一个 dbt task `bad_orders`，SQL `select * from missing_table_xyz`；publish 200。
- 触发 run `01a205f7-46cf-72b3-9c2a-be22310a45de` → `failed`，`error="pipeline execution failed"`（不泄露 SQL/dbt 原始错误）。
- `pipeline.run_outputs` 对该 pipeline 计数为 0；Kestra 执行 `FAILED`，dbt 非零退出。
- 结论：坏 SQL 不产生输出/血缘，API 返回脱敏通用错误。

## 13.4 发布/触发中断与恢复（2026-11-23）

- 基线：pipeline `e2e-kestra-v2` version 6。
- 停 Kestra 后 publish（`Idempotency-Key: e2e-retry-pub-1`）→ 503 `unavailable`，version 仍 6。
- 启动 Kestra 后同一请求/同一 key 重试 → 200，version 7（只递增一次）。
- 停 Kestra 后 trigger（`Idempotency-Key: e2e-retry-run-1`）→ 503，创建 run `01a205f8-ba2d-7915-85e7-89b211e1e0db` 状态 `failed`，`error="kestra trigger failed"`。
- 启动 Kestra 后同一请求/同一 key 重试 → 202，新 run `01a205f9-6a54-75d5-a9d5-b1b05f91fa5d` → `succeeded`，version 7。
- Kestra 按 `ontoaxis.run_id` 搜索：失败 run 的 execution 数 0；成功 run 的 execution 数 1（无重复执行）。

## 13.5 状态回写（2026-11-23）

- 成功 run `01a205f9-6a54-75d5-a9d5-b1b05f91fa5d`（version 7）：停止 Kestra 后 `GET /api/v1/pipelines/{id}/runs/{run}` 仍返回 `200 succeeded`，`finished_at` 不变。
- 非终态 run `01a205fa-5132-746f-bcae-2e5d668e24c9`（trigger 后立即停止 Kestra）：查询返回 `200 running`，不是 503，也不是伪造 `succeeded`；恢复 Kestra 后 reconciler 将其推进为 `succeeded`。
- 结论：查询路径只读持久化状态；引擎不可达不影响已落库状态。

## 13.6 负向集成用例（2026-11-23）

- 跨租户 404：`go test ./internal/modules/pipelines/api -run TestPipelineCRUDAndErrorContract` PASS；用例内 `tenantB GET tenantA pipeline` 断言 404。
- 非法 DAG：创建 pipeline `01a205fb-b176-7ec5-86e8-8e2317c38e55`（task type `python`）成功，publish → **400** `invalid_argument`。
- 缺失 dataset：pipeline `e2e-missing-dataset`（outputs 引用不存在 UUID）publish → **400** `invalid_argument`。
- 幂等异体：对 `e2e-kestra-v2` 复用 `Idempotency-Key: e2e-retry-pub-1` 但 body `{"published_by":"other"}` → **409** `already_exists`，message `idempotency key reused with different request`。
- 错误体不含 SQL/tenant/内部标识；均带 `request_id`。

## 13.7 遥测（2026-11-23）

- `/metrics` 抓取：`ontoaxis_http_requests_total{method="POST",route="/api/v1/pipelines/{pipelineID}/runs",status="202"} 1`、`ontoaxis_module_ready{module="pipelines"} 1`；无 tenant/凭据/DSN label。
- Jaeger trace `434f0692b02322d0e435b394335d3aa7`（HTTP POST runs，11 spans）包含 `authz.check`、`authz.cedar_eval`、`authz.fga_check` 与 postgres spans；无 tenant/Authorization/URL 凭据属性。
- 原始抓取：`evidence/2026-11-23-metrics-trace.txt`。

## 8.3/8.4 dbt test 与 run_results 对账（2026-11-23 补验）

- `flow.go` 的 dbt 任务现执行 `dbt run --select <model>` 与 `dbt test --select <model>`；中间复制 `target/run_results.run.json`、`target/run_results.test.json`，`outputFiles: target/run_results*.json`。
- 真实执行 `7T6svmCkTcKdh6mvZErDus`（run `01a2061c-1aa5-7a9a-ba90-1f2f4dfb4d1c`，version 5）：任务 `exitCode=0`，outputFiles 同时包含 run/test 产物；`run_results.test.json` 的 `args.which=test`，`run_results.run.json` 的模型结果 `model.ontoaxis_pipeline.build_orders=success`。
- `HTTPAdapter.GetRunResults` 通过 v2 `/outputs/tasks/{executionID}[/{taskRunID}]` + `/executions/{id}/file?path=` 下载产物；`service.reconcileRun` 在成功回写前用 `dbt.ParseRunResults` 校验，失败/缺失不写 dataset 输出，并记录 `pipeline dbt run results` 计数日志（`total=1 success=1 failed=0 skipped=0`）。
- 原始产物：`docs/stages/evidence/stage-2-pipeline-kestra-dbt/raw/kestra-7T6svmCkTcKdh6mvZErDus-run_results*.json`。
