# Integration patch: pipeline-kestra-dbt

> 状态：实现方交付 patch；`internal/app/`、`cmd/`、`deploy/compose/`、`internal/platform/config` 由 `stage-2-integration` / `production-delivery` 合并。本文件不修改共享文件。

## 1. app 接线

`internal/app/modules.go` 当前：

```go
pipelinesModule := pipelines.New(pool,
    pipelines.WithTenantResolver(requestTenantResolver{}),
    pipelines.WithDatasetResolver(datasetLookup{module: datasetsModule}),
)
```

改为：

```go
runtimeCfg, err := pipelines.LoadRuntimeConfigFromEnv()
if err != nil { return nil, fmt.Errorf("pipeline runtime config: %w", err) }
pipelinesModule := pipelines.New(pool,
    pipelines.WithTenantResolver(requestTenantResolver{}),
    pipelines.WithDatasetResolver(datasetLookup{module: datasetsModule}),
    pipelines.WithRuntimeConfig(runtimeCfg),
)
```

- `ONTOAXIS_KESTRA_URL` 为空时模块仍启动，publish/trigger 返回 503，不创建版本/运行记录。
- 非空时 `pipelines.New` 构造 `kestra.HTTPAdapter`；构造失败由 `Module.Ready` 返回错误，进程不进入 ready。
- 环境变量：`ONTOAXIS_KESTRA_URL`、`ONTOAXIS_KESTRA_TIMEOUT`、`ONTOAXIS_KESTRA_API_TOKEN`、`ONTOAXIS_PIPELINE_DBT_CATALOG`、`ONTOAXIS_PIPELINE_DBT_IMAGE`、`ONTOAXIS_PIPELINE_NAMESPACE_PREFIX`、`ONTOAXIS_PIPELINE_RECONCILE_THROTTLE`、`ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT`、`ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS`、`ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL`、`ONTOAXIS_ENVIRONMENT`。
- 建议后续把这些字段纳入 `internal/platform/config`（当前由模块自行读取，便于不修改共享文件）。

## 2. 迁移

`pipeline` 模块 `Migrate` 现在按文件名顺序应用 `00001_pipeline_init.sql`、`00002_pipeline_execution.sql`。`00002` 新增：

- `pipeline.runs.execution_id/failure_code/attempt_count/last_reconciled_at`
- `pipeline.run_inputs`、`pipeline.run_outputs`
- `pipeline.idempotency_keys`（本期仅建表，服务层幂等未接线）
- 对应 RLS 策略与索引

集成环境需确认 `app.tenant_id` / `app.is_platform_admin` GUC 由连接层设置；否则新表 RLS 会拒绝写入。

## 3. 部署

`deploy/compose/` 与 `deploy/` 未修改。需要 `production-delivery` 增加 `pipeline` profile：

- 固定版本 Kestra（建议 0.24.x 或经 spike 确认的 1.x），端口 18086
- 自定义 dbt 镜像（dbt-trino），不使用 Docker socket
- Trino `memory` catalog（`ONTOAXIS_PIPELINE_DBT_CATALOG=memory`）
- Kestra 基础 URL 与 API token 注入；token 仅进 secret，不进镜像/日志
- healthcheck 与资源限制

本 change 的 D13 证据未生成，compose fragment 未提供；合并前必须完成 spike 与 E2E。

## 4. 已知限制（不得当作已完成）

- 未对真实 Kestra 版本做 spike；HTTP 端点、namespace files 路径、执行按 label 查询、outputFiles 下载均未对真实实例验证。
- 无 E2E：未启动 Kestra/Trino/dbt，S2-AC-04 未证明。
- 幂等中间件未实现（仅表结构）。
- `run_inputs`/`run_outputs` 写回与 `succeeded` 不是同一个数据库事务；输出写回失败时运行保持 `running` 并带 `output_writeback_failed`，由后续对账重试。
- 发布补偿仅删除新 namespace 文件；未回滚 Flow 到上一版本。
- 未实现 `execution_not_found` 有界重试、legacy 24h 运行收敛、指标/span 与幂等键。
- `outputs[]` dataset 存在性/同租户校验未接入（只校验 `dataset_ids`/`inputs[]`）。
- Flow 注册使用 `FlowYAML`（有值）或 JSON DAG（回退）；HTTPAdapter 端点契约仍需真实 Kestra 验证。
- `object-search` 等并行 change 未涉及。

## platform config fields (task 4.1)
- `internal/platform/config` 新增 `KestraURL/KestraTimeout/KestraAPIToken/PipelineDBTCatalog/PipelineDBTImage/PipelineNamespacePrefix/PipelineReconcileThrottle/PipelineRunRecoveryTimeout/PipelineRetryMaxAttempts/PipelineIdempotencyTTL`，默认 30s/2s/2m/3/24h；`Load` 校验正值；单测覆盖。
- 集成 owner 可将这些字段传入 `pipelines.WithRuntimeConfig`（当前模块自身仍读同名 env）。

## outbound metrics (task 11.1)
- `kestra.HTTPConfig.Metrics`/`HTTPAdapter` records `ontoaxis_outbound_request_duration_seconds{dependency="kestra",operation,result}` for flows.upsert/files.publish/files.delete/executions.trigger/executions.get/executions.cancel。
- `pipelines.RuntimeConfig.Metrics` 透传；集成 owner 在 `BuildRuntimeAssembly` 把 observability.Metrics 填入 pipeline runtime config。
