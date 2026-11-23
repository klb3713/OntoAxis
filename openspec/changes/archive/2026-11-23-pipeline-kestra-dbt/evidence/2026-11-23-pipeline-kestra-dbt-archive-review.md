# pipeline-kestra-dbt 归档评审记录（2026-11-23）

## 结论

- 任务：69/69 全勾选，无未完成项；`openspec status` 显示 4/4 artifacts complete。
- `openspec validate pipeline-kestra-dbt --strict` 通过；`openspec validate --all --strict` 20/20。
- traceability：无遗留 `planned`；S2-AC-05 的证据全部为 `actual`，未覆盖项在「未覆盖 / 后续」表中逐条给出归属。
- 允许归档。归档后 delta spec 合入 `openspec/specs/pipelines/spec.md`，主规格由 421 行 delta 更新。
- 归档不等于 Stage 2 完成：AC-01/AC-11 及跨 change 全链路 E2E 仍归 `stage-2-integration`。

## 门禁（本次归档前复跑）

| 门禁 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go test ./...`（全仓） | 通过（第二次复跑；首次出现 1 次 flaky，见下） |
| `go test ./internal/modules/pipelines/...` | 通过（8 个包全 ok） |
| `go test -race ./internal/modules/pipelines/...` | 通过 |
| `go vet ./...` | 通过 |
| `make deps-check` | 通过（errors=0，warnings=30，placeholders=21） |
| `openspec validate --all --strict` | 20 passed, 0 failed |

环境说明：本次复跑在受限沙箱内进行，Go 构建缓存重定向到仓库内 `.gocache`（未提交）；模块缓存只读使用。

## 真实实例与证据

- Kestra `v2.0.12`（digest `sha256:b521aff2…`）+ dbt-core 1.12.7 / dbt-trino 1.8.0 + Trino `memory` catalog。
- 发布 200 → 触发 202 → run `succeeded` → `pipeline.run_outputs` 落库 → Trino `memory.oa_t11111111_p01a205f5.build_orders` 可查。
- 负向与失败路径：坏 SQL `failed` 且无输出、Kestra 停机 publish/trigger 503、恢复后重试版本只 +1 且 execution 唯一、状态回写不回退、跨租户 404 / 非法 DAG 400 / 幂等异体 409。
- 遥测：`/metrics` 与 trace 摘录，断言无 tenant/凭据 label。
- 证据位置：`evidence/2026-11-23-kestra-v2-upgrade-e2e.md`、`evidence/2026-11-23-pipeline-publish-compensation-unit.md`、`evidence/2026-11-22-pipeline-unit.md`、`evidence/2026-11-23-metrics-trace.txt`；原始交接 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/`（含 `raw/` 下 13.x 原始响应）。

## 边界与限制

- 共享文件改动经 `integration-patch.md` 记录：`deploy/compose/pipeline-kestra/`（本 change 自有）、`deploy/compose/compose.yaml` 的 Trino digest 属 `stage-2-dependency-governance`；`Makefile`/`AGENTS.md`/`RUNBOOK`/brief/change-map 的改动为两 change 混排，提交时需拆分。
- 本 change 未修改 proto、`web/`、`internal/app`、`cmd/`。
- 已知残余风险：
  1. `internal/modules/connectors/runtime` 的 `TestHTTPClientPing/reachable` 在 `go test ./...` 高并发下出现过 1 次超时 flaky，单包隔离与复跑均通过；归 `connector-runtime`（D-03）处理，与本 change 无代码关联。
  2. 运行状态的后台主动推进、强制 `Idempotency-Key`、Schedule 投影等未覆盖项已列入 traceability「未覆盖 / 后续」，归 `stage-2-integration` 或后续 change。
  3. Kestra RBAC、生产 TLS、Trino 持久 catalog 不在本 change 范围，归 `production-delivery`。
- 无 `.env`、token、密码或真实数据进入证据；本地 Basic Auth 口令只出现在文档占位处。

## 评审动作

- 负责人于 2026-11-23 确认归档；本记录由执行方（lead）在归档前生成。
- 未自动 commit、未自动宣布 Stage 2 退出。
