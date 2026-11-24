# Production Delivery Hardening

> 状态：planning（2026-11-24 创建，Stage 3 候选，暂不归属 Stage）。本 change 尚未实施：`tasks.md` 全部未勾选。
> 来源：`production-delivery` 归档时显式移出的 9 项 + 3.3/3.4 多副本增强。见 `docs/stages/evidence/stage-2-production-delivery-archive-review.md`。

## Why

`production-delivery` 已按「交付资产 + 本地 kind 证据」归档；生产级 GitOps 门控、真实回滚路径、离线完整 bootstrap、k3d、CI runner 与完整烟测链因环境/时间限制未验证，需独立 change 在具备 runner 与可控镜像环境的阶段完成，不把未验证行为写入已归档主 spec。

## What Changes

- GitOps：`sync-wave` 健康门控（子 Application 真实 Healthy 后才推进）、真实 `delivery-rollback.sh --sync` 路径。
- kind：离线完整 `bootstrap.sh`（含 ArgoCD/ESO/Envoy 与固定 SHA Git 源）、`verify.sh` 正常退出、k3d 等价验证。
- 烟测：readiness/full 全绿链路与 `running-images` kind-load 语义落地。
- CI：`delivery-static`/`delivery-kind` 在真实 runner 运行并保留证据。
- 多副本：core 等关键组件多副本下的探针/PDB/滚动更新验证（替换当前单副本 Recreate 限制）。
