# production-delivery 归档评审（收缩范围）

- 评审日期：2026-11-24
- 评审人：klb3713（负责人）
- 结论：**批准按收缩范围归档**；生产级验证移交 Stage 3 后续 change。
- 归档前状态：54/70（评审时）；归档时 70/70，其中 10 项为「移出至后续 change」、2 项降级接受、4 项 search 交接快速完成。

## 1. 决策依据

Stage 2 周期过长、后续功能待开发，当前环境无法在合理时间内完成生产级验证：
- 本机无法拉取 data/observability 12 个 render-only 工作负载镜像；
- 无 CI runner（`delivery-static`/`delivery-kind` 无法真实运行）；
- 无真实 staging IdP / claims mapper；
- core 单副本 `Recreate`，无法做多副本滚动/PDB 验证；
- 无 k3d 二进制。

因此只归档 **已具备可复核证据** 的交付资产，其余显式拆分，不伪造通过。

## 2. 未完成 16 项判定

| # | 任务 | 判定 | 去向/说明 |
|---|---|---|---|
| 3.3 | 优雅终止/滚动无 5xx | 降级接受 | 0×5xx 达成；6 次连接中断（单副本 Recreate）记 residual；多副本→hardening |
| 3.4 | PDB 阻止驱逐 | 降级接受 | Eviction 429 + drain 阻塞有证据；滚动最低副本不适用；多副本→hardening |
| 5.4 | wave 依赖门控 | 移出 | 实测 sync-wave 只排序不等待 Healthy |
| 5.8 | 真实 `--sync` 回滚 | 移出 | 脚本+隔离回归 29/29；真实路径被烟测全绿阻断 |
| 6.3 | bootstrap 完整安装 | 移出 | 演练用 `--skip-argocd --skip-eso --skip-envoy` |
| 6.4 | verify 正常 exit 0 | 移出 | 失败路径已验证；正常依赖 9.1 |
| 6.5 | k3d | 移出 | 无 k3d 二进制 |
| 8.6 | TLS fallback IdP | 移出 | 随 AC-10 移交 `production-idp-staging` |
| 9.1 | 烟测全绿 | 移出 | edge 6/6；readiness 三项失败 |
| 9.4 | CI delivery-static | 移出 | workflow 已定义，无 runner |
| 9.5 | CI delivery-kind | 移出 | 同上 |
| 9.7 | 完整证据链 | 移出 | 依赖 5.8/9.1 |
| 11.1 | compose search profile | 快速完成 | `config -q` rc0；`up -d` 后 meili/worker Up，`/health` available，`/healthz`、`/readyz` 200 |
| 11.2 | search chart | 快速完成 | chart 已存在；`helm template` rc0，kubeconform 11 valid |
| 11.3 | kind 部署评审 | 快速完成 | 演练集群 meilisearch/ontoaxis-search 1/1 Running，探针/资源/PSA 合规 |
| 11.4 | 回填 OSM 7.5/7.6 | 快速完成 | OSM 51/52（仅剩 8.4 由 stage-2-integration 收录） |

## 3. 证据索引

- `docs/stages/evidence/stage-2-production-delivery-kind.md`：探针 3.1、OOM 3.2、PDB 3.4、备份 7.1、恢复 7.2、RPO/RTO 7.4、烟测 6.7、缺陷表。
- `docs/stages/evidence/stage-2-production-delivery.md`：静态门禁、§17 GitOps 9 应用 Synced/Healthy、§18 5.9 两轮回滚、§19 D-PD-13/16、§20 D-PD-16b、§21 剩余任务评估。
- `docs/stages/evidence/stage-2-idp-security-review.md`：8.7 条件通过（仅技术证据，不作 AC-10 依据）。
- `dist/delivery/gitops-5/{apps-final.txt,drift.log}`；`dist/delivery/drill-5.9a|b/`；`dist/delivery/restore-l5.log`；`dist/delivery/rotation-4.6/`；`dist/delivery/idp-negative/`；`dist/delivery/idp-rotation/`；`dist/delivery/11-search-{compose,chart,kind}/`。
- `dist/gates/full-gates.log`、`dist/gates/code-gates.log`。

## 4. 门禁结果（归档前复跑）

- `openspec validate --all --strict`：19 passed 0 failed（归档后复跑记录）。
- `openspec validate production-delivery --strict`：valid。
- `helm lint` 5 charts 0 failed；`delivery-render-check.sh` PASS；`delivery-config-check.sh` OK。
- `actionlint` rc=0；`shellcheck scripts/delivery-*.sh deploy/kind/*.sh` rc=0；`gitleaks detect` no leaks。

## 5. 移交 Stage 3

- `production-delivery-hardening`：5.4/5.8/6.3/6.4/6.5/9.1/9.4/9.5/9.7 + 多副本增强。
- `production-idp-staging`：8.6 + S2-AC-10 真实 staging IdP。
- 以上 change 尚未实施，登记于 `docs/stages/stage-2-change-map.md` §12。
