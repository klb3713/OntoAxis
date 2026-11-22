# Traceability: connector-runtime

## 变更信息

| 项目 | 值 |
| --- | --- |
| Change | `connector-runtime` |
| Stage | Stage 2（in_progress） |
| Wave | Wave 1 |
| Stage AC | S2-AC-06 |
| 负责目录 | `internal/modules/connectors/`、`internal/modules/ingestion/`、`openspec/changes/connector-runtime/`、`deploy/connector-runtime/` |
| 依赖 | `runtime-observability`（已归档）、`edge-gateway`（已归档）；`authorization-openfga-cedar`（未归档，跨租户授权断言依赖） |
| 状态 | implementation in progress：单元/race/静态校验已通过，真实 Connect 与 E2E 未验证 |

## Stage 2 AC 映射

| AC | 要求（摘要） | 覆盖的 Requirement | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-06 | Connector 真实同步：Kafka Connect/Debezium CDC 至少一条真实链路，最终生成 Dataset 并可查询；Meltano 不在 Stage 2 验收范围 | connectors: 同步运行时预检与类型范围、运行时连接器配置生成、Kafka Connect 任务提交、运行时状态读取与归一；ingestion: 同步作业调度与运行时提交、运行时状态对账与回写、初始快照完成与成功确认 | 单元/race 证据见 `evidence/2026-11-22-connector-runtime-unit.md`；**真实 E2E 未执行**（无 job id/Dataset RID/Trino 行数） | partial（unit only） |
| S2-AC-06 | 负向与降级：运行时不可用不伪造成功；失败/重试/拒绝路径可测 | connectors: 凭据解析与秘密边界、运行时任务停止与回收、运行时命名空间与租户隔离；ingestion: 调度幂等与并发约束、取消与运行时停止传播、重试/退避与失败分类 | 单元测试覆盖 Connect down/非法配置/paused/停止未确认/重试耗尽/脱敏；跨租户 403 仍待 authz 装配 | partial（unit only） |
| S2-AC-06 | 可观测与证据要求 | connectors: 同步运行时观测；ingestion: 同步运行观测、后台调度与对账的租户隔离 | 指标/span 断言单测通过；Prometheus 抓取与 dashboard 片段未产出 | partial |

门禁 G1 对应：真实实例在 Compose profile 启动（任务 5.1/6.2）；成功/失败重试/拒绝路径集成测试（6.1/6.3）；外部实例不可用降级明确且不冒充成功（1.2/6.3）；授权 change 跨租户/越权测试（6.3，跨 change 依赖 `authorization-openfga-cedar`）。Meltano 明确不在本 change 与 Stage 2 验收范围（Stage 3 再评估）。

## Requirement 映射

### connectors（delta：8 条 ADDED）

| Requirement | 关键场景 | 对应设计 | 计划验证 | 状态 |
| --- | --- | --- | --- | --- |
| 同步运行时预检与类型范围 | 受支持类型通过、不支持类型拒绝、运行时不可达、缺配置 | D2/D5 | 单测（假 Connect 健康检查）+ E2E 负向 503/400 | verified（unit） |
| 运行时连接器配置生成 | 确定性、租户命名空间、非法标识符拒绝 | D3 | golden 单测（同名跨租户、非法表名、未知键） | verified（unit） |
| 凭据解析与秘密边界 | 提交时注入、无法解析、错误脱敏 | D4 | 单测（解析/缺失/脱敏断言）+ 集成错误凭据路径 | verified（unit） |
| Kafka Connect 任务提交 | 首次创建、重复幂等、运行时拒绝 | D5/D9 | httptest 单测 + 集成真实提交/重复提交 | verified（unit） |
| 运行时任务停止与回收 | 暂停停止、删除回收、停止失败、已不存在 | D7/D9 | 单测 + 集成停止/回收路径 | verified（unit） |
| 运行时状态读取与归一 | 运行中、任务失败、运行时不可用 | D6 | 表驱动单测 + 集成真实状态读取 | verified（unit） |
| 运行时命名空间与租户隔离 | 跨租户同名不冲突、跨租户启动/暂停无副作用 | D3/D7 | 单测 + E2E 跨租户 404 无副作用 | partial（unit） |
| 同步运行时观测 | 指标白名单、出站 trace、失败结果标签 | D8 | 指标/span 断言单测 + Prometheus 抓取 | partial（unit） |

### ingestion（delta：8 条 ADDED）

| Requirement | 关键场景 | 对应设计 | 计划验证 | 状态 |
| --- | --- | --- | --- | --- |
| 同步作业调度与运行时提交 | 排队作业提交、运行时不可达、Connector 已暂停 | D5 | 单测（调度器 + 假运行时）+ 集成 | verified（unit） |
| 调度幂等与并发约束 | 并发仅一方提交、重复触发、同 Connector 并发 | D5/D7 | 并发单测（两 goroutine）+ 重入测试 | verified（unit） |
| 运行时状态对账与回写 | 运行中、运行时失败、运行时缺失、终态不改写 | D6 | 表驱动单测（映射矩阵、终态保护） | verified（unit） |
| 初始快照完成与成功确认 | 快照完成成功、仅运行中不成功、停止失败留待清理 | D6 | 单测（行数比对/稳定两次）+ E2E 行数三方一致 | partial（fake verifier） |
| 取消与运行时停止传播 | 排队取消、运行中取消、停止未确认、终态冲突 | D7 | 单测四场景 + E2E 取消路径 | verified（unit） |
| 重试、退避与失败分类 | 重试成功、重试耗尽、不可重试立即失败 | D5/D9 | 单测（序列、上限、终态唯一） | verified（unit） |
| 同步运行观测 | 作业结果指标、对账 span、重试可观测 | D8 | 指标/span 断言 + 抓取样例 | partial（unit） |
| 后台调度与对账的租户隔离 | 对账只处理本租户、跨租户取消无副作用 | D6/D7 | 单测（租户上下文）+ 集成负向 | verified（unit） |

## 未覆盖与后续

| 事项 | 说明 | 建议去向 |
| --- | --- | --- |
| Dataset 自动注册 | 当前由 `dataset-catalog` 既有 API 预创建并在 Connector 配置中绑定；自动注册需 `ingestion→datasets` 端口与装配 | 后续 change 或 Stage 3 |
| 持续增量/流式语义 | 本 Stage 以初始快照完成并可查询为成功判定，未定义长期订阅、Compaction、保留策略 | Stage 3 |
| 快照完成判定 large-table | 行数比对有界（默认 1,000,000 行）且可能受并发写影响 | Stage 3 watermark/offset 方案 |
| 凭据外部化 | Connect 持久化连接器配置含密码 | Stage 3（Vault / Connect config provider） |
| MySQL/S3/REST API/Kafka CDC 映射 | 本 change 仅 `postgres`，其他类型明确不支持 | 按需求另开 change |
| 告警阈值与 dashboard 签核 | 片段移交 `production-delivery`，阈值未签核 | 运维评审 |
| 授权层跨租户/越权断言 | 依赖 `authorization-openfga-cedar` | 该 change 合并后补测 |
| 生产/真实数据验收 | 本 change 只做本地 profile 与合成数据 | 授权环境另行执行 |

## 归档检查（全部未完成，仅列清单）

- [ ] `proposal.md`、`specs/connectors/spec.md`、`specs/ingestion/spec.md`、`design.md`、`tasks.md`、`traceability.md` 齐备且评审通过。
- [x] `openspec validate connector-runtime --strict` 通过（2026-11-22）。
- [ ] tasks 全部完成并逐条附可复核证据（非伪造、非模板）。
- [ ] Stage AC S2-AC-06 的证据可复核（真实容器 profile + 合成数据；明确标注非生产/非真实数据）。
- [ ] 共享文件（`internal/app`、`cmd/ontoaxis-core`、`deploy/compose`、`deploy/observability`、`go.mod`）无越权修改，片段与 patch 已移交对应 owner。
- [ ] 评审确认后由负责人执行归档；本 change 不自行归档、不自动 commit、不宣布 Stage 完成。
