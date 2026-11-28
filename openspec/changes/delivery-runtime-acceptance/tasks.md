# Tasks: delivery-runtime-acceptance

> 计划任务（2026-11-28）。全部未实施、未勾选；不得以已归档 `production-delivery` 的收缩证据冒充完成。承接原 `stage-2-integration` 6.1–6.5、7.7。

## 1. 运行环境与 GitOps 源
- [ ] 1.1 在可控 runner 环境启动 kind（镜像可拉取），记录节点 `Ready` —— 验证：`kubectl get nodes` 输出入库
- [ ] 1.2 配置真实 GitOps 源（真实 `repoURL` 只读凭据，或固定 SHA 本地只读 git 服务替代），root app 可解析 —— 验证：`argocd app get ontoaxis-root` 输出入库

## 2. Helm 安装与集群入口（承接 6.1 / 6.3）
- [ ] 2.1 Helm 渲染后安装/apply，Deployment/Service/ConfigMap/Secret 就绪 —— 验证：`kubectl get pods` 全部 Ready
- [ ] 2.2 探针/PDB/资源限额与 chart 一致；故意破坏 readiness 端点验证摘除/回填 —— 验证：source 对比 + 端点观察日志
- [ ] 2.3 `oa-edge` 集群入口：public 200 / 受保护 401|200 / 未知 404 / `/metrics` 不可达 —— 验证：命令输出入库

## 3. ArgoCD 同步（承接 6.2）
- [ ] 3.1 app-of-apps 全部 Application `Synced/Healthy` —— 验证：CLI 输出或截图入库

## 4. 备份恢复与镜像回滚（承接 6.4 / 6.5）
- [ ] 4.1 备份 Job → 恢复 → `/readyz` 与关键数据抽查通过 —— 验证：演练日志入库
- [ ] 4.2 固定上一已验证 tag 回滚 → `/healthz`、`/readyz`、核心 smoke 通过 —— 验证：记录 tag 与时间

## 5. kind 栈 E2E 子集（承接 7.7 / E2E-10）
- [ ] 5.1 在 kind 执行 E2E-01、E2E-02、E2E-10（交付链路） —— 验证：结果入库；未覆盖组合在报告标 `not covered`

## 6. 证据与映射
- [ ] 6.1 逐项映射原 S2-AC-08，证据归档到 `docs/stages/evidence/delivery-runtime-acceptance/` —— 验证：索引文件 + 每条 `passed` 有可点开证据
