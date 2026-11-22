# Compose 基础栈与授权 overlay 冒烟（2026-11-23）

## 关键修复

基础 Compose 未提供 OpenFGA，导致 `internal/app` 的 fail-fast authz 启动失败。新增本地 overlay `deploy/compose/authz/compose.authz.yaml` + `authz-init.sh`（memory OpenFGA、创建 store/model、写共享 `authz.env`），并让 `scripts/smoke-foundation.sh` 默认叠加该 overlay；本地以 `ONTOAXIS_ACTIONS_EXECUTION_MODE=noop` 显式 local 模式启动（不伪造 Temporal；生产默认仍为 temporal fail-closed）。

## 已执行

```bash
./scripts/smoke-foundation.sh
```

结果：

```text
PASS: ontoaxis-core foundation smoke (/healthz, /readyz, /metrics, 404)
EXIT:0
```

启动后 `/readyz` 原始输出（节选）：

```json
{"checks":{"actions":"ok","authorization":"ok","connectors":"ok","database":"ok","datasets":"ok","ingestion":"ok","objects":"ok","ontology":"ok","pipelines":"ok","query":"ok","tenancy":"ok"},"status":"ok"}
```

空库 `down -v` 后重建，migration 输出 `goose: no migrations to run` 之后正常；以上证明 9 个业务模块 + authorization + database 共 11 项 ready。

## 限制

- 仅基础栈 + OpenFGA；Temporal/Kestra/Kafka Connect/Meilisearch 等真实运行时 profile 未合并启动，故 Stage 2 全链路 E2E 仍未通过。
- `noop` 执行模式仅用于本地基础冒烟，证据中明确标注，不冒充 Temporal/真实 Action 执行。

## 5.5 可重复性

连续执行两次 `./scripts/smoke-foundation.sh`（脚本内 `down -v --remove-orphans` 清空后重建），两次均 `PASS`、`EXIT:0`；无不可解释差异。仅覆盖基础栈 + OpenFGA，不覆盖 Temporal/Kestra/Kafka Connect/Meilisearch profile。

## observability profile 集成（2026-11-23）

- `scripts/observability-smoke.sh` 已改为同时加载 `deploy/compose/authz/compose.authz.yaml`（core 需要 authz 环境）。
- 运行结果 exit 0：`PASS: 1 HTTP metric series, 1 trace(s), Perses dashboard imported`；Prometheus 有 `ontoaxis_core_http_*` 指标，Jaeger 有 core trace，Perses dashboard 导入成功。
- 任务 5.3 勾选。注：日志中首次等待 Prometheus 样本出现 AssertionError 是脚本内部重试，最终成功。

## edge profile 集成（2026-11-23）

- 修复 `deploy/compose/authz/compose.authz.yaml`：为 `ontoaxis-core-edge` 增加 authz 环境、authz-init 依赖、`/shared/authz.env` 入口与共享卷；此前 edge core 启动失败 `ONTOAXIS_AUTHZ_OPENFGA_API_URL is required`。
- `EDGE_TEST_FAIL_CLOSED=1 ./scripts/edge-smoke.sh`：12 passed / 0 failed；覆盖 healthz/readyz、metrics 隐藏、unknown 404、JWT 401、有效 JWT 200、伪造身份头 403、405、429 限流、鉴权后端不可达 fail-closed 5xx。
- 任务 5.2 勾选；该修复属跨 change 装配缺陷，已在任务 5.6 记录。

## 归档证据复跑（2026-11-23）

- `make observability-rules-test`：SUCCESS（promtool rules test）。
- `scripts/observability-smoke.sh`：exit 0（metric/trace/Perses 通过）。
- `EDGE_TEST_FAIL_CLOSED=1 scripts/edge-smoke.sh`：12/0 通过。
- 任务 1.3 勾选。

## 5.6 compose 跨 change 缺陷修复授权记录

- 负责人已在目标指令（m00792）中授权“其他情况默认按你的建议确认通过”，并批准 Stage 2 全部剩余 change；据此修复 `deploy/compose/authz/compose.authz.yaml` 的 edge core authz env 装配缺陷。
- 修复后 `EDGE_TEST_FAIL_CLOSED=1 scripts/edge-smoke.sh` 12/0 通过；任务 5.2/5.6 勾选。
