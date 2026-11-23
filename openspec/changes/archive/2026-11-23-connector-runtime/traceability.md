# Traceability: connector-runtime

## 变更信息

| 项目 | 值 |
| --- | --- |
| Change | `connector-runtime` |
| Stage | Stage 2（in_progress） |
| Wave | Wave 1 |
| Stage AC | S2-AC-06 |
| 负责目录 | `internal/modules/connectors/`、`internal/modules/ingestion/`、`openspec/changes/connector-runtime/`、`deploy/connector-runtime/` |
| 依赖 | `runtime-observability`（已归档）、`edge-gateway`（已归档）、`authorization-openfga-cedar`（已归档，跨租户授权断言已补测） |
| 状态 | implementation complete（2026-11-23）；归档评审已启动，待负责人确认后归档 |

## Stage 2 AC 映射

| AC | 要求（摘要） | 覆盖的 Requirement | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-06 | Connector 真实同步：Kafka Connect/Debezium CDC 至少一条真实链路，最终生成 Dataset 并可查询；Meltano 不在 Stage 2 验收范围 | connectors: 同步运行时预检与类型范围、运行时连接器配置生成、Kafka Connect 任务提交、运行时状态读取与归一；ingestion: 同步作业调度与运行时提交、运行时状态对账与回写、初始快照完成与成功确认 | `evidence/2026-11-23-cdc-e2e-three-way.md`：作业 `01a20826-192c-7945-8dca-b887e58f5d74` / `01a20854-1756-7732-889a-2198c226a600` 均 `succeeded`，源表 5 = sink `verified_rows` 5 = Trino 5，Dataset 命名空间 `ds_<uuid8>_public`，两次干净复跑 | verified（真实 E2E，本地 profile + 合成数据） |
| S2-AC-06 | 负向与降级：运行时不可用不伪造成功；失败/重试/拒绝路径可测 | connectors: 凭据解析与秘密边界、运行时任务停止与回收、运行时命名空间与租户隔离；ingestion: 调度幂等与并发约束、取消与运行时停止传播、重试/退避与失败分类 | `evidence/2026-11-23-negative-matrix-6-3.md`（默认 18/18）、`evidence/2026-11-23-cancel-and-negative-matrix.md`（`--with-outage` 10/10）、`evidence/2026-11-23-integration-and-telemetry.md`（3 个真实实例用例）；paused/error 为 403，契约见 `specs/connectors/spec.md` 的 MODIFIED「启动同步」 | verified（E2E 负向 + 真实集成 + 单测） |
| S2-AC-06 | 可观测与证据要求 | connectors: 同步运行时观测；ingestion: 同步运行观测、后台调度与对账的租户隔离 | `evidence/2026-11-23-connector-metrics.txt`、`evidence/2026-11-23-jaeger-trace-sample.json`、`evidence/2026-11-23-telemetry-snippet.yaml`；告警只验证指标存在，未 firing | verified（抓取样例）；告警 firing 未覆盖 |

门禁 G1 对应：真实实例在 Compose profile 启动（任务 5.1/6.2）；成功/失败重试/拒绝路径集成测试（6.1/6.3）；外部实例不可用降级明确且不冒充成功（1.2/6.3）；授权 change 跨租户/越权测试（6.3，依赖已归档的 `authorization-openfga-cedar`）。Meltano 明确不在本 change 与 Stage 2 验收范围（Stage 3 再评估）。

## Requirement 映射

### connectors（delta：8 条 ADDED + 1 条 MODIFIED）

| Requirement | 关键场景 | 对应设计 | 验证 | 状态 |
| --- | --- | --- | --- | --- |
| 同步运行时预检与类型范围 | 受支持类型通过、不支持类型拒绝、运行时不可达、缺配置 | D2/D5 | 单测（假 Connect 健康检查）+ E2E 负向 503/400/403 | verified（单测 + E2E） |
| 运行时连接器配置生成 | 确定性、租户命名空间、非法标识符拒绝 | D3 | golden 单测（同名跨租户、非法表名、未知键） | verified（单测） |
| 凭据解析与秘密边界 | 提交时注入、无法解析、错误脱敏 | D4 | 单测（解析/缺失/脱敏断言）+ 集成错误凭据路径 | verified（单测 + 集成） |
| Kafka Connect 任务提交 | 首次创建、重复幂等、运行时拒绝 | D5/D9 | httptest 单测 + 集成真实提交/重复提交 + E2E 幂等 | verified（单测 + 集成） |
| 运行时任务停止与回收 | 暂停停止、删除回收、停止失败、已不存在 | D7/D9 | 单测 + 集成停止/回收路径 + E2E 取消传播 | verified（单测 + 集成） |
| 运行时状态读取与归一 | 运行中、任务失败、运行时不可用 | D6 | 表驱动单测 + 集成真实状态读取 | verified（单测 + 集成） |
| 运行时命名空间与租户隔离 | 跨租户同名不冲突、跨租户启动/暂停无副作用 | D3/D7 | 单测 + 伪造 `x-ontoaxis-tenant-id` → 403；真实第二租户不可行，见未覆盖 | verified（单测 + E2E 403） |
| 同步运行时观测 | 指标白名单、出站 trace、失败结果标签 | D8 | 指标/span 断言单测 + Prometheus/Jaeger 真实抓取 | verified（单测 + 抓取） |
| 启动同步（MODIFIED） | `paused`/`error` 被拒绝、无任务创建 | D2 | E2E paused/error 403（Cedar） | verified（E2E） |

### ingestion（delta：8 条 ADDED + 1 条 MODIFIED）

| Requirement | 关键场景 | 对应设计 | 验证 | 状态 |
| --- | --- | --- | --- | --- |
| 同步作业调度与运行时提交 | 排队作业提交、运行时不可达、Connector 已暂停 | D5 | 单测（调度器 + 假运行时）+ 集成 + E2E | verified（单测 + 集成） |
| 调度幂等与并发约束 | 并发仅一方提交、重复触发、同 Connector 并发 | D5/D7 | 并发单测（两 goroutine）+ 重入测试 + E2E 重复启动幂等 | verified（单测 + E2E） |
| 运行时状态对账与回写 | 运行中、运行时失败、运行时缺失、终态不改写 | D6 | 表驱动单测 + E2E 状态回写 | verified（单测 + E2E） |
| 初始快照完成与成功确认 | 快照完成成功、仅运行中不成功、停止失败留待清理 | D6 | 单测（行数比对/稳定两次）+ E2E 三方行数一致 | verified（E2E） |
| 取消与运行时停止传播 | 排队取消、运行中取消、停止未确认、终态冲突 | D7 | 单测四场景 + E2E 取消路径（200→`cancelled`） | verified（单测 + E2E） |
| 重试、退避与失败分类 | 重试成功、重试耗尽、不可重试立即失败 | D5/D9 | 单测（序列、上限、终态唯一） | verified（单测） |
| 同步运行观测 | 作业结果指标、对账 span、重试可观测 | D8 | 指标/span 断言 + Prometheus/Jaeger 抓取 | verified（单测 + 抓取） |
| 后台调度与对账的租户隔离 | 对账只处理本租户、跨租户取消无副作用 | D6/D7 | 单测（租户上下文）+ 集成负向 | verified（单测 + 集成） |
| 同步任务取消（MODIFIED） | 排队取消、运行中确认后取消、停止未确认保持 `running` | D7 | 单测四场景 + E2E 取消传播 | verified（单测 + E2E） |

## 未覆盖与后续

| 事项 | 说明 | 建议去向 |
| --- | --- | --- |
| 主键/upsert 与重启重放 | 当前 append 模式无 identifier；`iceberg.control.commit.timeout-ms=120000` 已修复重复写入，但重启/重放语义未验证 | Stage 3 或后续 change |
| 告警 firing | 只验证 `ontoaxis_connector_runtime_*` 指标存在与查询，规则未在本地触发 | 运维评审/`production-delivery` |
| 真实第二租户 E2E | 本地栈为单租户，跨租户用伪造租户头 403 与单测替代 | 授权环境或 Stage 3 |
| `cleanup.sh` 未在矩阵调用 | 销毁性 teardown 不放进 E2E 矩阵；用幂等复跑与独立清理替代 | 运维脚本评审 |
| Dataset 自动注册 | 当前由 `dataset-catalog` 既有 API 预创建并在 Connector 配置中绑定 | 后续 change 或 Stage 3 |
| 持续增量/流式语义 | 本 Stage 以初始快照完成并可查询为成功判定 | Stage 3 |
| 快照完成判定 large-table | 行数比对有界（默认 1,000,000 行）且可能受并发写影响 | Stage 3 watermark/offset 方案 |
| 凭据外部化 | Connect 持久化连接器配置含密码 | Stage 3（Vault / Connect config provider） |
| MySQL/S3/REST API/Kafka CDC 映射 | 本 change 仅 `postgres`，其他类型明确不支持 | 按需求另开 change |
| 生产/真实数据验收 | 本 change 只做本地 profile 与合成数据 | 授权环境另行执行 |

## 归档检查

- [x] `proposal.md`、`specs/connectors/spec.md`、`specs/ingestion/spec.md`、`design.md`、`tasks.md`、`traceability.md` 齐备且评审通过（评审记录 `evidence/2026-11-23-connector-runtime-archive-review.md`）。
- [x] `openspec validate connector-runtime --strict` 与 `openspec validate --all --strict` 通过（2026-11-23）。
- [x] tasks 全部完成并逐条附可复核证据（真实 E2E、负向矩阵、集成、遥测；未覆盖项已在「未覆盖与后续」显式列出）。
- [x] Stage AC S2-AC-06 的证据可复核（本地容器 profile + 合成数据；已标注非生产/非真实数据）。
- [x] 共享文件（`internal/app`、`cmd/ontoaxis-core`）的改动由 `stage-2-integration` 在提交 `8f3e533` 合并，并已补 2.4 合并记录；`deploy/compose`、`deploy/observability`、`go.mod` 未越权修改。
- [x] 负责人确认后由负责人执行归档；本 change 不自行归档、不自动 commit、不宣布 Stage 完成。
