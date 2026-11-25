# stage-2-integration · 评审材料包（10.1 提交用）

- 状态：`submitted for owner review`（**未**自动批准；评审结论由负责人记录）
- 日期：2026-11-25
- 分支/PR：`stage2-int/d07-osm-evidence` / 草稿 PR #3（https://github.com/klb3713/OntoAxis/pull/3）

## 1. planning artifacts

| 文件 | 用途 |
| --- | --- |
| `openspec/changes/stage-2-integration/proposal.md` | 范围、目标、非目标 |
| `openspec/changes/stage-2-integration/design.md` | 决策（D1–D15）、证据规则、报告结构、回滚 |
| `openspec/changes/stage-2-integration/tasks.md` | 56 项任务与进展（当前 34/56） |
| `openspec/changes/stage-2-integration/traceability.md` | S2-AC-01…11 映射 + 2026-11-25 状态更新表 |

## 2. 证据与报告

| 产物 | 路径 |
| --- | --- |
| 证据索引 | `docs/stages/evidence/stage-2-integration.md` |
| 原始目录 | `docs/stages/evidence/stage-2-integration/`（`regression/`、`object-search/`、`baseline/`、`defects/`、`d07-port-conflict.md`、`security-check.md`、`wave1-stack-matrix.md`、`kind-cluster-state.md`） |
| 退出报告草稿 | `docs/stages/stage-2-exit-report.md`（`exit candidate pending owner confirmation`） |
| G2 门禁记录草稿 | `docs/stages/evidence/stage-2-g2-gate-record.md`（`draft — pending owner confirmation`） |

## 3. 本轮（2026-11-25）关键结果

- D-07 端口冲突修复（Kafka Connect 18086→18089；search worker `ONTOAXIS_SEARCH_WORKER_PORT` 参数化；`services-and-ports.md` 补登 5 项）。
- Wave 1 分组启动矩阵在真实 CI runner 通过：run [45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) 3/3（connector-runtime / pipeline-kestra / action-temporal）。
- `object-search-meilisearch` 归档为 `2026-11-25-object-search-meilisearch`（52/52）。
- 完整 CI 主干全绿：[45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 11/11。
- `openspec validate --all --strict`：21 passed / 0 failed。

## 4. 未完成 / 阻塞（评审须知）

| 项 | 状态 |
| --- | --- |
| G2 登记 | 条件 5 的 compose 启动已通过，helm/kind 待 6.x；**待负责人确认口径** |
| 2.5 pipeline-kestra-dbt 合并验证 | 归档 69/69 已有真实 E2E；本轮组合复跑待有镜像环境 |
| 5.4 全量同栈启动 | CI 分组矩阵已过；一次性同栈 + 资源占用未记录 |
| 6.1–6.5 kind 交付层 | 阻塞：集群多数工作负载 `ImagePullBackOff`（13）、ArgoCD 无 Application（`kind-cluster-state.md`） |
| 7.1–7.7 全栈 E2E | 未执行（受镜像/环境阻塞） |
| 8.1/8.3/8.4 部分门 | golangci-lint/actionlint/shellcheck/buf 本机缺失；以 CI 或标未执行 |
| 8.5 OpenSpec 门 | 有范围外在途 change，不构成退出门通过 |
| 9.4 文档同步 | 端口表与 change map 已更新；ARCHITECTURE 15.x/README/local-development 待 owner 同意 |
| S2-AC-05 | `not passed`（2.5/E2E-06 未完成） |

## 5. 评审请负责人确认

1. G2 是否登记 `pass`（及其条件 5/2 口径）。
2. 6.x/7.x 是否必须在本 Stage 完成，或接受 `passed-with-caveat` 收口并在 Stage 3/后续补。
3. 退出报告是否按当前 `exit candidate pending owner confirmation` 状态接受，或退回补充。

> 本材料包不自动批准、不自动归档、不自动宣布 Stage 2 退出。
