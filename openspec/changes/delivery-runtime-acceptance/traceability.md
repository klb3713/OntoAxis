# Traceability: delivery-runtime-acceptance

> 承接原 `stage-2-integration` 的交付运行态验收。Origin: Stage 2 / S2-AC-08 / `stage-2-integration` 6.1–6.5、7.7。状态均为 `planned`，未验证不得写 `passed`。

| 原 Stage 2 溯源 | 原任务 | 本 change 任务 | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-08 生产交付（已整条移出 Stage 2） | 6.1 Helm 部署 | 2.1 / 2.2 | `kubectl get pods`、probe/PDB 对比日志 | planned |
| S2-AC-08 | 6.2 ArgoCD 同步 | 1.2 / 3.1 | `argocd app get` 输出 | planned |
| S2-AC-08 | 6.3 集群入口 | 2.3 | 入口四类响应命令输出 | planned |
| S2-AC-08 | 6.4 备份恢复 | 4.1 | 恢复演练日志 | planned |
| S2-AC-08 | 6.5 镜像回滚 | 4.2 | 回滚 tag/时间/health 输出 | planned |
| S2-AC-08 / E2E-10 | 7.7 kind 栈 E2E 子集 | 5.1 | kind E2E-01/02/10 结果 | planned |

## 边界

- 仅本地/受控 runner + 合成数据；不等于生产验收。
- 与 `production-delivery-hardening`（生产级加固）、`production-idp-staging`（真实 IdP）、`jaeger-v2`（v2 迁移）分工，不重复。
