# Traceability: pipeline-kestra-dbt

> 证据状态约定：`planned` = 计划中、尚未执行；`actual` = 已执行且可复核。本 change 已进入实施：单元/契约/迁移与 Kestra v2.0.12 真实 E2E（成功/失败/重试/输出血缘/状态回写/负向/遥测）均为 `actual`；证据见 `evidence/` 与 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/`。

## Stage 2 AC 映射

| Stage AC | 验收要求 | 本 change 覆盖 | 证据（planned / actual） |
| --- | --- | --- | --- |
| S2-AC-05 | Pipeline 通过真实 Kestra + dbt 执行：发布 Flow、运行、状态回写、失败重试与 Dataset 输出均可验证 | 发布：ADDED「Authoring DAG contract to Flow generation」「Flow namespace and labels for tenant isolation」「Flow publish and execution trigger」+ MODIFIED「Pipeline publishing and immutable versions」「Kestra adapter boundary」；运行：MODIFIED「Pipeline run entry」「Pipeline run query」+ ADDED「Run status reconciliation and writeback」；失败重试：ADDED「Retry, backoff and compensation」+ 负向矩阵；Dataset 输出：ADDED「Dataset outputs and lineage」+ MODIFIED「Dataset dependency validation」；dbt：ADDED「dbt project and model execution」 | `actual`：`evidence/2026-11-23-kestra-v2-upgrade-e2e.md`（发布 200、触发 202、run `succeeded`、输出 pipeline `01a205f5-…` 的 `run_outputs` 与 Trino `memory.oa_t11111111_p01a205f5.build_orders`、坏 SQL `failed` 且无输出、停机 503→恢复重试版本只 +1/execution 唯一、状态回写、负向 400/404/409、metrics/trace 无 tenant/凭据）；`actual`（单元/契约/迁移）：`evidence/2026-11-23-pipeline-publish-compensation-unit.md`、`go test ./internal/modules/pipelines/...`、`make test-race`；原始交接：`docs/stages/evidence/stage-2-pipeline-kestra-dbt/` |

## Requirement 映射

| Requirement | 场景 | 预期证据（planned） |
| --- | --- | --- |
| ADDED: Authoring DAG contract to Flow generation | 合法 DAG 生成确定性 Flow 定义 / 未知任务类型被拒绝 / 依赖环或缺失依赖被拒绝 / 重复任务标识被拒绝 / 不支持的 DAG 版本 | tasks 1.1–1.4、2.1 单测：DAG 校验表驱动用例与错误码断言；golden Flow YAML 与重复渲染哈希一致断言；负向用例 400 响应快照 |
| ADDED: Flow namespace and labels for tenant isolation | 不同租户的 Flow 相互隔离 / labels 携带版本元数据 / 跨租户执行查询被拒绝 | tasks 2.2–2.3、13.6：namespace 派生单测；labels 快照断言无凭据；跨租户读取/触发 404 集成证据 |
| ADDED: Flow publish and execution trigger | 同一 revision 幂等 upsert / 触发创建执行并持久化引用 / 触发重试接管已有执行 / 引擎校验错误映射 / 引擎超时或 5xx | tasks 3.1–3.3、5.1–5.4、6.1–6.2、12.2：`httptest` 契约测试；重试接管单测；集成执行引用落库查询 |
| ADDED: Publish and trigger idempotency | 相同键重放首次结果 / 相同键不同请求体 / 并发同键只生效一次 / 缺少幂等键 | tasks 10.1–10.4、13.6：并发幂等测试；引擎调用计数断言；409 错误体证据 |
| ADDED: Run status reconciliation and writeback | 成功执行回写 / 失败执行回写 / 未知状态不伪造成功 / 执行不存在 / 终态不回退 / 引擎不可达 | tasks 7.1–7.5、13.2–13.5：状态映射表驱动单测；CAS 并发测试；集成两次查询对比（停引擎仍 `succeeded`）；`execution_not_found` 证据 |
| ADDED: dbt project and model execution | 项目确定性生成 / 项目不含凭据 / 模型 schema 隔离 / dbt 执行失败 / dbt 测试失败 | tasks 8.1–8.5、13.2–13.3：dbt golden 与凭据扫描；Trino 派生 schema 表存在证据；坏 SQL `failed` 证据 |
| ADDED: Dataset outputs and lineage | 成功运行记录输出与血缘 / 输出写回原子性 / 重放不重复输出 / 输出 dataset 不存在 | tasks 9.1–9.4、13.2、13.6：`run_outputs`/`run_inputs` 查询结果；故障注入单测；400 响应证据 |
| ADDED: Retry, backoff and compensation | 可重试失败后退避重试 / 校验错误不重试 / 重试有界 / 发布失败补偿 / 触发中断接管 | tasks 3.4、5.3、6.3、12.2–12.4、13.4：注入 clock 的重试单测；补偿调用断言；503→恢复→成功且版本只递增一次的集成证据 |
| ADDED: Execution telemetry and failure sanitization | 出站调用遥测 / 对账任务遥测 / 错误结构一致 / 错误与日志脱敏 | tasks 11.1–11.3、13.6–13.7：`/metrics` 序列抓取；日志扫描断言无 token/DSN/SQL；统一错误体证据 |
| MODIFIED: Pipeline publishing and immutable versions | 首次发布 / 重新发布递增版本 / 归档拒绝发布 / 修改已发布定义回到草稿 / 发布时 DAG 校验失败 / Flow 登记失败 | tasks 5.1–5.4、1.4、12.3、13.4：service 单测（顺序与事务边界）；失败不推进版本断言；集成版本只递增一次证据 |
| MODIFIED: Pipeline run entry | 触发已发布 Pipeline / 草稿 Pipeline 拒绝触发 / adapter 失败 / 触发中途失败后恢复 | tasks 6.1–6.4、12.2、13.4：触发状态字段断言；重试接管单测；503 与 `failed` 记录证据 |
| MODIFIED: Pipeline run query | 查询运行列表 / 跨租户读取运行 / 查询时对账写回 / 引擎不可达时返回持久化状态 / 执行不存在 | tasks 7.3–7.4、13.5：分页排序回归；对账写回单测；停引擎查询 200 证据 |
| MODIFIED: Kestra adapter boundary | 默认内存 adapter / adapter 未配置 / 真实 adapter 访问 / 引擎不可达 fail-closed | tasks 3.1–3.5、4.4、12.4、13.4：模块构造单测（默认未配置 503）；`httptest` 请求断言；本地启动日志显示 adapter 配置状态 |
| MODIFIED: Dataset dependency validation | 引用不存在 dataset / resolver 未注入 / 输出 dataset 不存在 / 跨租户 dataset 引用 | tasks 9.1、13.6：resolver 未注入 503；不存在/跨租户 400 证据 |

## 未覆盖 / 后续

| 项 | 原因 | 后续归属 |
| --- | --- | --- |
| 运行状态的后台主动推进（无人查询的运行可能长期 `running`） | 模块接口无 Start/Stop 生命周期，后台 worker 需平台接线 | `stage-2-integration` 平台接线后另立 change（design D8、Open Questions 5） |
| proto `PipelineRun.execution_id` 暴露 | 需要 proto/gen/web 同步，本 change 保持 wire 不变 | 后续 change（design Open Questions 1） |
| 全站强制 `Idempotency-Key` | 跨 change 中间件，强制会破坏 `web/` 与其他 in-flight 调用方 | `stage-2-integration`（design D11、Open Questions 2） |
| Schedule → Kestra trigger 投影/调度执行 | 保持 Schedule 为本地投影，控制本 change 行为边界 | 后续 change（design Open Questions 3） |
| Pipeline 提交 Dataset 分支/事务 | `datasets` 模块当前只有 catalog 元数据，无提交 API | `datasets` owner 后续 change（design D10、Open Questions 6） |
| Kestra RBAC/多租户认证、生产 TLS、Helm/kind、Trino 权限与持久 catalog | Stage 2 非目标与 `production-delivery`/后续安全 change 范围 | `production-delivery` 与后续 change（design D4、D15、Open Questions 8–9） |
| 发布补偿遗留的孤儿 Flow/namespace 文件清理 | 需要运维清扫任务，超出本 change 范围 | `production-delivery` 运维任务（design D5、Risks） |
| connector→dataset→pipeline 闭环 E2E | 依赖并行 change `connector-runtime` | Wave 3 `stage-2-integration` |

## 实施证据（actual，2026-11-22）

- 证据文件：`evidence/2026-11-22-pipeline-unit.md`
- 命令：`GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./internal/modules/pipelines/...` 与 `go test ./...` 全绿；`go build ./...` exit 0。
- 覆盖：DAG v1、FlowIR/YAML、HTTPAdapter 契约、dbt 渲染、00002 迁移、repo CAS/血缘、service 发布/触发/对账单测。
- 未执行：真实 Kestra/Trino/dbt E2E、幂等中间件、指标/span、legacy run 收敛、发布 Flow 回滚；见 integration-patch.md 限制。

## 归档检查

- [ ] 所有 tasks 已勾选且有可复核证据（`planned` 全部转为 `actual` 或明确的未执行说明）。
- [ ] 至少一个真实实例集成测试（非仅 fake adapter），覆盖成功、失败重试与关键拒绝路径（change map §6、Wave 1 G1）。
- [ ] 未修改其他 change 目录与共享文件（`cmd/`、`internal/app/`、`deploy/`、`web/`、`go.mod`/`go.sum` 只提交 patch）。
- [ ] `openspec validate pipeline-kestra-dbt --strict` 通过，`openspec validate --all --strict` 通过。
- [ ] 证据已由 `stage-2-integration` 汇总到 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/` 且可复核。
- [ ] 无凭据、真实客户数据或生产连接信息进入仓库、日志与证据。
- [ ] 评审确认（change archive 门禁，不自动归档）。
