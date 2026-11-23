# CDC 真实同步三方行数 E2E（2026-11-23）

## 结论

- 干净单次同步通过：源表 5 行 = sink `verified_rows` 5 = Trino 查询 5，作业 `succeeded`。
- 命令：`./scripts/connector-runtime-e2e.sh --success-only`，输出 `RESULT pass=7 fail=0`。
- Trino 读取依赖的 `local.location=/` 修复见 `2026-11-23-trino-local-location-fix.md`。

## 运行记录

| 检查 | 期望 | 实测 |
| --- | --- | --- |
| core `/readyz` | 200 | 200 |
| Kafka Connect REST | 200 | 200 |
| 启动同步 | 201 | 201 |
| 作业终态 | `succeeded` | `succeeded` |
| `verified_rows`（sink `total-records`） | 5 | 5 |
| Trino `select count(*)` | 5 | 5 |
| 源表 `public.customers` 行数 | 5 | 5 |
| 脚本总结果 | 全绿 | `pass=7 fail=0` |

标识（同一轮）：

- job：`01a207ca-ba59-70c2-b0fa-8073cb2dc804`（`succeeded`）
- dataset：`01a207ca-b9d6-7a4e-88ad-fb48b6a14425`（RID 形如 `ri.ontoaxis.main.dataset.<id>`）
- runtime ref：`oa-bd7662a5-23ab1d1c-sink`
- 表：`iceberg.<dataset namespace>.customers`，metadata `total-records=5`

## 边界与已知问题

- **重复写入**：如果同步期间 Kafka Connect 发生 rebalance（矩阵用例反复创建/删除连接器），sink task 重启后会重新消费并追加同样的行；实测出现 `verified_rows=10/15`。干净单次同步（无 churn）为 5 行。根因是 append 模式且没有 identifier fields/upsert，重启后的 offset 恢复语义未验证；需要补主键方案或确认 sink 恢复行为，不能把"重复同步不重复写"当作已实现。
- **5.3 尚未完全满足**：脚本覆盖 Connect 不可达（`--with-outage`）、非法配置、重复启动、未知资源、取消；未覆盖 paused 与跨租户用例，也没有调用 `deploy/connector-runtime/scripts/cleanup.sh`，因此 5.3 保持未勾选。
- 脚本在 `--success-only` 模式下会先清掉非活跃复制槽（源库 `max_replication_slots=10`），否则连续运行会因槽位耗尽失败。
- 一次运行中脚本被并发编辑导致尾部语法错误（检查项已全部执行并通过）；上表结果来自随后一次未被修改的运行。

## 2026-11-23 复跑：提交超时调大后无重复写入

上一轮观察到：矩阵 churn 或慢 catalog 下，sink 首个 snapshot 缺失 `kafka.connect.offsets.*`，随后 task 重放同一批记录，`verified_rows` 变 10/15。定位为 `iceberg.control.commit.timeout-ms` 默认 30s 过紧。

修复：`BuildSinkConfig` 显式设置 `iceberg.control.commit.timeout-ms=120000`（`internal/modules/connectors/runtime/sink.go`，golden 同步）。

复跑结果（两次干净运行一致）：

- 作业 `01a20826-192c-7945-8dca-b887e58f5d74` / dataset `ds_01a20826_public`：1 个 snapshot，added=5，total=5，summary 含 `kafka.connect.offsets.*`；源表 5、Trino 5。
- 作业 `01a20854-1756-7732-889a-2198c226a600` / dataset `ds_01a20854_public`：1 个 snapshot，added=5，total=5，summary 含 `kafka.connect.offsets.*`；脚本 `RESULT pass=7 fail=0`，INFO 行见 `2026-11-23-success-e2e-5-3.log`。

结论：重复写入来自首个 commit 未带 offset 的重放，不是 CDC 源重复；调大提交超时后单次提交即带 offset，源 = sink = Trino = 5。
