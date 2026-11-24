# 交接待办清单：object-search-meilisearch → production-delivery / stage-2-integration

- 生成日期：2026-11-24（object-search-meilisearch 评审通过后）
- 状态基线：object-search-meilisearch **49/52**，未勾 7.5 / 7.6 / 8.4（8.2 已于 2026-11-24 由真实 CI run 关闭）；未归档、未 commit。
- 归属依据：`docs/stages/stage-2-change-map.md` 行 116–123（`deploy/` 归 production-delivery；`docs/stages/evidence/` 归 stage-2-integration）。
- 详细内容见：[production-delivery.md](production-delivery.md)、[stage-2-integration.md](stage-2-integration.md)。

## A. 给 production-delivery（对应 object-search 7.5 / 7.6）

| # | 待办 | 验收命令 / 证据 | 回填 |
| --- | --- | --- | --- |
| A1 | 合并 `search` profile 片段到 `deploy/compose/compose.yaml`（meilisearch、search-worker、core 的 `ONTOAXIS_SEARCH_*` 与 `depends_on.search-worker{required:false}`、卷 `search_data`） | `docker compose -f deploy/compose/compose.yaml --profile search config -q` → exit 0；`up -d` 后两者 healthy | object-search 7.5 |
| A2 | 依 `deployment-requirements.md` 创建 search chart（Deployment/StatefulSet、Service、PVC、Secret、探针、资源、ArgoCD wave、迁移/重建 Job、回滚） | `helm template` 渲染通过 + kubeconform 通过 | object-search 7.6 |
| A3 | kind 部署评审 Meilisearch 与 worker | `kubectl get pods` Ready；探针/资源/PDB/Secret 与 chart 一致；评审记录入库 | 证据索引 |
| A4 | 完成后通知 object-search 负责人回填 7.5/7.6 勾选与证据链接 | — | 7.5 / 7.6 |

已登记位置：`openspec/changes/production-delivery/tasks.md` §11（11.1–11.4）。

## B. 给 stage-2-integration（对应 object-search 8.2 / 8.4 与评审记录）

| # | 待办 | 验收 / 证据 | 回填 |
| --- | --- | --- | --- |
| B1 | 收录证据源文件到 `docs/stages/evidence/`（集成记录、隔离验证、CI 本地日志、手册复现） | 索引内路径真实存在、抽查可复跑 | object-search 8.4 |
| B2 | ~~回填 GitHub Actions `search-integration` run 链接~~ **已完成 2026-11-24**：[run 45405474349](https://github.com/klb3713/OntoAxis/actions/runs/45405474349) job [138484907178](https://github.com/klb3713/OntoAxis/actions/runs/45405474349/job/138484907178) success（草稿 PR #1，分支 `ci/object-search-8.2`）；Stage 索引只需引用该链接 | CI job 绿 + run URL | object-search 8.2 |
| B3 | 记录 9.1/9.2/9.3 评审签字（klb3713，2026-11-24）到 Stage 证据索引 | 附各材料 §4/§5/§6 链接 | 证据索引 |
| B4 | 证据边界声明：本地合成验证 ≠ CI/生产验收；业务质量阈值仍 `pending-owner-signoff` | 报告/索引中可见声明 | 报告 §8 |

已登记位置：`openspec/changes/stage-2-integration/tasks.md` §11（11.1–11.4）。

## C. 不在本次交接范围（G2 / Stage 退出）

- G2（kind/备份/回滚/生产 IdP）与 Stage 2 退出由负责人按 `docs/stages/stage-2-brief` 与 change-map 处理，不属于本清单。
- 本清单内的勾选须由各归属方在完成并具备证据后进行；object-search-meilisearch 不代勾、不代签、不自动归档。
