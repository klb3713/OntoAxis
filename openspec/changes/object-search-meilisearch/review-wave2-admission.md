# change 评审与 Wave 2 准入核对（object-search）

> 对应任务 **9.3**。本文件记录 change 评审与 Wave 2 准入核对；评审结论见 §6 签字表（2026-11-24，评审人 klb3713 通过）。
> G1 已由负责人于 2026-11-24 [登记为 `pass`](../../../docs/stages/evidence/stage-2-g1-gate-record.md)。本轮实施仅用隔离本地环境与合成数据；G2 仍未执行。

## 1. 门禁定义（摘录）

- **G1（Wave 1 门，进入 Wave 2 的前置）**：每个 Wave 1 change 的真实实例在 Compose profile 或 kind 中启动；
  成功路径、失败重试、拒绝路径均有集成测试；外部实例不可用时 adapter 降级行为明确且不冒充成功；
  授权 change 的跨租户/越权测试全部拒绝。
- **G2（Wave 2 门）**：outbox → Meilisearch 对象搜索 E2E 通过，增量更新与全量重建可复现；Helm/ArgoCD 在
  kind/k3d 完成部署，探针/PDB/Secret 生效；备份恢复与镜像回滚演练通过；生产 IdP 使用非本地 OIDC issuer
  完成登录与 token 校验；所有 Wave 1 服务的 compose/helm 配置可启动。

## 2. G1 前置核对

| G1 要素 | 当前事实 | 结论 |
| --- | --- | --- |
| Wave 1 三个 change 真实实例启动 | 负责人已在 [G1 记录 §1–2](../../../docs/stages/evidence/stage-2-g1-gate-record.md#1-门禁条件与证据) 确认并列出证据 | 引用已登记结果 |
| Wave 1 集成/失败/拒绝与降级 | 同一 G1 记录列出三项 change 的验证与归档证据 | 引用已登记结果 |
| 授权 change 跨租户/越权负向测试 | `authorization-openfga-cedar` 已归档，G1 引用其 29 条负向用例；不等于搜索 R10 已完成端到端验收 | G1 已满足；搜索单独取证 |
| change 实施范围 | 2026-11-24 会话确认，仅隔离本地环境与合成数据 | 仅适用于本轮验证 |

**结论：G1 已登记；当前仅执行确认范围内的本地搜索验证。** change 评审、安全评审和 G2 保持待办。

## 3. change 自评审清单

| # | 评审项 | 结果 | 证据/说明 |
| --- | --- | --- | --- |
| C1 | 所有 9.x 之外的未完成任务是否有真实证据 | PostgreSQL/Meilisearch 集成与重建 CLI 已实跑；8.1 隔离 Compose E2E 73/73 通过（2026-11-24 21:07，环境恢复后）；8.2 真实 CI run [45405474349](https://github.com/klb3713/OntoAxis/actions/runs/45405474349) 的 `search-integration` job success（集成测试 + 检索回归门，0 SKIP；本地日志 [2026-11-24-search-integration-ci-local.log](evidence/2026-11-24-search-integration-ci-local.log)）；8.3 工程门已签、业务阈值未签核 | [2026-11-24 隔离验证](evidence/2026-11-24-object-search-verification.md) |
| C2 | 实现是否与 design 一致 | 基本一致；**发现并修复** `--object-type` 重建会覆盖整租户索引的缺陷（`rebuild.go`） | 集成测试 `TestIntegrationRebuildObjectTypePreservesOtherTypes` |
| C3 | spec delta 与实现是否一致 | R10 已接入真实守卫并校验租户内类型事实，原生 HTTP 与隔离 Compose E2E 矩阵均已通过；单类型重建语义已由 `design.md` D15 决策并在 spec delta 拆为「租户级全量重建收敛」与「单类型修复收敛」，规格与实现一致 | 见 `traceability.md`、`design.md` D15、`specs/object-search/spec.md` |
| C4 | 测试是否真实（无 fake 冒充） | 集成测试使用真实 PostgreSQL + Meilisearch；未注入变量时 skip，CI 强制要求（`ONTOAXIS_SEARCH_TEST_REQUIRED=1`） | `internal/platform/search/integration_test.go` |
| C5 | 是否删除/放宽断言 | 未删除；`--object-type` 修复增加了更严格的跨类型断言 | diff |
| C6 | 是否修改受限共享文件 | 未改 `internal/app/`、`cmd/ontoaxis-core/`、`deploy/compose/compose.yaml`、`deploy/helm/`、`deploy/argocd/`、`go.mod/go.sum` | `git status` |
| C7 | 交接项是否明确 | Compose `search` profile 需 production-delivery 合并进 base compose；Helm/ArgoCD 要求见 `deployment-requirements.md`；`internal/app` 装配 patch 见 `integration-patch.md` | 本文件 §4 |
| C8 | 证据是否可复核 | 证据文件含命令、时间、退出码、关键输出摘要；未执行项写 `not executed` | `evidence/` |
| C9 | 安全/数据治理评审 | 已完成（2026-11-24 评审人 klb3713，见 [security-data-governance-review.md](security-data-governance-review.md) §4/§6）；业务质量门保持 pending | **已满足**（9.2） |
| C10 | 中文评测阈值 | 工程回归门 `engineering_gate` 已签并接 CI `--strict`；业务阈值（recall@5≥0.9、MRR@10≥0.8）仍 `pending-owner-signoff` | **不得判定业务合格** |

> 9.1/9.2/9.3 的提交材料与待决问题汇总见 [review-packet.md](review-packet.md)；9.1 手册检查表见 [review-ops-runbook.md](review-ops-runbook.md)。

## 4. Wave 2 准入待办（进入实施/合并前）

1. **G1 记录**：已由负责人登记，见 §2；本材料不代签或重开门禁。
2. **change 评审**：✅ 2026-11-24 已完成（评审人 klb3713，见 §6）；核对 C1–C10 与证据，确认单类型重建语义、C7 交接边界与 R10 搜索整栈证据。
3. **共享文件合并**：
   - `deploy/compose/compose.yaml` 增加 `search` profile（片段已在 overlay 就绪，见 `compose.search.yaml`）；
   - `production-delivery` 按 `deployment-requirements.md` 创建 Helm/ArgoCD 资源；
   - `stage-2-integration` 合并 `internal/app/` 装配 patch。
4. **安全评审**：✅ 2026-11-24 已完成 7 个待决问题的处置（见 `security-data-governance-review.md` §4/§6）；S12/S13/S10/S11/S15 等细化登记后续 change。
5. **CI 证据**：在 GitHub Actions 实跑 `search-integration` job 并附 run 链接；本地同命令已通过，但不能替代 CI 记录。
6. **阈值签核**：负责人签核 `fixtures/search/dataset.json` 的阈值与评审结论；签核前不得写“评测通过/业务合格”。
7. **G2 材料**：kind/k3d 部署、备份恢复、镜像回滚、生产 IdP 验证属 Wave 2/G2，当前均 `not executed`。

## 5. 当前可准入结论

| 结论项 | 值 |
| --- | --- |
| G1 | **已登记通过**，见 §2 |
| change 评审 | **已评审通过**（2026-11-24，评审人 klb3713，见 §6） |
| 本轮实施 | **限隔离本地与合成数据验证** |
| 仍然待办 | 7.5/7.6 合并、8.4 收录、G2（kind/备份/回滚/生产 IdP）；8.2 已由真实 CI run 关闭 |
| 禁止的动作 | 代签评审、把 skip 写成 pass、用本地结果冒充 CI/kind、在阈值未签核时判业务合格 |

## 6. 签字（2026-11-24 已填）

| 角色 | 姓名 | 结论 | 日期 |
| --- | --- | --- | --- |
| change 评审人 | klb3713 | 通过（核对 C1–C10 与证据；确认 D15 单类型语义、C7 交接边界、R10 搜索整栈证据） | 2026-11-24 |
| 数据治理/安全评审人 | klb3713 | 通过（见 [security-data-governance-review.md](security-data-governance-review.md) §4/§6；业务质量门保持 pending） | 2026-11-24 |
| 负责人（G1/G2 登记） | klb3713 | G1 已登记 pass（2026-11-24）；G2 未执行 | 2026-11-24 |