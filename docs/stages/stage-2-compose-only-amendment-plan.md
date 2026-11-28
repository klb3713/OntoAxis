# Stage 2「Compose-only + 交付层外移」合同修订草案

> 状态：**APPROVED（2026-11-28 负责人 klb3713 签核）** —— 已按 §3 / §8 落地。三项决策：① 确认「Compose-only 直到正式发布」为项目策略；② S2-AC-08 口径 **B（整条移出）**；③ **只新建 change，不建 Stage**（交付运行态验收由独立 change `delivery-runtime-acceptance` 承接）。
> 日期：2026-11-28（起草）。出处：治理提案（2026-11-25，用户提出）；本轮据最新 E2E 进展重拟。
> 关联：[stage-2-ontoaxis-runtime-brief.md](stage-2-ontoaxis-runtime-brief.md)、[stage-2-change-map.md](stage-2-change-map.md)、[stage-2-production-delivery-rescope-plan.md](stage-2-production-delivery-rescope-plan.md)、[stage-2-exit-report.md](stage-2-exit-report.md)。
> 依据 AGENTS.md §9 / RUNBOOK §2、§4：修改 AC、退出条件、Stage 状态必须负责人明确批准，不能自动改、自动归档或自动宣布。

## 1. 要决策的问题

项目功能尚未开发完成、正式发布尚远。建议：**正式发布之前，Stage 2 及后续功能阶段一律以 Compose 作为退出验证栈**；把 **Helm + ArgoCD + kind/k3d 的运行态验收**从 Stage 2 整体移出，与已拆出的 `production-idp-staging`、`production-delivery-hardening` 合并为一个**专门的「生产交付 / 发布就绪」Stage**，作为发布前的强制门。

## 2. 建议方案（TL;DR）

- **采用 Compose-only 作为功能阶段的退出验证栈**，直到进入「发布就绪」Stage；Helm/ArgoCD/kind/k3d 的部署形态验证在发布前一次性做严。
- **从 Stage 2 移出**：`stage-2-integration` 的 **6.1–6.5、7.7** 运行态验收；以及 `S2-AC-08` 中的运行态部分。
- **新建 Stage（建议名：Stage 3「生产交付与发布就绪」）** 承接：`production-delivery-hardening` + `production-idp-staging` + 上述 6.x/7.7 交付层验收。它 = 正式发布前的**强制门**。
- **S2-AC-08 走「选项 A」**：Stage 2 只保留「交付资产**静态可渲染**（helm lint / kubeconform 渲染门）+ 备份/恢复/回滚**脚本级**验证（Compose/本地）」，运行态 k8s 证据移入新 Stage。

## 3. Stage 2 合同修订（逐条提案文本）

| 文件 / 位置 | 现状 | 拟改为 | 影响 |
|---|---|---|---|
| [brief §2.1 :31](stage-2-ontoaxis-runtime-brief.md#L31) 必须交付 | "生产交付：Helm chart + ArgoCD Application……**Compose/kind 双环境**" | "生产交付：Helm chart + ArgoCD Application 资产**静态可渲染**；运行态 kind/ArgoCD 验收移交新 Stage；Stage 2 以 **Compose 单环境**完成 AC 的 E2E" | 去双环境硬要求 |
| [brief S2-AC-08 :65](stage-2-ontoaxis-runtime-brief.md#L65) | "生产交付资产与**本地 kind 证据**：……探针/PDB/Secret 配置生效；备份恢复与镜像回滚在本地 kind/演练集群演练通过" | "生产交付**资产静态可渲染**：Helm/ArgoCD 资产 lint+渲染门通过；探针/PDB/Secret/GitOps 同步与备份恢复、镜像回滚**运行态验收移交新 Stage**；备份/恢复/回滚**脚本在 Compose/本地可验证**" | 收缩为静态门+脚本验证 |
| [brief §8 退出条件 4 :129](stage-2-ontoaxis-runtime-brief.md#L129) | "Compose **与 kind 均能启动**并按 AC 完成 E2E（k3d 与生产 IdP 不在 Stage 2 退出判据）" | "**Compose 完成 Stage 2 AC 的 E2E**；kind/k3d/ArgoCD 运行态验收与生产 IdP 均移交新 Stage（不在 Stage 2 退出判据）" | 移除 kind 启动要求 |
| [brief §9 决策 5 :140](stage-2-ontoaxis-runtime-brief.md#L140) | "生产级 GitOps 门控、k3d、CI runner 与真实 staging IdP 后移 Stage 3" | 补："**kind 运行态验收（6.1–6.5、7.7）与本 change 的交付层复跑亦后移新 Stage**" | 决策口径一致 |
| [change-map §12](stage-2-change-map.md) | `production-delivery-hardening`、`production-idp-staging` 标注"Stage 3 候选/owner 待定" | 改为"**归属新 Stage（生产交付/发布就绪）**"，并补 row：从其承接 6.x/7.7 | 归属明确 |
| [exit-report §9](stage-2-exit-report.md) | 退出建议含"6.x/7.x 未完成" | 6.x/7.7 移出后不再作为 Stage 2 退出判据；仅保留 Compose 侧缺口（E2E-05/06/09、Playwright） | 退出判据收敛 |

> 注：`jaeger-v2` 迁移是独立事项（镜像 v1→v2），**不并入**本 Stage，除非负责人另定。

## 4. 交付运行态验收 change（**已改为 change，不建 Stage**）

> 2026-11-28 决策③：**不建 Stage**；交付运行态验收由新建 OpenSpec change `delivery-runtime-acceptance` 承接（`openspec/changes/delivery-runtime-acceptance/`）。以下原 Stage 草案内容仅作历史记录，**不再执行**。

- **目的**：把「交付资产」变成「在真实发布形态下可部署、可回滚、可运维」的发布就绪能力，作为正式发布的强制门。
- **交付（in）**：ArgoCD 真实 GitOps 源持续 Synced/Healthy；kind/k3d 全栈部署 E2E；Helm 安装/探针/PDB/Secret/资源限额生效；集群入口；备份恢复与镜像回滚演练；真实 staging IdP（非 localhost，JWKS/claims/登录）；CI runner 真实跑 delivery-kind、完整 bootstrap、烟测全绿；多副本探针/PDB。
- **不交付（out）**：新业务功能；生产云账号/生产密钥/真实客户数据（仍受 AGENTS §6 约束）。
- **候选 AC（草）**：
  - D-AC-01 ArgoCD 真实源（真实 `repoURL`/凭据）app-of-apps 持续 `Synced/Healthy`
  - D-AC-02 kind（及 k3d）全栈 E2E-01/E2E-02/E2E-10 通过
  - D-AC-03 Helm 安装 + 探针/PDB/Secret/资源限额生效（原 6.1）
  - D-AC-04 ArgoCD 同步（原 6.2）
  - D-AC-05 集群入口 public/受保护/未知/`/metrics`（原 6.3）
  - D-AC-06 备份→恢复演练（原 6.4）
  - D-AC-07 镜像回滚演练（原 6.5）
  - D-AC-08 真实 staging IdP 登录/JWKS/claims（原 `S2-AC-10` / 8.6）
  - D-AC-09 CI runner 真实 delivery-kind + 完整 bootstrap + 烟测全绿（原 5.4/5.8/9.1）
  - D-AC-10 多副本探针/PDB 验证
- **change 清单（草）**：`production-delivery-hardening`（承接 6.1–6.5、7.7、5.4/5.8/9.1 等），`production-idp-staging`（承接 8.6 / 原 AC-10）。如需拆分再立新 change。
- **退出条件（草）**：全部 change 归档 + 各 AC 有可复核证据 + 发布就绪门记录 + 负责人确认；不自动退出。
- **资源/归属（草）**：owner 待定；目录 `deploy/helm/`、`deploy/argocd/`、`deploy/kind/`、`deploy/values/`、`scripts/delivery-*`、`.github/workflows/`（delivery-kind）、`deploy/kind/tls/`、`deploy/values/staging-like.yaml`、`scripts/delivery-idp-check.sh`。

## 5. 任务 / 交付项迁移清单

> 2026-11-28 更新：6.1–6.5、7.7 的承接 change 由 `production-delivery-hardening` 改为新建的 `delivery-runtime-acceptance`；`production-delivery-hardening` 仅保留生产级加固。

| 项 | 现属 | 去向 | 备注 |
|---|---|---|---|
| 6.1 Helm 部署验收（pod Ready + probe） | `stage-2-integration` | `production-delivery-hardening` | 镜像可拉取环境 |
| 6.2 ArgoCD 同步 | 同 | `production-delivery-hardening` | 真实 GitOps 源 |
| 6.3 集群入口 | 同 | `production-delivery-hardening` | 依赖 6.1 |
| 6.4 备份恢复演练 | 同 | `production-delivery-hardening` | |
| 6.5 镜像回滚演练 | 同 | `production-delivery-hardening` | |
| 7.7 kind 栈 E2E 子集 | 同 | `production-delivery-hardening` | E2E-01/02/10 |
| 5.4/5.8 全量同栈启动、资源占用、真实 `--sync` 回滚、verify 正常退出、烟测全绿 | `stage-2-integration` / `production-delivery` | `production-delivery-hardening` | 已在 change-map §12 登记 |
| 8.6 / 原 `S2-AC-10` 真实 staging IdP | 已移 | `production-idp-staging` | 已在 change-map §12 登记 |
| Jaeger v1→v2 迁移 | — | `jaeger-v2`（独立） | 不并入本 Stage |

## 6. `production-delivery` 已归档的处理

- 该 change 已于 2026-11-24 按收缩范围归档 **70/70**（`openspec/changes/archive/...`）。**不重写归档件、不改历史**。
- 只在其**退出后注（post-exit note）**与 change-map §12 说明："kind 运行态验收（6.1–6.5、7.7）由新 Stage 的 `production-delivery-hardening` 承接"。
- Helm/ArgoCD/kind/k3d 的**资产与脚本原样保留在仓库**（Compose 仍是主开发路径），仅"验收归属"改到新 Stage。

## 7. 前提与风险（必须正视）

1. **Compose E2E 必须能顶住功能 AC**：截至 2026-11-28，E2E-01/02/03/04/07/08/12 已通过，但 **E2E-05（CDC）、E2E-06（dbt 产 dataset）、E2E-09（SLO/告警/基线）仍部分**。这些缺口分别关联 AC-06 / AC-05 / AC-09，移出 kind 后只能靠 Compose 证据，需先补齐，否则相应 AC 只能留在 partial / not passed。
2. **S2-AC-08 残留口径**：选项 A（静态渲染 + 脚本验证留 Stage 2）会让 Stage 2 不再有探针/PDB/Secret/GitOps 的运行态证据——这是接受的取舍，须明示。
3. **发布前强制门不能顺延**：新 Stage 必须固定为正式发布的前置门；功能阶段用 Compose 不得成为跳过部署验证的借口。
4. **安全边界不变**：默认拒绝、未注册路由 404、不提交密钥等（AGENTS §6）不受影响。
5. **分支**：当前工作树在 `web-design-tokens` 分支且含暂存的 `docs/design/`、`web/src/styles/` 改动；本草案与文档同步只改治理文档，未提交到任何分支。

## 8. 签核后要执行的文档同步清单

- [ ] 按 §3 修订 [stage-2-ontoaxis-runtime-brief.md](stage-2-ontoaxis-runtime-brief.md)（§2.1 / S2-AC-08 / §8 条件 4 / §9 决策 5）
- [ ] 更新 [stage-2-change-map.md](stage-2-change-map.md) §12 归属，并补迁移 row
- [ ] 更新 [stage-2-exit-report.md](stage-2-exit-report.md) §9：6.x/7.7 不再是 Stage 2 退出判据
- [ ] 新建新 Stage brief（status `draft`）+ change map
- [ ] 将 `stage-2-integration` 的 6.1–6.5、7.7 标 `deferred → 新 Stage`（保留 traceability 去向，不删）
- [ ] 同步 `openspec/config.yaml` context、`ARCHITECTURE.md` 15.x、README/路线图（"Compose-only 直到发布 + 独立交付 Stage"）

## 9. 签核清单（负责人填写，未勾 = 未批准）

- [x] 确认「Compose-only 直到正式发布」为项目策略（2026-11-28 负责人确认）
- [x] S2-AC-08 口径：**B（整条移出）**（2026-11-28 确认）
- [x] 不建 Stage，改为新建 change `delivery-runtime-acceptance`（2026-11-28 确认）
- [x] 授权按 §8 落地文档修订（仍不自动归档、不改 Stage 状态）
- 签字人：__________  日期：__________

## 10. 边界声明

本草案**不**包含：任何 AC 的实际批准、任何 Stage 状态变更、任何 change 归档、任何 commit。以上均需负责人明确指令后另行执行。
