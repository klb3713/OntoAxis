# kind 集群现状快照（6.x 复跑前置检查）

- 日期：2026-11-25
- 上下文：`kind-ontoaxis-kind`（当前 kubectl context）
- 目的：评估 `stage-2-integration` 6.1–6.5（kind 交付层）能否在本轮复跑

## 集群

| 节点 | 状态 | 角色 | 版本 |
| --- | --- | --- | --- |
| ontoaxis-kind-control-plane | Ready | control-plane | v1.32.0 |
| ontoaxis-kind-worker | Ready | — | v1.32.0 |
| ontoaxis-kind-worker2 | Ready | — | v1.32.0 |

命名空间存在：`argocd`、`envoy-gateway-system`、`external-secrets`、`oa-app`、`oa-data`、`oa-jobs`、`oa-observability`、`oa-system`、`local-path-storage` 等。

## 部署现状

- ArgoCD 控制器 Running，但 `kubectl -n argocd get applications` **无 Application** → 6.2（ArgoCD 同步）当前无对象可验。
- `oa-app`：`ontoaxis-core`、`web`、`ontoaxis-search`、`meilisearch` 为 `Running`；`diag-bad-image` 为 `ImagePullBackOff`（演练残留）。
- `oa-data`、`oa-observability`：`kafka`、`debezium`、`apicurio`、`nessie`、`kestra`、`jaeger`、`prometheus`、`alertmanager`、`otel-collector`、`perses` 等大量 `ImagePullBackOff`。
- `kubectl get pods -A` 中 `ImagePullBackOff` 计数：**13**（镜像未在集群可拉取的 registry 中）。

## 结论

- 6.1（Helm 安装/探针/PDB/Secret 全 Ready）、6.2（ArgoCD Synced/Healthy）、6.4（备份恢复）、6.5（镜像回滚）**本轮无法通过**：集群镜像源未填充，多数工作负载无法拉取镜像；ArgoCD 无 Application。
- 7.7（kind 栈 E2E 子集）同样受此阻塞。
- 需在有可用镜像仓库（或预先 `kind load docker-image`）的环境重跑，或先由 `production-delivery` 归属方补齐 registry 同步后复验。
- 本文件仅记录现状，不构成 6.x 通过或失败的门禁结论。
