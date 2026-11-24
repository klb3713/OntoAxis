# Traceability: production-delivery

> 2026-11-24 收缩范围后重写（负责人批准）。本 change 以「交付资产 + 本地 kind 证据」归档；生产级 GitOps 门控、k3d、CI runner 与真实 staging IdP 移交 Stage 3 的后续 change。

## Stage 2 AC 映射

| Stage AC | 验收要求（Stage 2 收缩后口径） | 本 change 覆盖 | 证据与结论 |
|---|---|---|---|
| S2-AC-08 | 生产交付资产与本地 kind 证据：Helm chart + ArgoCD 资产可渲染/同步；探针/PDB/Secret 配置生效；备份恢复与镜像回滚在本地 kind/演练集群有可复核证据 | Helm 渲染与摘要固定、受控晋升、ArgoCD Synced/Healthy 与漂移自愈、kind 安装与诊断、探针/资源/PDB、密钥注入与轮换、备份恢复 L1–L5、镜像回滚演练、交付烟测失败语义、受控环境边界 | **pass（scope reduced，2026-11-24）**：`docs/stages/evidence/stage-2-production-delivery-kind.md`（3.1 探针、3.2 OOMKilled、3.4 PDB 429、7.1 备份、7.2 恢复、7.4 RPO/RTO、6.7 port-forward 烟测）、`docs/stages/evidence/stage-2-production-delivery.md` §17–§21（GitOps 9 应用全 Synced/Healthy、漂移 45s 自愈、5.9 两轮真实回滚、4.6 轮换、D-PD-13/16 修复）、`dist/delivery/gitops-5/`、`dist/delivery/drill-5.9a|b/`、`dist/delivery/restore-l5.log`、`dist/gates/*.log`。**限制**：单副本 Recreate 的 6 次连接中断、多副本 PDB/滚动验证、k3d、CI runner 不在本口径内。 |
| S2-AC-10 | 生产 IdP（非本地 OIDC issuer/JWKS/claims 映射完成登录与 token 校验，不提交生产密钥） | 已拆分到 Stage 3 的 `production-idp-staging` change；本 change 只保留本地合成 IdP 与负向/轮换证据，不作为 AC 通过依据 | **not covered（deferred，2026-11-24）**：8.3 本地合成 Keycloak 3 passed、8.4 六项负向全 401、8.5 JWKS 轮换新老 token 200、8.7 安全评审条件通过（`docs/stages/evidence/stage-2-idp-security-review.md`）。因 issuer 为本地/非 TLS fallback，按 Stage 2 收缩决定 **AC-10 移出 Stage 2**，由 `production-idp-staging` 在 Stage 3 验收。 |
| S2-AC-11（协作） | Stage 2 证据与报告完成：全部 change 归档、`openspec validate --all --strict` 通过、CI 全绿、Stage 退出获负责人确认 | 本 change 提供证据源文件、工作流定义与 runbook | **partial（2026-11-24）**：证据源与 `docs/operations/delivery-runbook.md` 齐全；`openspec validate --all --strict` 19 passed 0 failed；`delivery-static`/`delivery-kind` workflow 已定义但未在 runner 运行（移交 `production-delivery-hardening`）。G2/Stage 退出由 `stage-2-integration` 汇总。 |

说明：AC 口径的收缩属 Stage 范围变更，已由负责人于 2026-11-24 批准并记录在 `docs/stages/evidence/stage-2-production-delivery-archive-review.md`。

## Requirement 映射（归档后 11 条 ADDED Requirement）

| Requirement | 场景证据 |
|---|---|
| 可复现的 Helm 发布单元与镜像摘要固定 | `scripts/delivery-render-check.sh` 两次渲染 sha256；负向 latest/缺 digest 拒绝；kubeconform 11 valid；`dist/helm-render/`、`dist/gates/*.log` |
| 环境 values 与受控晋升 | `scripts/delivery-promote.sh` 隔离回归 18/18；`deploy/releases/kind.yaml` 7 镜像 digest；`delivery-release-check.sh` 正向 7/7、负向 rc=1 |
| ArgoCD 同步与漂移收敛 | 演练集群 root+8 子应用 `Synced/Healthy`；`kubectl scale deploy web --replicas=3` 45s 自愈（`dist/delivery/gitops-5/{apps-final.txt,drift.log}`）；prod-like 无 automated 由 values 校验 |
| kind 可重复安装与诊断 | `deploy/kind/kind-up.sh` 幂等/registry/隔离网络 8 用例；`kind-up-test.sh` 8/8；坏镜像 `verify.sh` rc=1 + 诊断目录（`dist/delivery/diagnostics-badimage-verify/`） |
| 探针、资源与 PDB | 3.1 改坏 readiness → Endpoint 摘除不重启；3.2 OOMKilled 137；3.4 Eviction 429 / drain 阻塞；`dist/delivery/diagnostics-oom/describe.txt`、`dist/delivery/pdb-drain/drain.log` |
| 密钥注入与外部密钥源 | ESO 5 个 ExternalSecret `SecretSynced True`；4.6 数据库口令轮换后 core/openfga 恢复、镜像 digest 不变（`dist/delivery/rotation-4.6/`）；缺失 key 未就绪负向 |
| 仓库零明文密钥 | `gitleaks detect` no leaks（34 commits）；`delivery-secret-scan.sh` rc=0；证据脱敏 |
| 备份与恢复演练 | 7.1 CronJob 手动 Job `backup complete sha256=941ccc20…`；7.2 L1–L5 恢复 `dist/delivery/restore-l5.log`；7.3 篡改/截断被拒；7.4 RPO<5s/RTO≈107s；7.5 未覆盖范围标注 |
| 镜像摘要与回滚 | 5.9a 坏 digest `ErrImagePull→ImagePullBackOff`，回滚 20s 恢复；5.9b readiness 404，回滚 33s 恢复；`dist/delivery/drill-5.9a|b/`；5.6 release manifest 一致性 |
| 交付烟测与失败证据 | 注入坏镜像 rc=1 + diag；未执行项 `not passed`；JSON 含 environment/digest/overall；`dist/delivery/smoke-20261124.log` |
| 受控环境与安全边界 | 无生产凭据/客户数据；证据标注环境；云端步骤 `not executed`；`deploy/README.md` 边界清单 |

## 移交 Stage 3 的后续 change

| change | 承接内容 |
|---|---|
| `production-delivery-hardening` | 5.4 wave 健康门控、5.8 真实 `--sync` 回滚、6.3 离线完整 bootstrap、6.4 verify 正常退出、6.5 k3d、9.1 烟测全绿、9.4/9.5 CI runner、9.7 完整证据链、3.3/3.4 多副本增强 |
| `production-idp-staging` | 8.6 TLS fallback + S2-AC-10 真实 staging IdP 登录/JWKS/claims 与安全复评 |

## 未覆盖 / Residual risk

- 不覆盖真实云账号、生产密钥、生产环境、真实客户数据；不做生产切换、多区域 HA/DR、容量规划、自动扩缩容。
- 单副本 `strategy: Recreate`：滚动期间存在短暂连接中断（6/76），无多副本 PDB 滚动验证。
- ArgoCD sync-wave 仅控制 Application 创建顺序，不等待子 Application 真实 Healthy（5.4 移交 hardening）。
- 烟测完整模式未全绿（`read-only-api`、`running-images` kind-load 语义、`idp-discovery`），已提供 `SMOKE_IMAGE_ID_MODE=pinned` 修复但全链路验收移交 hardening。
- k3d、CI runner、真实 staging IdP 未验证；对象存储/Meilisearch 依赖重建路径，不在备份范围。
- 工作区变更未推送远端；本地双 kind 集群与 registry 保留。

## 归档检查

- [x] tasks 全部勾选且每项有可复核证据；移出项标注去向 change 与原因
- [x] `openspec validate production-delivery --strict` 与 `openspec validate --all --strict` 通过
- [x] S2-AC-08/AC-10 结论已标注 pass（收缩）/ not covered（deferred），未达成项有 residual risk
- [x] G2 门禁记录可追溯，证据由 `stage-2-integration` 汇总
- [x] `deploy/README.md`、`docs/operations/delivery-runbook.md`、Stage 2 change map 与证据索引已同步
