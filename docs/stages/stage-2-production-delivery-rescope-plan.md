# production-delivery 降级归档与 Stage 2 收缩执行计划

> 状态：**待负责人批准**（2026-11-24）。本文件是计划，不是已执行结果；按 AGENTS.md 不自动批准、不自动归档、不自动 commit。
> 目标：在 1–2 个工作日内**只归档 `production-delivery`**，把生产级验证（GitOps 门控、k3d、CI runner、真实 staging IdP）拆到 Stage 3 / 独立 change，并同步收缩 Stage 2 的 AC 与退出条件。

## 1. 结论摘要

| 项 | 现状 | 本计划处置 |
|---|---|---|
| `production-delivery` | 54/70，16 项未完成 | 归档前把 2 项快速做完、2 项降级接受、12 项显式移出到新 change；裁剪 delta spec；归档 |
| Stage 2 AC-08（生产交付） | partial / not passed | **收窄为「交付资产 + 本地 kind 证据」**，按新口径可判 pass |
| Stage 2 AC-10（生产 IdP） | not executed | **移出 Stage 2**，由新 change `production-idp-staging` 承接，Stage 2 标「明确未覆盖」 |
| 未完成的生产级验证 | 受阻于单副本、离线镜像、无 CI runner、无真实 IdP | 拆为 3 个 Stage 后 change：`production-delivery-hardening`、`production-idp-staging`、`search-delivery` |
| Stage 2 剩余 | OSM 49/52、stage-2-integration 23/56 | 保持 Stage 2；本计划不归档 Stage 2，只收缩其范围 |

预计工时：PD 归档手术 4–6h + 新 change 骨架 2–3h + 全量门禁与提交 2h；若同时快速完成 11.1/11.3（约 1–2h），可让 OSM 的 7.5/7.6 一并具备闭合条件。

## 2. 现状量化

- `openspec list --json`：production-delivery **54/70**、object-search-meilisearch 49/52、stage-2-integration 23/56、jaeger-v2 0/10（Stage 外）。
- 工作区 **65 项未提交**（含 Helm chart、kind 脚本、CI、traceability、search 代码、证据文件）。
- 已具备的真实证据：kind 主集群与独立演练集群均 3 节点 Ready；9 个 ArgoCD Application Synced/Healthy；探针、OOMKilled、PDB 驱逐、备份/恢复 L1–L5、5.9 两轮真实回滚、4.6 口令轮换、8.4 六项负向 401、8.5 JWKS 轮换、helm lint/render/config/actionlint/shellcheck/gitleaks 全绿、`openspec validate --all --strict` 19 passed。
- 真实缺口：单副本 `Recreate` 无滚动能力；本机无法拉取 data/observability 镜像（离线 registry 仅 7 个核心镜像）；k3d 二进制缺失；无 CI runner；真实 staging IdP/claims mapper 不可得。

## 3. 未完成 16 项逐项判定

> 判定口径：**降级接受**＝实现与证据已覆盖验收的可观察部分，剩余限制写入 residual risk 并勾选；**快速完成**＝归档前 1–2h 内可做且证据充分；**移出**＝本轮不实现，任务标记「移出至 <change>」，由新 change 承接。

| # | 任务 | 已有证据/状态 | 判定 | 去向 |
|---|---|---|---|---|
| 3.3 | 优雅终止 / 滚动无 5xx | grace 30s + preStop；76 次请求 70×200、**0×5xx**、6×连接失败（单副本 Recreate） | **降级接受**（0 个 5xx 已达成；连接中断记 residual） | 勾选并注限制；多副本滚动验证→hardening |
| 3.4 | PDB 阻止驱逐 / 滚动可用副本 | Eviction API 429；`kubectl drain` 因 `ontoaxis-search-pdb` ALLOWED DISRUPTIONS=0 超时阻塞 | **降级接受**（阻止驱逐已达成；滚动最低副本因单副本不适用） | 勾选并注限制；多副本 PDB→hardening |
| 5.4 | wave 依赖门控 | 实测 sync-wave 只控制创建顺序（2s 一级），**不等待子 Application Healthy**；缺健康门控 | **移出**（验收不成立，不能降级为 pass） | `production-delivery-hardening` |
| 5.8 | 真实 `--sync` 回滚 | 脚本 + 隔离回归 29/29；真实路径被 readiness 烟测全绿阻断 | **移出**（5.9 的真实回滚已提供部分依据，5.8 脚本端到端验收仍需全绿烟测） | hardening |
| 6.3 | bootstrap 空集群完整安装 | 演练集群用 `--skip-argocd --skip-eso --skip-envoy` 复用组件；离线完整安装未做 | **移出** | hardening |
| 6.4 | verify.sh 正常环境 exit 0 | 失败路径已验证（rc=1 + 诊断目录）；正常 0 依赖 9.1 全绿 | **移出**（与 9.1 同批） | hardening |
| 6.5 | k3d 路径 | 无 k3d 二进制；Stage 2 不要求 | **移出**（低优先） | hardening（或独立 `delivery-env-parity`） |
| 8.6 | 非 localhost TLS fallback IdP | 仅生成临时证书；未部署 TLS fallback、未做登录/负向 | **移出**（与 AC-10 同批，含安全复评） | `production-idp-staging` |
| 9.1 | 烟测全绿 | edge 6/6；readiness 因 `read-only-api`、`running-images`、`idp-discovery` 失败 | **移出**（依赖真实工作负载/IdP/回滚标记） | hardening |
| 9.4 | CI `delivery-static` | workflow 已定义，未在 runner 跑；本地等价命令全绿 | **移出**（验收要求 CI run） | hardening |
| 9.5 | CI `delivery-kind` | workflow 已定义（workflow_dispatch/定时），无 runner | **移出** | hardening |
| 9.7 | 完整证据链 | 依赖 5.8/9.1 全绿 | **移出** | hardening |
| 11.1 | compose 合并 search profile | 片段 `deploy/compose/search/compose.search.yaml` 已备；主 `compose.yaml` 未合并 | **快速完成**（`up -d` healthy 可验证） | PD 内完成 |
| 11.2 | Meilisearch/search chart | **chart 已存在**：`deploy/helm/ontoaxis-app/values.yaml:140/171` 已渲染 meilisearch 与 ontoaxis-search | **快速完成**（补 `helm template`+kubeconform 证据即可） | PD 内完成 |
| 11.3 | kind 部署评审 | 演练集群已运行 meilisearch/search-worker | **快速完成**（补 Ready/探针/Secret 证据） | PD 内完成 |
| 11.4 | 回填 OSM 7.5/7.6 | 依赖 11.1–11.3 | **快速完成**（回填链接，不改 OSM tasks 之外的实现） | PD 内完成，通知 OSM |

**判定统计**：快速完成 4 项（11.x）；降级接受 2 项（3.3、3.4）；移出 10 项（5.4、5.8、6.3、6.4、6.5、8.6、9.1、9.4、9.5、9.7）。

## 4. 拆出的新 change（Stage 3 / Stage 外候选，不进 Stage 2 退出判据）

### 4.1 `production-delivery-hardening`
- 范围：5.4 wave 健康门控（Application 自定义 health 或 PreSync 门控）、5.8 真实 `--sync` 回滚、6.3 离线完整 bootstrap、6.4 verify 正常退出、6.5 k3d 一致性、9.1 烟测全绿、9.4/9.5 CI runner、9.7 完整证据链；3.3/3.4 多副本滚动与 PDB 增强。
- 依赖：可拉取 data/observability 镜像的 runner 或离线镜像包；CI runner 资源；≥2 副本的关键组件。
- 不做：不改变已归档的交付资产契约；不引入生产云账号/密钥。
- 主要目录：`deploy/kind/`、`scripts/delivery-*.sh|py|rb`、`.github/workflows/ci.yml`、`deploy/values/`。

### 4.2 `production-idp-staging`
- 范围：8.6 fallback（非 localhost issuer + TLS 证书轮换）+ S2-AC-10 真实 staging IdP 登录/JWKS/claims 映射与安全复评。
- 依赖：真实 staging IdP 或可复用的集群内 TLS Keycloak + 可签发/轮换密钥的管理权限。
- 不做：不提交生产密钥；本地 Compose Keycloak 不产生通过证据。
- 主要目录：`deploy/kind/tls/`、`deploy/values/staging-like.yaml`、`scripts/delivery-idp-check.sh`、`docs/stages/evidence/`。

### 4.3 `search-delivery`
- 背景：若 11.x 不在 PD 内快速完成，则整包移交本 change，同时闭合 OSM 7.5/7.6。
- 范围：compose `search` profile 合并、Meilisearch/search chart 验收、kind 部署评审、回填 OSM。
- 依赖：`production-delivery` 的 chart/values 所有权；OSM 重建路径。
- 说明：本计划推荐 11.x 在 PD 内快速完成，届时本 change 仅作为兜底方案，可不创建。

## 5. `production-delivery` 归档手术清单（文件级）

1. `openspec/changes/production-delivery/tasks.md`
   - 3.3、3.4 勾选，备注写「0×5xx / PDB 阻止驱逐已达成；单副本 Recreate 的连接中断与滚动最低副本限制记 residual risk，多副本验证移出 hardening」。
   - 5.4/5.8/6.3/6.4/6.5/8.6/9.1/9.4/9.5/9.7 勾选为「**移出**（2026-11-24 负责人决定）」，写明去向 change 与原因，不再声称通过。
   - 11.1–11.4 按快速完成结果勾选并附证据路径。
2. `openspec/changes/production-delivery/specs/production-delivery/spec.md`（345 行，13 个 ADDED Requirement）裁剪：
   - **整组移除并移入新 change delta**：`预发 IdP 配置与 claims 映射`（全部 6 场景）→ `production-idp-staging`。
   - **移除不成立/未实现场景**：`ArgoCD 同步与漂移收敛` 的「wave 顺序阻止并发启动」（实测只排序不等待）；`kind/k3d 可重复安装与诊断` 的「kind 与 k3d 结果一致」；`探针、资源与 PDB` 的「滚动更新遵守 PDB」；`交付烟测与失败证据` 的「全绿产出可归档结果」。上述场景移入 hardening delta。
   - 保留并已在本地验证的场景（Helm 渲染/摘要拒绝、受控晋升、ArgoCD Synced/Healthy+drift、kind 安装/幂等/诊断、探针/OOM/PDB 驱逐、Secret/轮换、零明文密钥、备份恢复、坏部署回滚、烟测非零/未执行标记、受控环境边界）。
   - 归档后 capability spec 不得包含未实现行为（这是诚实归档的硬要求）。
3. `openspec/changes/production-delivery/traceability.md`
   - 重写三条 AC 结论：S2-AC-08 = **pass（scope reduced：本地 kind 交付资产与证据）**；S2-AC-10 = **not covered / deferred to Stage 3**；S2-AC-11 = partial（保留 integration/CI 责任）。
   - Requirement 映射按裁剪后的 spec 重列；未覆盖项列 residual risk 与去向 change。
   - 归档检查 5 项全部勾选，注明证据文件。
4. `docs/stages/evidence/stage-2-production-delivery-archive-review.md`（新建）
   - 内容：范围收缩决定与负责人批准、证据索引、16 项判定表、residual risk、新 change 列表、门禁结果。
5. `deploy/README.md`、`docs/operations/delivery-runbook.md`：补「生产级验证已移交 Stage 3」提示；版本表/命令保持不变。
6. `openspec/changes/production-delivery/design.md`：Open Questions 记录降级与拆分决定（wave 门控、k3d、真实 IdP、CI runner）。

## 6. Stage 2 文档更新清单

| 文件 | 改动 |
|---|---|
| `docs/stages/stage-2-ontoaxis-runtime-brief.md` | §4 S2-AC-08 改为「交付资产与本地 kind 证据：Helm/ArgoCD 资产、探针/PDB/Secret 配置、备份恢复与镜像回滚演练在本地 kind 有可复核证据」；S2-AC-10 标注「Stage 3 范围（production-idp-staging），Stage 2 明确未覆盖」；§5 change 表 production-delivery 注「已按收缩范围归档」；§8 退出条件 4 删除 k3d 与「生产 IdP」字样，保留 Compose + kind 全栈 E2E；§9 决策 5 补「生产级交付验证后移 Stage 3」 |
| `docs/stages/stage-2-change-map.md` | Wave 2 / G2 删除「k3d 完成部署」与「生产 IdP 使用非本地 issuer」两条，改为「本地 kind 交付资产与证据」；§10 状态表更新 production-delivery 为已归档（收缩范围）；§12「Stage 外/后续候选」登记 `production-delivery-hardening`、`production-idp-staging`（及兜底 `search-delivery`） |
| `docs/stages/stage-2-unblock-checklist.md` | D-07 端口冲突等风险保留，注明不再阻断 Stage 2 归档（若属实）；补「生产级验证移交 Stage 3」 |
| `docs/stages/stage-2-change-batch-review.md` | 增加一条变更记录：production-delivery 范围收缩与 AC-08/AC-10 调整（需负责人批准） |
| `openspec/changes/stage-2-integration/traceability.md` | 若引用 AC-08/AC-10，同步改为收缩后的口径（由该 change 负责人执行，不越权修改） |

## 7. 执行顺序与门禁

**Phase 0 — 负责人决策（0.5h）**
- 批准本计划：AC-08 收窄口径、AC-10 移出 Stage 2、3 个新 change 名称与归属。
- 记录到 `docs/stages/evidence/stage-2-production-delivery-archive-review.md`。

**Phase 1 — 快速完成 11.x（1–2h，可选但推荐）**
- 合并 `deploy/compose/search/compose.search.yaml` 到 `deploy/compose/compose.yaml`；`docker compose -f deploy/compose/compose.yaml --profile search config -q` + `up -d` 后 meilisearch/search-worker healthy。
- `helm template ontoaxis-app deploy/helm/ontoaxis-app -f deploy/values/common.yaml -f deploy/values/kind.yaml | kubeconform` 证据。
- 演练集群 `kubectl -n oa-app get pods` 记录 meilisearch/search Ready、探针与 Secret。
- 回填 OSM 7.5/7.6 链接（不改其 tasks 实现）。

**Phase 2 — PD change 手术（2–3h）**
- 按 §5 修改 tasks/spec/traceability/design/evidence 文档。
- `openspec validate production-delivery --strict` 与 `openspec validate --all --strict` 必须通过。
- `git diff` 人工复核，确认 spec 不再包含未实现场景。

**Phase 3 — 提交（0.5h）**
- 按主题分 2 个提交：`feat(delivery): kind/ArgoCD evidence and chart hardening`（65 项工作区改动）与 `docs(openspec): rescope production-delivery for archive`。
- 提交前跑：`make lint`、`make test`、`helm lint deploy/helm/*`、`scripts/delivery-render-check.sh`、`scripts/delivery-config-check.sh`、`actionlint`、`shellcheck scripts/delivery-*.sh deploy/kind/*.sh`、`gitleaks detect --config .gitleaks.toml`。

**Phase 4 — 归档（0.5h）**
- `openspec archive production-delivery --yes`。
- 校验：归档目录 `openspec/changes/archive/2026-11-24-production-delivery/`；`openspec/specs/production-delivery/spec.md` 已生成；`openspec validate --all --strict` 通过；`openspec list` 中 PD 消失。
- 提交 `chore(openspec): archive production-delivery (reduced scope)`。

**Phase 5 — Stage 2 更新（1h）**
- 按 §6 更新 brief/change-map/unblock/batch-review。
- 如需新 change 骨架：`openspec new change production-delivery-hardening` 等，生成 proposal/tasks/design/spec delta（仅规划，不实施）。
- `openspec validate --all --strict`；更新 `docs/stages/evidence/` 索引。
- 输出 Stage 2 剩余清单：OSM（49/52，若 11.x 完成则 52/52 可归档）、stage-2-integration（23/56）、G2 收缩后条件。

## 8. 风险与决策点

1. **AC 变更的治理风险**：修改 S2-AC-08/AC-10 属 Stage 范围变更，必须负责人书面批准并记录；不得由 agent 自行改判定。
2. **诚实性**：delta spec 裁剪必须彻底，删除所有未实现场景；否则归档后主 spec 会固化未实现行为，违反 AGENTS「不把草案当已实现」。
3. **11.x 取舍**：快速完成可让 OSM 同步接近归档，但增加 1–2h；若优先「最快归档 PD」，可把 11.x 整体移入 `search-delivery`，PD 立即可归档。
4. **Stage 2 工期**：本计划只收缩 PD；OSM + integration 仍需时间，Stage 2 退出不会被本计划自动触发。
5. **未提交工作区**：65 项改动必须先提交并复核；否则归档提交会混入未审代码。
6. **双集群**：`ontoaxis-kind` 与 `ontoaxis-kind-rehearsal` 均保留；归档后建议 `kind-down` 演练集群释放资源（需负责人确认）。

## 9. 完成判据

- `production-delivery` 已归档，`openspec list` 不再显示。
- 归档 spec 中不存在未实现场景；traceability 明确 AC-08 pass（收缩）、AC-10 deferred、AC-11 partial。
- Stage 2 brief/change-map/unblock 已同步新口径，`openspec validate --all --strict` 通过。
- 3 个后续 change（或至少 2 个）在 change map §12 登记，且未进入 Stage 2 退出判据。
- 工作区干净（或仅剩明确的 Stage 3 规划改动），提交说明含范围、证据与 residual risk。
