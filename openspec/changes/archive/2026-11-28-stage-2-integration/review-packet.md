# stage-2-integration · 评审材料包（10.1 提交用）

- 状态：`submitted for owner review`（**未**自动批准；评审结论由负责人记录）
- 日期：2026-11-28
- 分支/PR：`stage2-int/d07-osm-evidence` / 草稿 PR #3（https://github.com/klb3713/OntoAxis/pull/3）

## 1. planning artifacts

| 文件 | 用途 |
| --- | --- |
| `openspec/changes/stage-2-integration/proposal.md` | 范围、目标、非目标 |
| `openspec/changes/stage-2-integration/design.md` | 决策（D1–D15）、证据规则、报告结构、回滚 |
| `openspec/changes/stage-2-integration/tasks.md` | 56 项任务与进展（当前 54/56；剩 7.2、10.1） |
| `openspec/changes/stage-2-integration/traceability.md` | S2-AC-01…11 映射 + 2026-11-28 状态更新 |

## 2. 证据与报告

| 产物 | 路径 |
| --- | --- |
| 证据索引 | `docs/stages/evidence/stage-2-integration.md` |
| 原始目录 | `docs/stages/evidence/stage-2-integration/`（含 `pipeline-dbt-7.2.md`、`connector-runtime-7.2.md`、`observability-7.4.md`、`playwright-7.6.md`、`wave1-stack-local.md`、`baseline/admission.md` 等） |
| 退出报告 | `docs/stages/stage-2-exit-report.md`（`exit candidate pending owner confirmation`） |
| G2 门禁记录 | `docs/stages/evidence/stage-2-g2-gate-record.md`（pass，2026-11-25） |
| G3 门禁记录 | `docs/stages/evidence/stage-2-g3-gate-record.md`（技术候选通过，待负责人确认） |

## 3. 关键结果（2026-11-28 最新）

- **E2E-06（Kestra + dbt → dataset）**：本地 Compose 11/11 通过（`scripts/pipeline-dbt-e2e.sh`；publish/run/status/succeeded + Trino `memory` 查询 `answer=42`），证据 [`pipeline-dbt-7.2.md`](../../../docs/stages/evidence/stage-2-integration/pipeline-dbt-7.2.md)；CI `e2e-pipeline-dbt` job 已新增，待真实 runner 跑绿。
- **E2E-09（可观测性）**：修复 Prometheus `rule_files` 路径 bug；promtool 规则测试 SUCCESS；`observability-smoke` PASS（3 metric series、1 trace、Perses dashboard、12 rules）；负载基线 300 请求/20 并发，0 失败、974.74 req/s、p95 60.899ms。证据 [`observability-7.4.md`](../../../docs/stages/evidence/stage-2-integration/observability-7.4.md)。
- **Playwright 7.6**：`pnpm test:e2e` 17/17 通过（core-workbench 13 + approvals 4，mock 模式）。证据 [`playwright-7.6.md`](../../../docs/stages/evidence/stage-2-integration/playwright-7.6.md)。
- **E2E-05（connector-runtime）**：本地隔离栈 **23/23** 通过（负向矩阵 16/16 + 成功路径 5/5：source→Kafka Connect→Iceberg sink→Trino 查询，5 行一致）；证据 [`connector-runtime-7.2.md`](../../../docs/stages/evidence/stage-2-integration/connector-runtime-7.2.md)。
- **CI 主干**：[45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 11/11（含 `proto` buf lint/generate-check/breaking）。
- `openspec validate --all --strict`：22 passed / 0 failed（22 items）。

## 4. 未完成 / 阻塞（评审须知）

| 项 | 状态 |
| --- | --- |
| 7.2 全栈 E2E | E2E-04/07 已过；E2E-06 本地 11/11；E2E-05 本地通过；E2E-05 真实 CI `e2e-connector` 仍为 opt-in（Iceberg 插件扫描 >45 分钟，默认 skipped） |
| 6.1–6.5、7.7、S2-AC-08 | 2026-11-28 已整条移交 `delivery-runtime-acceptance`，**不计 Stage 2 退出判据** |
| 5.4 | 本地分组启动与资源快照已补齐（`wave1-stack-local.md`） |
| 10.1 评审 | 本材料包待负责人结论（G3 / 退出 / 归档） |
| AC caveat | AC-01/02/03/04/06/07/09 建议按 `passed-with-caveat` 口径逐项确认；AC-05/AC-09 已本地 Compose 通过 |

## 5. 评审请负责人确认

1. G3 技术条件是否登记 `pass`（见 `stage-2-g3-gate-record.md`）。
2. 是否接受 Stage 2 以 Compose-only 口径退出（kind/k3d 由 `delivery-runtime-acceptance` 发布前承接）。
3. 是否批准归档 `stage-2-integration` 并更新 Stage 2 退出报告状态。

> 本材料包不自动批准、不自动归档、不自动宣布 Stage 2 退出。
