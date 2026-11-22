# pipeline-kestra-dbt 单元/契约验证证据

> 日期：2026-11-22。范围：本仓库实现与单元/契约测试，不含真实 Kestra/Trino/dbt E2E。

## 环境

- macOS x86_64，Docker 27.0.3（本次未启动服务）
- Go 1.26.3：`/tmp/go/bin/go`
- `GOFLAGS=-buildvcs=false`，`GOMODCACHE=/tmp/gomod`
- Kestra 镜像版本：无（未 spike，见限制）

## 命令与结果

```bash
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...
# exit 0

GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./internal/modules/pipelines/...
# ok: pipelines, api, dbt, domain, kestra, migrations, repo, service

GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./...
# 全仓通过（见最终命令输出）
```

## 覆盖（新增）

- DAG v1：`domain/dag_test.go` — 解析宽松/默认版本/未知字段/非法版本；发布校验六类稳定错误码、确定性环检测、无任务兼容。
- Flow：`kestra/flow_test.go` — namespace/flow id 派生、6 个 label、拓扑序、no-op、golden YAML、哈希稳定、无凭据。
- HTTP adapter：`kestra/http_adapter_test.go` — token/traceparent、Flow upsert、multipart 文件、删除、触发、执行查询、按 run label 查找、取消、4xx/5xx/超时分类、重试边界、脱敏、响应丢失接管。
- dbt：`dbt/render_test.go` — 项目文件确定性、schema、profiles 仅 env_var、3-part SQL 拒绝、run_results 解析。
- 迁移：`migrations/migrations_test.go` — 00002 覆盖、文件名排序、重复应用幂等、DownStatements 顺序。
- 服务：`service/runtime_test.go` — 发布渲染并上传/注册、触发写 execution_id、对账成功写 outputs、未配置 adapter fail-closed。
- repo：`repo/*_test.go` — 新字段持久化、CAS 终态不回退、跨租户 404、输出/血缘幂等。

## 限制

- 未启动 Kestra/Trino/dbt；无真实 E2E、S2-AC-04 未证明。
- Kestra 端点、namespace files 路径、执行按 label 查询、outputFiles 未对真实实例验证。
- 幂等键、指标、legacy run、`execution_not_found`、发布 Flow 回滚未实现。
- 部署 compose/RUL 未合并；`ONTOAXIS_KESTRA_URL` 未配置时 publish/trigger 503 为预期。

## task 4.1 platform config
- `internal/platform/config` 新增 pipeline/Kestra 字段与校验；`go test ./internal/platform/config/` ok。

## task 11.1 outbound metrics
- HTTP adapter records duration/result by operation; `TestHTTPAdapterRecordsOutboundMetrics` gathers registry and asserts kestra/executions.get/success sample count 1.

## task 11.2 reconcile metric
- `service.WithMetrics` + `Manager.metrics`; `reconcileRun` increments `ontoaxis_jobs_total{type="pipeline_run_reconcile",result="success|failed"}` only for actual reconcile attempts.
- `TestReconcileRecordsJobsTotalMetric` gathers registry and asserts counter >=1; no tenant label.

## task 11.3 redaction
- `http_adapter_test.go` covers Authorization token never in errors and header only sent to Kestra (`TestHTTPAdapterSendsBearerTokenAndTraceparent`, redaction test); adapter errors are redacted and no request body/SQL logged; full logger scan clean.

## task 10.1 Idempotency-Key 解析/校验
- `api.parseIdempotencyKey`：仅 publish/trigger 读取；缺省保持旧语义；长度 >255 或非可见 ASCII → 400 `invalid_argument`；不改写键值。
- `TestIdempotencyKeyValidation` 覆盖过长键与含空格键。
- 注：10.2/10.3/10.4（持久化重放、并发、引擎调用次数）尚未实现。

## task 10.2–10.4 幂等记录/清理/调用次数
- 10.2：`IdempotencyRecord` + `Repository.LookupIdempotency/SaveIdempotency`；handler 同键同体重放首次状态/body，同键异体 409 `already_exists`；进程内 `idempotencyLocks` 串行化同键，Postgres `ON CONFLICT DO NOTHING` 作唯一约束后备。测试：`TestIdempotentPublishAndTriggerReplay`、`TestIdempotentTriggerConcurrent`（8 并发同键仅 1 次引擎调用）。
- 10.3：`SaveIdempotency` 插入前 `DELETE ... WHERE expires_at <= now()` 机会式清理；MemoryStore 过期记录不重放；`TestMemoryIdempotencyExpiryAndFirstWin`。
- 10.4：`TestIdempotentPublishAndTriggerReplay` 断言重放 `adapter.TriggeredCount` 不增加；新键才增加。
- 残余风险：多副本并发时仅数据库唯一约束兜底，可能两个实例各调用一次引擎后才冲突；单实例锁已覆盖本地/CI。未做跨实例 reservation。

## task 12.1 negative matrix inventory
- DAG/validation: `domain/dag_test.go` `TestValidateDAGDefinitionInvalid` covers invalid_dag_version, duplicate_task_id, dependency_cycle, unknown_task_type, missing_dependency, invalid_sql_reference.
- API: `api/api_test.go` per_page=201 400; cross-tenant get 404; `api/authz_test.go` cross-tenant 404 before authz.
- Service: `service/runtime_test.go` missing output dataset 400 path (`TestPublishRejectsOutputDatasetWithoutResolver`, `TestPublishRejectsNonexistentOutputDataset`).
- Idempotency: `TestIdempotentPublishAndTriggerReplay` different body 409 already_exists.
- `go test ./internal/modules/pipelines/...` green.

## task 14.1 ports doc
- `docs/architecture/services-and-ports.md` added `Kestra（pipeline-kestra profile） | 18086 | 本地/CI，仅 pipeline-kestra profile`.

## task 14.4 gates
- `make build` OK
- `make test` all packages ok
- `make test-race` all packages ok
- `make lint` → golangci-lint not installed; `go vet ./...` passed

## task 9.3 输出与成功状态原子写
- 新增 `Repository.MarkSucceededWithOutputs`：Postgres 在同一事务内执行 CAS `UPDATE pipeline.runs` 并插入 `pipeline.run_outputs`；任一失败回滚，run 保持非终态。Memory 在同一锁内更新 run 与 outputs，租户不匹配即失败。
- `reconcileRun` 成功路径改用该原子方法；失败回退 `output_writeback_failed` 且不写终态。
- 测试：`TestMemoryMarkSucceededWithOutputsAtomic`（输出租户不匹配时 run 仍 running、无输出）。
