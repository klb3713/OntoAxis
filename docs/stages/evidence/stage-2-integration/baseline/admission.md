# Wave 3 准入核对（stage-2-integration 1.1）

- 日期：2026-11-28
- 环境：本地仓库（分支 `main`，合成/本地测试范围）
- 结论：**准入核对通过**（8 个前置 change 已全部归档；G2 已由负责人登记 `pass`）

## 1. G2 准入门

- `docs/stages/stage-2-change-map.md` §4 存在 G2 = `pass` 记录：日期 2026-11-25、确认人 klb3713、口径（compose 真实 runner 证据 + 采信归属方 helm/kind 证据；6.x kind 复跑属退出条件 4）。
- 详细记录：`docs/stages/evidence/stage-2-g2-gate-record.md`。

## 2. 前置 8 个 change 归档确认

| Change | 归档位置 |
| --- | --- |
| runtime-observability | `openspec/changes/archive/2026-11-22-runtime-observability` |
| edge-gateway | `openspec/changes/archive/2026-11-22-edge-gateway` |
| authorization-openfga-cedar | `openspec/changes/archive/2026-11-23-authorization-openfga-cedar` |
| connector-runtime | `openspec/changes/archive/2026-11-23-connector-runtime` |
| pipeline-kestra-dbt | `openspec/changes/archive/2026-11-23-pipeline-kestra-dbt` |
| action-temporal | `openspec/changes/archive/2026-11-23-action-temporal` |
| object-search-meilisearch | `openspec/changes/archive/2026-11-25-object-search-meilisearch` |
| production-delivery | `openspec/changes/archive/2026-11-24-production-delivery` |

## 3. `openspec validate --all --strict`

- 2026-11-28 结果：**22 passed / 0 failed**。
- connector-runtime 早前遗留的未跟踪 evidence（`evidence/2026-11-23-success-e2e-5-3.log`，仅被 `*.log` 忽略）已迁入 `openspec/changes/archive/2026-11-23-connector-runtime/evidence/`，并在 `.gitignore` 增加 archive 例外；残留空目录已删除。

## 4. `openspec list`（2026-11-28 原文）

```
Changes:
  delivery-runtime-acceptance       0/10 tasks
  stage-2-integration               43/56 tasks
  production-idp-staging            0/9 tasks
  production-delivery-hardening     0/11 tasks
  jaeger-v2                         0/10 tasks
```

## 5. 范围外 change 排除理由（不参与 Stage 2 退出判据）

| Change | 排除理由 | 登记位置 |
| --- | --- | --- |
| production-idp-staging | 真实 staging IdP（非 localhost TLS/JWKS/claims）；S2-AC-10 已移出 Stage 2 | 退出报告 §2、change-map §12 |
| production-delivery-hardening | 生产级交付加固（GitOps 健康门控、离线 bootstrap、k3d、CI runner、多副本等） | 退出报告 §2、change-map §12 |
| jaeger-v2 | Jaeger v2 迁移，独立技术债 | 退出报告 §2、change-map §12 |
| delivery-runtime-acceptance | 承接 S2-AC-08 交付运行态验收（2026-11-28 新建），Stage 外独立 change | 退出报告 §2、change-map §12 |
