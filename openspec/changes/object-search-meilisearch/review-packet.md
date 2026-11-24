# 评审材料包索引（object-search-meilisearch · 9.x）

> 日期：2026-11-24。本文件是**材料包封面**，说明 9.1/9.2/9.3 提交评审的内容、待决问题与**尚未 ready 的项**；不构成任何评审通过或签字。

## 1. 提交评审的三份材料

| 任务 | 材料 | 性质 |
| --- | --- | --- |
| 9.1 运维手册 | [review-ops-runbook.md](review-ops-runbook.md) + [docs/operations/object-search-runbook.md](../../../docs/operations/object-search-runbook.md) | 手册 + 评审检查表；命令本地复现已完成 |
| 9.2 安全与数据治理 | [security-data-governance-review.md](security-data-governance-review.md) | 事实清单 S1–S15 + 7 个待决问题 + 风险 R-1..R-4 + §4 结论表（已评审通过）+ §6 处置记录 |
| 9.3 change 评审与 Wave 2 准入 | [review-wave2-admission.md](review-wave2-admission.md) | 自评审清单 C1–C10 + G1 引用 + 准入待办 + 签字表（待填） |

## 2. 支撑证据

- [evidence/2026-11-24-object-search-verification.md](evidence/2026-11-24-object-search-verification.md)：后端集成 12 项、原生 HTTP 68/68、隔离 Compose E2E 73/73。
- [evidence/2026-11-24-search-integration-ci-local.log](evidence/2026-11-24-search-integration-ci-local.log)：CI 同参本地实跑（exit 0、0 skip）。
- [evidence/2026-11-24-object-search-runbook-repro.md](evidence/2026-11-24-object-search-runbook-repro.md)：手册命令复现。
- [evidence/2026-11-23-object-search-integration.md](evidence/2026-11-23-object-search-integration.md)：真实 PG+Meili 集成与缺陷修复。
- [traceability.md](traceability.md)：需求→证据映射。

## 3. 评审人需决定的重点

- 9.1：单类型原地修复语义（D15）、告警阈值、旧索引保留一版是否满足回退 SLA。
- 9.2：7 个待决问题（S12 敏感键覆盖、S13 properties_text 可见性、S7/R10 对象级授权缺失、S10 删除流程、S11/S15 备份驻留、S14 日志、S5 索引名哈希）；R-1..R-4 风险是否接受。
- 9.3：C3 单类型语义、C7 交接边界、R10 搜索整栈证据；确认后方可进入 change 评审签字。

## 4. 尚不 ready / 不能勾选的项（评审须知）

- **9.1/9.2/9.3 已于 2026-11-24 由评审人 klb3713 评审通过并勾选**（结论见各材料 §4/§5/§6 签字表）。
- 8.2：缺 GitHub Actions run 链接（本地同参已通过）。
- 7.5/7.6：主 `compose.yaml` 合并、Helm chart 与 kind 评审归 `production-delivery`（见 [handoff/production-delivery.md](handoff/production-delivery.md)）。
- 8.4：`docs/stages/evidence/` 收录归 `stage-2-integration`（见 [handoff/stage-2-integration.md](handoff/stage-2-integration.md)）。
- G2 项（kind/k3d 部署、备份恢复、镜像回滚、生产 IdP）均 `not executed`，属 Stage 2 / Wave 2，不在本材料包。

## 5. 禁止

不得代签、不得把 skip 写成 pass、不得用本地结果冒充 CI/kind、不得在业务阈值未签核时判业务合格。
