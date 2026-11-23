# SnapshotVerifier 接线与作业终态（2026-11-23）

## 结论

- `internal/app` 新增快照校验器，替换原先传 `nil` 的接线；同步作业可以进入终态 `succeeded`。
- 实测：作业 `01a20741-3bbd-7d59-a851-5cdd03b007c2` 由 `running` 变为 `succeeded`，`verified_rows=5`，日志 `sync job completed`。
- 校验口径：读取 `public.iceberg_tables` 的 `metadata_location` → 解析 Iceberg metadata JSON → 取 `current-snapshot-id` 对应 snapshot 的 `summary.total-records`；未提交快照时返回 `done=false`，作业保持 `running`。
- **限制（必须保留）**：当前 `verified_rows` 是 sink 侧 `total-records`，不是"源表行数 = sink 行数"的等式校验。源端计数对比依赖 Trino 或派发期 `expected_rows`，仍未实现；traceability 不得把它写成双向行数一致。

## 实现要点

- `internal/app/connector_verifier.go`
  - `snapshotVerifier` 实现 `ingestionruntime.SnapshotVerifier`；
  - 配置来源：优先 `job.Config`，为空时回退到 Connector 记录（实测同步作业的 `config` 持久化为空，必须回退）；
  - `icebergCatalogReader`：按 `DatasetNamespace(datasetID, schema)` 定位 `table_namespace`，逐表读取 metadata，累加 `total-records`；任一表无快照即 `done=false`；
  - 仅支持 `file://` warehouse（`localWarehousePath`），S3/GCS 会在初始化时报错，不静默降级；
  - 校验 metadata 路径必须落在 warehouse 根内，防止越界读取。
- `internal/app/runtime.go`：从 `ONTOAXIS_ICEBERG_WAREHOUSE` 构造 reader；未配置时保持 `nil` 并 WARN，作业不会假成功。
- `deploy/connector-runtime/compose.connector-runtime.yaml`：core 只读挂载 `ontoaxis_iceberg_warehouse:/warehouse`。

## 单测

`go test ./internal/app/` 覆盖：

- job 配置解析（缺 dataset_id/缺 tables/空表名 → error；schema 缺省 `public`）；
- reader 结果透传（有快照 done=true、无快照 done=false）；
- metadata 解析（`current-snapshot-id=-1` → 未提交；取 `current-snapshot-id` 对应的 `total-records`，不是最新数组元素）；
- 路径越界拒绝（`/etc/passwd`）；
- `file:///`、`file:/`、裸路径转换，`s3://` 明确报错。

## 证据

- 作业响应：`dist/connector-e2e/token.txt` 会话内调用 `GET /api/v1/ingestion/jobs/01a20741-3bbd-7d59-a851-5cdd03b007c2` 返回 `succeeded`；
- 日志：`sync job completed ... verified_rows=5`；
- 快照来源：`dist/connector-e2e/ns-metadata.json`（`added-records=5`）。

## 未完成

- 源端行数等式校验（依赖 Trino 或派发期 expected_rows）；
- 取消传播：`ingestion:cancel` 仍返回 503 `connector runtime stopper is not configured`（6.3 的取消场景因此无法通过）；
- 负向矩阵其余项（Connect 不可达、非法配置、paused/error、重试耗尽、幂等、跨租户 404）尚未用脚本固化。
