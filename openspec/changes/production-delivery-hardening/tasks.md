# Tasks: production-delivery-hardening

> 计划任务（2026-11-24）。全部未实施，未勾选；不得以 `production-delivery` 的收缩证据冒充完成。

## 1. GitOps 门控与回滚
- [ ] 1.1 为子 Application 配置真实健康门控，空集群验证 platform Healthy 后 data/app 才创建
- [ ] 1.2 执行真实 `delivery-rollback.sh --sync` 并保留 Synced/Healthy 证据

## 2. kind/k3d 与安装
- [ ] 2.1 离线完整 `bootstrap.sh`（ArgoCD/ESO/Envoy + 固定 SHA Git 源）
- [ ] 2.2 `verify.sh` 正常环境退出 0
- [ ] 2.3 k3d 等价安装/烟测

## 3. 烟测与 CI
- [ ] 3.1 readiness/full 烟测全绿（含 running-images kind-load 语义）
- [ ] 3.2 `delivery-static` CI 在 runner 通过
- [ ] 3.3 `delivery-kind` CI 在 runner 通过并上传证据
- [ ] 3.4 完整证据链归档（9.7）

## 4. 多副本增强
- [ ] 4.1 core 多副本探针/PDB/滚动更新（无 5xx）验证
- [ ] 4.2 记录与单副本 Recreate 的差异与迁移步骤
