# stage-2-integration 未完成任务向 Stage 外 change 的迁移分析

- 日期：2026-11-25
- 输入：`openspec/changes/stage-2-integration/tasks.md`（34/56，未完成 22 项）、`docs/stages/stage-2-ontoaxis-runtime-brief.md` §4 AC、`docs/stages/stage-2-change-map.md` §4 G3/§12、`openspec/changes/production-idp-staging/*`、`openspec/changes/production-delivery-hardening/*`
- 结论：**只有 7.5（生产 IdP E2E）应整体迁移**；6.2 属「拆分门控深度」而非迁移；6.4/6.5/8.x 不建议迁移。其余 18 项是 Stage 2 退出（G3）判据，必须留在本 change。

## 1. 判定标准

1. Stage 2 退出（G3）要求「9 个模块装配不变，所有 Stage 2 AC 有证据；Compose 与 kind/k3d 均完成全栈 E2E；CI、契约、许可证、Playwright、负载基线全部通过；报告与证据归档」。凡服务于 S2-AC-01…09、S2-AC-11 的未完成项**不可移出**。
2. `stage-2-change-map.md` §12 已明确：`production-idp-staging` 承接 S2-AC-10（**已移出 Stage 2**），`production-delivery-hardening` 承接 `production-delivery` 移出项；**两者均不阻塞 Stage 2 退出**。
3. S2-AC-08 的括号界定：「生产级 GitOps/k3d/CI 验证移交 Stage 3」→ Stage 2 只保留「可渲染/同步 + 探针/PDB/Secret + 本地 kind 备份恢复与镜像回滚」。
4. 迁移必须同时满足：目标 change 的 `Why/What Changes` 已覆盖该行为；迁移后 Stage 2 AC 证据链不出现空洞。

## 2. 未完成项逐条判定（22 项）

| # | 任务 | 对应 AC / 门 | 判定 | 去向 / 理由 |
|---|---|---|---|---|
| 1 | 1.1 G2 `pass` 记录 | G2 准入 | 保留 | 门禁记录只能由负责人登记，不可转移 |
| 2 | 2.5 `pipeline-kestra-dbt` 合并验证 | S2-AC-05 | 保留 | Stage 2 核心 AC；归档证据已有，仅缺组合复跑 |
| 3 | 5.4 Wave 1/2 同栈 profile | AC-04/05/06/07 | 保留 | 属 G3「Compose 全栈 E2E」前置 |
| 4 | 6.1 Helm 部署本地 kind（probe/PDB/Secret） | S2-AC-08 正文 | 保留 | AC-08 明确要求本地 kind 生效；仅「环境缺镜像」是执行障碍，不是范围问题 |
| 5 | 6.2 ArgoCD 同步 | S2-AC-08（生产级门控已移交） | **保留 + 拆分引用** | 本 change 只验基础 `Synced/Healthy`；「wave 健康门控/空集群顺序推进」已在 `production-delivery-hardening` 1.1，无需迁移 |
| 6 | 6.3 集群入口 Envoy 路由 | AC-02/AC-08 | 保留 | Stage 2 行为验收 |
| 7 | 6.4 备份恢复演练 | S2-AC-08 正文 | 保留 | 本地 kind 演练是 AC-08 明确判据；hardening 不重复承接 |
| 8 | 6.5 镜像回滚演练 | S2-AC-08 正文 | 保留 | 同上；hardening 1.2 是「真实 `--sync` GitOps 回滚」的更深版本，不替代 |
| 9 | 7.1 Compose 基础链路 E2E-01/08 | AC-01/AC-11 | 保留 | G3 |
| 10 | 7.2 Compose 数据链路 E2E-04/05/06/07 | AC-04/05/06/07 | 保留 | G3 |
| 11 | 7.3 Compose 身份/授权 E2E-02/03/12 | AC-02/AC-03 | 保留 | G3 |
| 12 | 7.4 Compose 可观测 E2E-09 | AC-09 | 保留 | G3 |
| 13 | **7.5 生产 IdP E2E-11** | **S2-AC-10（已移出）** | **迁移** | 与 `production-idp-staging` 2.1/2.2 完全同源；该 change 的验收即「非 localhost issuer/JWKS/claims 登录 + 负向矩阵」。建议在该 change 新增 `4.1 执行 E2E-11（登录→API→claims 断言）`，Stage 2 本项标记「out of scope（AC-10 移交）」 |
| 14 | 7.6 前端 E2E（Playwright） | AC-01 | 保留 | G3 |
| 15 | 7.7 kind 栈 E2E 子集 | AC-08/AC-11 | 保留 | G3 要求 kind 全栈 |
| 16 | 8.1 Go 质量门（含 golangci-lint） | AC-01/AC-11 | 保留（改用 CI 证据） | 工具缺口属环境；`build-test` CI 已覆盖，建议标注「以 CI 为准」而非迁移 |
| 17 | 8.3 契约门（buf lint/breaking/generate-check） | AC-01/AC-11 | 保留（改用 CI 证据） | `proto` CI job 已绿；本机无 buf，同样以 CI 为准 |
| 18 | 8.4 脚本与配置门（actionlint/shellcheck） | AC-01/AC-11 | 保留（可选 CI 补装） | 属仓库通用质量门，不是交付硬化专属；迁移会削弱 Stage 2 退出证据。可在 CI `delivery-static` 中补装工具，但任务仍归本 change |
| 19 | 8.5 OpenSpec 门 | AC-11 | 保留 | 需改口径：以「8 个前置 change 归档」为准，Stage 外 change（jaeger-v2/hardening/idp）不计入 |
| 20 | 9.4 文档同步 | AC-11 | 保留 | README/ARCHITECTURE/local-development 状态同步是 Stage 2 收口动作 |
| 21 | 9.5 traceability 自检 | AC-11 | 保留 | 证据一致性 |
| 22 | 10.1 负责人评审结论 | 收口 | 保留 | 不可转移 |

## 3. 结论与建议动作

**P1（建议执行）**：迁移 `7.5` 到 `production-idp-staging`
- 在 `openspec/changes/production-idp-staging/tasks.md` 新增 `## 4. E2E 与验收`：`4.1 执行 E2E-11：非本地 OIDC 登录→API 调用→claims（tenant-id/org-ids/workspace-ids/realm-roles）断言，证据归档`；`4.2 将 S2-AC-10 证据映射到本 change traceability`。
- `stage-2-integration/tasks.md` 将 7.5 改为 `- [x] 7.5 生产 IdP：**out of scope**（S2-AC-10 已移出 Stage 2，由 production-idp-staging 4.x 承接）`；总数 56→55，完成计数保持 34。
- 同步 `traceability.md` S2-AC-10 行与 `docs/stages/evidence/stage-2-integration.md` 的 AC 表（标注移交，不计入 Stage 2 退出判据）。

**P2（建议记录，不迁移）**：
- `6.2` 注明「基础同步归本 change；wave 健康门控归 `production-delivery-hardening` 1.1」。
- `6.4/6.5` 注明「hardening 1.2 为生产级 `--sync` 回滚，本 change 只做本地演练」。
- `6.1–6.5` 的执行前置标注 `production-delivery-hardening` 2.1（离线完整 bootstrap）/3.1（running-images kind-load）——属**依赖关系**，不是任务迁移。
- `8.1/8.3/8.4` 统一「以 CI job 证据为准，本机工具缺失不作为未完成」。

**P3（不建议）**：
- 不要把 6.x 或 7.x 整体移入 `production-delivery-hardening`：它是 Stage 3 候选、owner 待定、且 §12 明确「不阻塞 Stage 2 退出」；移出会造成 AC-08/AC-11 证据空洞，等于用范围收缩掩盖未验证。
- 不要把 8.x 质量门移入 hardening 3.x：会削弱 Stage 2「CI/契约/许可证/Playwright/负载基线全部通过」的判据。

## 4. 影响评估

| 项 | 迁移前 | 迁移后 |
|---|---|---|
| stage-2-integration 任务总数 | 56 | 56（7.5 保留为移交标注 `[x]`；35 完成、21 未完成；有效判据仍为 34 项完成） |
| Stage 2 退出判据 | AC-01…09、AC-11 | 不变（AC-10 本已移出） |
| production-idp-staging 任务 | 7 项 | +2（4.1/4.2） |
| production-delivery-hardening | 11 项 | 不变 |

## 5. 风险与审批

- 迁移 7.5 需负责人确认，并同步 Stage 2 brief 的 AC-10 注记（已注明移出，只需在 tasks/traceability 体现）。
- **执行状态（2026-11-25）**：负责人确认后已执行 P1——`production-idp-staging/tasks.md` 新增 §4（4.1/4.2）、`stage-2-integration/tasks.md` 7.5 标记 out of scope（保留行以便追溯）、`traceability.md` 与证据索引同步；未完成项 22→21，实际执行完成 34 项（7.5 不计入）。P2 仅建议记录，未改归属。
