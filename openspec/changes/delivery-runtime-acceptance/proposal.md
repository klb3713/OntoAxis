# Delivery Runtime Acceptance

> 状态：planning（2026-11-28 创建）。本 change 尚未实施：`tasks.md` 全部未勾选。**Stage 外独立 change**（不归属任何 Stage）。
> 来源：负责人 2026-11-28 决策 —— Stage 2 采用 **Compose-only 直到正式发布**；S2-AC-08 整条移出 Stage 2，kind/k3d/ArgoCD 交付运行态验收（原 `stage-2-integration` 6.1–6.5、7.7、E2E-10）由本 change 承接。决策记录见 `docs/stages/stage-2-compose-only-amendment-plan.md`。

## Why

Stage 2 以功能与 Compose 验证为主；交付层（Helm/ArgoCD/kind/k3d）运行态验收在功能未完成、发布尚远时反复复跑成本高，且频繁被镜像/环境阻塞（2026-11-25 kind 集群 13 个镜像 `ImagePullBackOff`、ArgoCD 无 Application）。为避免 Stage 2 被交付运行态验收拖住，同时不丢失交付可部署性验证，将这部分验收整条移出为独立 change，在具备可控镜像/runner 环境时一次性做严，作为正式发布前的交付验收依据。

## What Changes

- 承接 Stage 2 移出的交付运行态验收：Helm 安装（Deployment/Service/ConfigMap/Secret/探针/PDB/资源限额）、ArgoCD 真实同步（Synced/Healthy）、集群入口（Envoy `oa-edge`）、备份→恢复演练、镜像回滚演练。
- 承接 kind 栈 E2E 子集（E2E-10 交付链路，含 E2E-01/E2E-02/E2E-10）与真实 `repoURL` GitOps 源下的同步验证。
- 产出可复核证据（`kubectl`/`argocd`/演练日志），逐项映射原 Stage 2 AC-08。
- 与既有 change 分工：本 change = 交付资产**基线运行态验收**；`production-delivery-hardening` = 生产级**加固**（GitOps 健康门控、离线 bootstrap、k3d 等价、CI runner、多副本）；真实 staging IdP 由 `production-idp-staging` 承接；Jaeger v2 由 `jaeger-v2` 承接。不重复上述 change 的工作。

## Capabilities

- **New Capabilities**：无（`.openspec.yaml` 设 `skip_specs: true`；本 change 仅改变验证/环境手段，不改变系统行为契约）。
- **Modified Capabilities**：无（`production-delivery` 主 spec 已归档，行为不变；本 change 只增加验收证据）。

## Impact

- 目录：`deploy/helm/`、`deploy/argocd/`、`deploy/kind/`、`deploy/values/`、`scripts/delivery-*`、`.github/workflows/`（delivery-kind）。
- 不修改已归档 `production-delivery` 资产的行为；仅新增验收证据与最小修复。
- 无生产密钥/真实客户数据；仍受 AGENTS.md §6 约束；不使用真实云账号。

## Out of Scope

- 生产级加固（`production-delivery-hardening`）、真实 staging IdP（`production-idp-staging`）、Jaeger v2（`jaeger-v2`）。
- 真实生产集群 / 真实客户数据 / 生产 SLO 压测。
