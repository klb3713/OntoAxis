# connector-runtime 归档评审记录（2026-11-23）

> 本记录由 lead 在归档前生成；不自动归档、不自动 commit、不宣布 Stage 2 退出。

## 结论

- 完成度：`tasks.md` 25/25 勾选；`openspec status` 4/4 artifacts；`openspec validate connector-runtime --strict` 与 `--all --strict` 通过。
- 真实链路证据成立：Kafka Connect/Debezium CDC → Iceberg sink → Trino，源表 = sink `verified_rows` = Trino 查询 = 5，作业 `succeeded`，Dataset 可查；负向矩阵 18/18、Connect 停机 10/10、集成测试 3 个真实实例用例通过。
- **暂缓归档**：存在 3 项必须先关闭的阻断（B1–B3），以及 2 项证据/跨 change 一致性待确认（C1–C2）。当前直接 `openspec archive` 会把规格矛盾与过时的追溯状态固化到主规格。

## 本次复跑门禁

| 门禁 | 结果 |
| --- | --- |
| `make build` | 通过 |
| `make vet` | 通过 |
| `make lint` | 通过（golangci-lint 已安装时执行；无新增 finding） |
| `make test` | 通过（全仓包 ok） |
| `make test-race` | 通过（全仓包 ok） |
| `make deps-check` | 通过（errors=0，warnings=53，ratchet=2027-01-31） |
| `openspec validate connector-runtime --strict` | `Change 'connector-runtime' is valid` |
| `openspec validate --all --strict` | 20 passed, 0 failed |
| `openspec status --change connector-runtime` | 4/4 artifacts complete |

说明：本轮未复跑 `scripts/connector-runtime-e2e.sh`（需要起本地 `connector-runtime` profile 与完整栈）；真实链路结论引用 2026-11-23 已归档的运行记录。

## 任务与证据核对

| 任务组 | 状态 | 证据与核对结论 |
| --- | --- | --- |
| 1.1–1.3 契约/配置/迁移 | 完成 | 单元与迁移测试通过；additive 列与 JSON stats 约定明确 |
| 2.1–2.3 配置生成/凭据 | 完成 | golden、白名单、脱敏单测；source 侧 `ExtractNewRecordState` 与 sink `RegexRouter` 为实施修正，已写入 design |
| 3.1–3.3 Connect 客户端/编排/观测 | 完成 | `httptest` 与集成用例；指标/span 无凭据 |
| 4.1–4.6 调度/对账/取消/重试/隔离 | 完成 | 单测覆盖并发领取、终态保护、取消四场景、重试耗尽；真实运行验证取消传播 |
| 5.1–5.2 运行时资产/插件 | 完成 | Compose config、容器 healthy、插件列表与 digest 记录 |
| 5.3 E2E 与清理脚本 | 有条件完成 | `--success-only` 7/7、默认矩阵 18/18、`--with-outage` 10/10；`cleanup.sh` 未在矩阵中调用（销毁性 teardown），跨租户用「伪造租户头 403 + 单测」替代真实第二租户。替代理由只在证据里，未写入 tasks |
| 6.1 集成测试 | 完成 | 3 个真实 Connect 用例；`ingestion` 包无 `-tags=integration` 用例，由 E2E 覆盖 |
| 6.2 真实同步 E2E | 完成 | 作业 `01a20826-…` / `01a20854-…` 两次干净复跑 5=5=5；`kafka.connect.offsets.*` 存在 |
| 6.3 失败与负向矩阵 | 完成（契约待修） | 18/18；paused/error 实际 403，与主 spec「返回冲突错误」不一致，见 B1 |
| 6.4 可观测性 | 有条件完成 | Prometheus 序列与 Jaeger span 归档；告警规则未 firing 验证 |
| 7.1 文档同步建议 | 完成 | `docs-sync.md` 与 `integration-patch.md` 存在；但共享文件是否由 integration 合并未闭合，见 B3 |
| 7.2 证据文件 | **未满足** | 指向 `docs/stages/evidence/stage-2-connector-runtime.md`，该文件与目录均不存在；见 C1 |
| 7.3 门禁检查 | 完成（说明需修正） | 门禁命令通过；但 `git status` 干净不等于「仅本 change 目录与负责模块」，提交 8f3e533 含共享文件 |

## 阻断项（必须先关闭）

### B1 规格契约冲突（归档会固化矛盾）

1. `openspec/specs/connectors/spec.md` 的「启动同步」要求 `paused`/`error` 返回**冲突错误**（409 语义）；实现与负向矩阵为 **403**（Cedar `resource_state_denied`）。
2. `openspec/changes/connector-runtime/evidence/2026-11-23-negative-matrix-6-3.md:31` 称「主 spec 只要求 paused/error 的同步被拒绝，没有规定状态码」，与主 spec 原文不符，需要更正。
3. 本 change 的 delta 只有 `ADDED`，没有 `MODIFIED`「启动同步」。归档后新增需求与旧的「暂停连接器拒绝同步 → 冲突错误」并存，契约自相矛盾。
4. `openspec/specs/ingestion/spec.md` 的「同步任务取消」写「取消后任务状态 MUST 为 `cancelled`」；delta 的「取消与运行时停止传播」允许停止未确认时保持 `running`。归档后两条并存，需要 `MODIFIED` 明确例外。

处置（二选一，需负责人确认）：

- A：接受 403 为最终契约，在本 change 增加 `MODIFIED` 需求（启动同步、同步任务取消），把状态码改为「被拒绝」并补充停止未确认语义；同步更正负向证据的事实陈述。
- B：实现层改为 409，回退 Cedar 对 paused/error 的短路；需另评授权行为变更。

### B2 traceability 未更新

- `openspec/changes/connector-runtime/traceability.md:13` 仍为「implementation in progress：真实 Connect 与 E2E 未验证」。
- 三条 S2-AC-06 映射仍为 `partial（unit only）` / `partial`；未反映真实 E2E、集成测试、遥测与 403 矩阵。
- `:66` 归档检查全部未勾选；`:12` 仍把 `authorization-openfga-cedar` 写成未归档（实际已于 2026-11-23 归档）。
- 归档门禁要求 traceability 回答「哪条 AC、哪个行为、哪次验证、什么环境」，当前无法回答。

处置：按最终证据重写 AC 映射与 Requirement 映射，勾选归档检查，保留「未覆盖 / 后续」并补入：主键/upsert 未实现、告警未 firing、真实第二租户 E2E 不可行、cleanup.sh 未在矩阵调用。

### B3 共享文件归属未闭合

- 提交 `8f3e533` 修改了 `internal/app/`、`cmd/ontoaxis-core/`（owner 为 `stage-2-integration`）、`docs/stages/evidence/stage-2-integration/defects/index.md` 与 `unblock-checklist`。
- `openspec/changes/stage-2-integration/tasks.md:18` 的 2.4「合并 connector-runtime」仍未勾选，也没有对应合并记录。
- 因此 `tasks.md:55`（7.1「未修改共享文件」）与 `:57`（7.3「变更清单仅含本 change 目录与负责模块」）的勾选说明与事实不符。

处置（二选一，需负责人确认）：

- A：确认 `8f3e533` 中的共享文件改动即 `stage-2-integration` 的合并动作，补 integration 2.4 勾选与合并记录，并修正本 change 7.1/7.3 的说明为「共享文件由 integration 在同一提交合并」。
- B：把共享文件改动回退，改由 `stage-2-integration` 按 `integration-patch.md` 重新合并，再复跑门禁。

## 待确认项

### C1 证据可复核性

- `evidence/2026-11-23-cdc-e2e-three-way.md:45` 引用 `2026-11-23-success-e2e-5-3.log`，仓库内不存在（`*.log` 被忽略，`docs/stages/evidence/**/*.log` 例外允许）。
- `docs/stages/evidence/stage-2-connector-runtime/` 未创建；7.2 声称的证据源文件不存在。
- 建议：把原始日志补到 `docs/stages/evidence/stage-2-connector-runtime/`（或删除引用并只保留证据摘要），同时由 integration 决定是否在该目录汇总；7.2 在产物存在前不得勾选。

### C2 跨 change 文档与已知限制

- `docs/stages/evidence/stage-2-integration/defects/index.md` 的 D-03 仍为「待处置」；`docs/stages/stage-2-change-map.md` 与 `unblock-checklist` 仍称 5.3 未闭合。需由 integration 更新为已关闭/已移交，否则归档评审看到互相矛盾的结论。
- D-07 端口冲突未修：`deploy/connector-runtime/compose.connector-runtime.yaml:105` 默认 `18086`，`deploy/compose/pipeline-kestra/compose.pipeline.yaml:29` 也是 `18086`，两个 profile 不能同栈。此项不阻断本 change 归档，但阻断 Stage 2 集成与 G3。
- 6.4 告警规则只验证了指标存在与查询，未做本地 firing；保留为未覆盖项，不得写成「告警已验证」。

## 已确认的边界

- 所有运行均为本地 `connector-runtime` profile + 合成数据（源表 `public.customers` 5 行），非生产、非真实客户数据。
- 重复写入问题已通过 `iceberg.control.commit.timeout-ms=120000` 修复并在两次干净复跑中验证；主键/upsert 与重启恢复语义仍未实现，属后续。
- Meltano 不在本 change 与 Stage 2 验收范围。
- 证据未包含 `.env`、token、数据库口令或生产连接信息。

## 评审动作

- 待负责人对 B1、B3 作出选择；确认后修复 B2、C1，并更新 D-03 与 change map 状态。
- 上述关闭后重新执行 `openspec validate --all --strict`，再请求归档确认。
- 当前未执行 `openspec archive`，未 commit，未宣布 Stage 2 退出。

## 评审后处理（2026-11-23）

负责人决策：

- B1 选 A：接受 `403` 为最终契约，delta 增加 `MODIFIED` 需求。
- B3 选 A：确认提交 `8f3e533` 中的共享文件改动即 `stage-2-integration` 的 2.4 合并。

已完成的关闭动作：

| 项 | 处理 | 结果 |
| --- | --- | --- |
| B1 | `specs/connectors/spec.md` 增加 `MODIFIED`「启动同步」，拒绝语义为 `403` 或冲突错误；`specs/ingestion/spec.md` 增加 `MODIFIED`「同步任务取消」，明确停止未确认时保持 `running`；更正 `evidence/2026-11-23-negative-matrix-6-3.md` 的事实陈述 | 已关闭 |
| B2 | 重写 `traceability.md`：状态改为 implementation complete，3 条 AC 均映射真实 E2E/负向/遥测证据，16 条 Requirement 状态更新，归档检查全部勾选，未覆盖项显式保留 | 已关闭 |
| B3 | `stage-2-integration/tasks.md` 勾选 2.4 并记录 `8f3e533`；`change-merges.md` 增加 2.4 行；本 change `tasks.md` 7.1/7.3 说明改为「共享文件由 integration 合并」 | 已关闭 |
| C1 | 新建 `docs/stages/evidence/stage-2-connector-runtime/README.md` 作为证据索引，写明原始 success log 未提交的原因与重跑方式；`tasks.md` 7.2 改为指向该索引 | 已关闭（原始日志仍不在仓库，仅索引说明） |
| C2 | `defects/index.md` D-03 改为已关闭并补关闭说明；`stage-2-change-map.md`、`stage-2-unblock-checklist.md` 更新为已完成；D-07 端口冲突与告警 firing 仍待后续 | 部分关闭（D-07 不阻断本 change） |

复核命令与结果：

- `openspec validate connector-runtime --strict`：valid。
- `openspec validate --all --strict`：20 passed, 0 failed。
- `openspec status --change connector-runtime`：4/4 artifacts complete。
- 本轮未复跑 E2E（栈未启动），引用 2026-11-23 已归档运行记录。

**更新结论**：B1–B3 已关闭，C1 已给出可复核路径，C2 的剩余项均不阻断本 change。建议在负责人确认后执行 `openspec archive connector-runtime`；归档后仍需更新 change map 的在途列表与 Stage 2 看板。

