# 取消传播、幂等启动与负向矩阵（2026-11-23）

## 结论

- 取消链路已打通：`ingestion:cancel` 对运行中作业返回 200，作业变 `cancelled`，Connect 侧 source/sink 被删除。
- 重复启动改为幂等：同一连接器已有 `queued`/`running` 作业时返回原 job id，不再排队重复作业。
- 负向矩阵脚本 `scripts/connector-runtime-e2e.sh` 落地，默认 8/8 通过（`--with-success` 追加成功路径，`--with-outage` 追加 Connect 不可达）。

## 本轮修复的真实缺陷

| 缺陷 | 现象 | 修复 |
| --- | --- | --- |
| stopper 未接线 | `ingestion:cancel` 返回 503 `connector runtime stopper is not configured` | 新增 `ingestion.Module.SetRuntimeStopper`，app 层注入 `ingestionruntime.NewConnectorJobRuntime(connectRuntime)` |
| `UpdateJobAndRun` 类型错误 | 任何经 `applyTransition` 的状态变更返回 500：`column "lease_until" is of type timestamp with time zone but expression is of type text` | SQL 改为 `$11::timestamptz` |
| 提交后过早失败 | 作业提交后 1–2s 内被 reconcile 判 `runtime_not_running: runtime_missing`（Connect 异步创建连接器） | Reconciler 增加 `StartupGrace`（默认 30s），新作业在窗口内不因 missing/stopped/paused 失败 |
| 重复启动产生重复作业 | 同一连接器连续两次 `sync` 各生成一个 job，后一个在派发期被拒 | `StartSync` 先查 `ListJobs`，有活动作业则返回其 id（幂等） |

## 负向矩阵（`scripts/connector-runtime-e2e.sh`）

默认用例与实测结果：

| 用例 | 期望 | 实测 |
| --- | --- | --- |
| core `/readyz` | 200 | 200 |
| Kafka Connect REST | 200 | 200 |
| 未知连接器 | 404 | 404 |
| 同步配置含未知 key | 400（创建期宽松、同步期严格） | 400 |
| 暂停连接器 | 200 | 200 |
| 对 paused 连接器启动同步 | 拒绝（授权策略 `resource_state_denied` → 403） | 403 |
| 标记连接器 error | 200 | 200 |
| 对 error 连接器启动同步 | 拒绝（403） | 403 |
| 恢复 error 连接器 | 200 | 200 |
| 源库主机不可达时提交 | 作业最终 `failed` | `failed` |
| 失败信息脱敏 | 不含口令/连接串 | 不含 |
| 恢复连接器 | 200 | 200 |
| 重复启动 | 201 且返回同一 job | 201，同一 job |
| 运行中取消 | 200 + `cancelled` | 200 + `cancelled` |
| 伪造 `x-ontoaxis-tenant-id` | 403（fail-closed，不按头切换租户） | 403 |

补充说明：

- 对已终态作业调用取消会被 Cedar 以 `resource_state_denied` 拒绝（403），这是策略行为，不是 500。
- **跨租户**：本地 realm 的 `tenant_id` 是 `ontoaxis-web` 客户端上的硬编码 claim，无法签出第二租户 token，所以脚本用"伪造租户头必须被拒绝（403）"作为 fail-closed 断言；真实跨租户数据隔离由单测覆盖：`internal/modules/ingestion/api/authz_test.go`（otherTenant）、`internal/modules/ingestion/service/cancel_test.go`（跨租户 ErrNotFound）、`internal/modules/connectors/runtime/runtime_test.go`（跨租户 Stop/Status 不触碰运行时）。
- **cleanup.sh**：`deploy/connector-runtime/scripts/cleanup.sh` 是销毁性 teardown（会删容器/卷），不在 E2E 矩阵里调用，避免把正在验证的栈拆掉；矩阵结束时按 API 删除本次创建的连接器。

## 证据

- 默认矩阵：`RESULT pass=18 fail=0`（含 paused/error/提交失败/伪造租户）；`--with-success`：`RESULT pass=11 fail=0`（成功路径 `succeeded`、`verified_rows=10`）；`--with-outage`：`RESULT pass=10 fail=0`
- 取消实测：作业 `01a20772-75b8-705c-a507-ffd4cf9d604a` 由 `running` → `cancelled`，`GET /connectors` 返回 `[]`
- 单测：`go test ./internal/modules/ingestion/...`、`./internal/app/` 全绿（新增 startup grace 用例）

## 未完成

- `--with-outage`（Connect 停机 → StartSync 503 → 恢复）：`RESULT pass=10 fail=0`，Connect 恢复后 REST 200。
- 5.3 全量 E2E（源/sink/Trino 三方行数断言）仍受 Trino 读取问题阻塞，脚本当前只断言作业终态与 `verified_rows`。
- 6.1 `-tags=integration`、6.4 遥测证据仍待补。

## 观察：重复写入（2026-11-23）

- 成功路径作业 `01a20774-4d67-…` 的 `verified_rows=15`，而源表 `public.customers` 只有 5 行。
- 检查该 Dataset 的表 metadata：3 个 snapshot，每个 `added-records=5`、`total-records` 分别为 5/10/15，即同一批 5 行被提交了三次。
- 触发条件：矩阵脚本在同一次运行里反复创建/删除连接器，Kafka Connect 发生 rebalance，sink task 重启后从头重放；当前 sink 是 append 模式，没有 identifier fields / upsert，所以重复落盘。
- 影响：`verified_rows` 是 sink 侧 `total-records`，会累加重复行；源/汇行数等式校验会因此失败。AC-06 的单次干净同步不受影响（5 行），但"重复同步/重启不产生重复数据"尚未保证。
- 后续：补主键白名单 + `iceberg.tables.default-id-columns` 走 upsert，或确认 sink 重启后从 metadata 记录的 offset 恢复（当前未验证恢复语义）。
