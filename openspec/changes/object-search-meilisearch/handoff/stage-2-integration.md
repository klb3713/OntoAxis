# 交付包：object-search-meilisearch → stage-2-integration

- 归属：`docs/stages/evidence/` 与 `cmd/`、`internal/app/` 由 `stage-2-integration` 收录/合并（见 `docs/stages/stage-2-change-map.md` 行 116–123）。
- 本 change 只提供证据源文件；共享 Stage 证据须由 `stage-2-integration` 收录，故 8.4 不勾。
- 日期：2026-11-24
- 汇总清单：[handoff-todo.md](handoff-todo.md) B 节；已在 `openspec/changes/stage-2-integration/tasks.md` §11 登记（11.1–11.4）。

## 1. 证据源文件（可复制到 `docs/stages/evidence/`）

| 源文件（本 change 内） | 覆盖 | 关键结果 |
| --- | --- | --- |
| `evidence/2026-11-23-object-search-integration.md` | 1.4/4.1/5.1–5.7 | 真实 PG16+Meilisearch 集成、缺陷修复、go.mod 未改、not executed 清单 |
| `evidence/2026-11-24-object-search-verification.md` | 5.5/5.7/8.1 | 后端集成 12 项、原生 HTTP 68/68、中文基线与隔离 Compose E2E 73/73 |
| `evidence/2026-11-24-search-integration-ci-local.log` | 8.2 | CI 同参本地日志：exit 0、0 SKIP、60 通过项 |
| `evidence/2026-11-24-object-search-runbook-repro.md` + `.log` | 9.1 | 手册 §3/§4/§5/§12 命令复现记录 |
| `traceability.md` | 9.4 | 需求→证据映射；planned 未写成 actual |

## 2. 收录后仍需回填

- [x] 8.2 的 GitHub Actions run 链接：[run 45405474349](https://github.com/klb3713/OntoAxis/actions/runs/45405474349)，job [`search-integration`](https://github.com/klb3713/OntoAxis/actions/runs/45405474349/job/138484907178) success（2026-11-24，草稿 PR #1）
- [ ] 9.1/9.2/9.3 评审签字记录
- [ ] 在 Stage 证据中区分“本地合成数据验证”与“生产/CI 验收”，不得把本地通过写成 CI/生产通过

## 3. 已完成的跨 change 协调

- `internal/app` 装配：`cmd/ontoaxis-search` 与 search worker 已由本 change 提供，装配 patch 供集成 change 合并。
- 授权：`authorization-openfga-cedar` 已归档，跨租户/越权/守卫不可用已获真实本地 HTTP 证据。
