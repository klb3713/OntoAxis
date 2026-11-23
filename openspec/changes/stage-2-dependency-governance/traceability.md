# Traceability: stage-2-dependency-governance

## 1. 与 Stage 2 的关系

本 change 是 Stage 支撑 change，不新增 Stage AC。它落实 `docs/stages/stage-2-change-map.md`
§10 评审结论第 3 条（「OpenFGA、Kestra、Temporal、Meilisearch 固定具体稳定 patch 并记录镜像
digest，不使用 latest」），并为 S2-AC-08、S2-AC-11 提供依赖证据。

## 2. Stage AC → requirement/scenario → 证据

| Stage AC | 支撑方式 | requirement/scenario | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-08 生产交付 | Helm 侧真实 digest 与 tag 的可审计前提 | `镜像引用必须可复现` / 「latest 被门禁拒绝」；`例外登记与到期失效` / 「占位 digest 不冒充已修复」 | `evidence/2026-11-23-check.txt`、`evidence/2026-11-23-check-negative.txt`；Helm 回填移交 6.1 | 部分（Compose 已覆盖，Helm 待 production-delivery 回填） |
| S2-AC-11 证据与报告 | 月度审计报告可归档；CI 门禁可复核 | `审计报告与阶段证据` / 「审计输出可归档」 | `evidence/2026-11-23-deps-audit.md`、`.github/workflows/deps.yml` | verified（本地运行），CI 首次执行待 push 后观察 |
| S2-AC-05 Pipeline（Kestra） | Kestra 2.0.12 明确写入策略与锁，避免 latest 漂移掩盖端点不兼容 | `组件分级与责任归属` / 「新增组件必须分级」 | `deploy/deps/policy.yaml:39`、`deploy/deps/lock.yaml` 中 kestra 条目 | verified |

## 3. Requirement → 实现 → 验证

| Requirement | 实现 | 验证 |
| --- | --- | --- |
| 单一真源与可 diff 锁 | `deploy/deps/policy.yaml` + `deploy/deps/lock.yaml`（`scripts/deps.py lock` 生成） | 连续两次 `make deps-lock` 无 diff；`make deps-check` 比对锁与仓库 |
| 镜像引用必须可复现 | `scripts/deps.py check` 的 E_LATEST / E_NOTAG / W_FLOATING / digest 棘轮 | 负向用例 1：`:latest` 触发 E_LATEST + E_LOCK_STALE，退出码非 0 |
| 组件分级与责任归属 | `policy.yaml` 的 tier/owner/review_by；E_UNCLASSIFIED | `make deps-check` 无未分级组件（entries=45, components=29） |
| 例外登记与到期失效 | `exceptions[].scope/expires/reason`；E_EXCEPTION | 负向用例 2：expires 改到 2020-01-01 触发 E_EXCEPTION |
| 自动更新通道的合并分级 | `renovate.json` 的 `platform-p0`（automerge=false）与 build-toolchain（仅 patch/digest） | `scripts/deps.py check` 的 E_RENOVATE 双向比对；`renovate.json` JSON 解析通过 |
| 审计报告与阶段证据 | `scripts/deps.py audit`（Docker Hub / Quay / endoflife.date） | `evidence/2026-11-23-deps-audit.md`，30 个组件行，查询失败标 unknown |
| 升级归类 change vs Fast Track | `docs/workflow/DEPENDENCIES.md` §4 | 文档评审；p0 升级路径写入 Stage brief 退出条件 |

## 4. 显式移交（不在本 change 验证）

| 项 | owner | 期限 | 依据 |
| --- | --- | --- | --- |
| Helm 21 个占位 digest 回填 + 补 `tag` | `production-delivery` | 2026-12-31 | 例外 `helm-placeholder-digests`；`deploy/README.md` 自述 placeholder |
| GitHub Actions `uses:` 改 commit SHA | `production-delivery` | 2027-01-31 | `renovate.json` 中 `helpers:pinGitHubActionDigests` 会提 PR |
| P0 升级（Jaeger v2、PostgreSQL major、Keycloak、Temporal、Meilisearch、Prometheus、Traefik、OTel） | 各 owner | 见 `review_by` | `evidence/2026-11-23-baseline.md` §6 |

## 5. 未覆盖/不声称

- 未验证 kind/k3d 集群内 Helm 渲染结果：本 change 不修改 Helm 模板，digest 仍是占位。
- 未验证 Renovate 实际开 PR 的行为：仓库未启用 Renovate App；配置已就绪，启用属运维动作。
- 未验证 CI 首次运行结果：`.github/workflows/deps.yml` 只做了本地 YAML 解析。
- 审计的 `上游最新` 依赖 registry API 当次返回；`unknown` 行不得当作「已是最新」。