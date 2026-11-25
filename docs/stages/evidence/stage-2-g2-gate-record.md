# Stage 2 · G2 门禁记录（草稿，待负责人确认）

- 起草日期：2026-11-25
- 起草人：`stage-2-integration`（自动起草，**非**批准）
- 门禁定义：`docs/stages/stage-2-change-map.md` §4「Wave 2 — 搜索与生产交付」门 G2
- 结论状态：**pass（2026-11-25，负责人 klb3713 确认登记）**；口径见下，登记记录已写入 `docs/stages/stage-2-change-map.md` §4

## 1. 门禁条件与证据

| G2 条件 | 证据 | 结果 |
| --- | --- | --- |
| outbox → Meilisearch 对象搜索 E2E 通过，增量更新与全量重建可复现 | object-search 隔离 Compose E2E `73 passed, 0 failed`、两次 `rebuild --verify` digest 一致（`docs/stages/evidence/stage-2-integration/object-search/2026-11-24-object-search-verification.md`）；CI `search-integration` 绿 | pass |
| Helm/ArgoCD 交付资产在本地 kind 完成部署，探针/PDB/Secret 生效（生产级 GitOps 门控移交 Stage 3） | `production-delivery` 归档证据 `evidence/stage-2-production-delivery-kind.md` 等（归属方 W2 归档 70/70）；**本 change 6.x 尚未在本轮复跑** | 依据归属方证据；本 change 复跑 pending |
| 备份恢复与镜像回滚演练通过 | production-delivery L1–L5 恢复与 5.9a/5.9b 回滚证据（归档 change evidence） | pass（归属方） |
| 生产 IdP 不在 Stage 2 范围 | 移交 `production-idp-staging` | out of scope（收缩口径） |
| 所有 Wave 1 服务的 compose/helm 配置可启动 | **compose 部分已通过**：真实 CI runner run [45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) 3/3（connector-runtime / pipeline-kestra / action-temporal，`docs/stages/evidence/stage-2-integration/wave1-stack-matrix.md`）；**helm 部分未验证**（属 6.x） | compose pass；helm pending |

## 2. 前置 change 归档状态

8 个前置 change 已全部归档（含 `object-search-meilisearch` 于 2026-11-25 归档：`openspec/changes/archive/2026-11-25-object-search-meilisearch`，主 spec `openspec/specs/object-search/spec.md`）。

## 3. 负责人确认结论（2026-11-25）

- 确认人：klb3713；结论：**pass**，附三条口径（compose 真实 runner 证据；helm/kind 采信 production-delivery 归档证据；本 change 6.x 复跑与 kind 全栈 E2E 仍属 Stage 2 退出条件 4）。
- 以下为起草时的待确认项，保留以便追溯：

1. 条件 5 的口径：是否接受「Wave 1 服务 compose 已真实启动、helm/kind 启动留待本 change 6.x」即登记 G2 `pass`，或要求先完成 6.x。
2. 条件 2 的口径：是否接受以 `production-delivery` 归属方 kind 证据作为 G2 第 2 条依据。
3. 确认后由负责人将结论填入 `docs/stages/stage-2-change-map.md` §4 G2 记录（日期、命令、结果、确认人），并把本文件状态改为 `pass`。

## 4. 边界

- 本记录不自动批准、不自动宣布 G2 pass、不改变 Stage 2 退出状态。
- 证据为本地/CI 合成数据环境，不代表生产验收。
