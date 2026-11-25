# 9.1 运维手册评审材料（object-search）

> 对应任务 **9.1**。本文件记录运维手册评审材料与结论；§1–§4 为核对材料，§5 为 2026-11-24 评审人 klb3713 的签字（已通过，9.1 已勾选）。
> 被评审对象：[docs/operations/object-search-runbook.md](../../../../docs/operations/object-search-runbook.md)。
> 日期：2026-11-24

## 1. 覆盖对照（任务 9.1 五项 + 相关项）

| 任务要求 | 手册章节 | 状态 |
| --- | --- | --- |
| 重建 | §4 重建索引（整租户 staging→swap→窗口重放→verify；单类型修复 = upsert+删 stale+重放+verify） | 覆盖 |
| 重放 | §5 失败事件与死信重放 | 覆盖 |
| 降级 | §6 降级与恢复（搜索 503、core 不受影响、恢复后自动追平） | 覆盖 |
| 索引清理 | §7 索引清理（列索引、删暂存/旧版本、查 index_state） | 覆盖 |
| 映射升级 | §10 映射/索引版本升级 v1→v2（含回退） | 覆盖 |
| 配置/健康/迁移/故障/指标 | §1 配置、§2 健康检查、§3 迁移、§8 常见故障、§9 指标与告警 | 覆盖 |
| 本地可复现验证 | §12 | 覆盖 |

## 2. 命令复现证据（2026-11-24）

见 [evidence/2026-11-24-object-search-runbook-repro.md](evidence/2026-11-24-object-search-runbook-repro.md) 及原始日志
[evidence/2026-11-24-object-search-runbook-repro.log](evidence/2026-11-24-object-search-runbook-repro.log)：

- §3 `migrate` 两次均 exit 0（幂等，建 `search.index_state`/`search.outbox_attempt`）。
- §4 `rebuild --verify` 由真实 CLI 集成用例 `TestIntegrationRebuildCLIExitAndDigest` 两次 exit 0、digest 一致、注入漂移后非零退出。
- §5 `replay --limit 5` exit 0（events=0）。
- §12 集成测试按 CI 同参（`REQUIRED=1`）exit 0、0 skip。
- 已在 §3 增补“裸进程需先有 `object_data` 业务 schema”的前置条件（避免 `42P01`）。

## 3. 已知边界与未实现项（评审需知）

- 租户注销/索引删除流程**未实现**（安全清单 S10）：当前只能按 §7 手工删除，需后续 change。
- 生产 HA、备份恢复、镜像回滚演练归 `production-delivery`，不在本手册范围。
- 单类型重建为**原地修复**语义（design D15、R-1 已接受），§4 已标注不得替代整租户重建。

## 4. 需评审人决定

1. 是否接受 §4 单类型“原地修复/收敛”语义（D15）。
2. §9 告警阈值建议值是否采用，或需按真实租户规模调整。
3. §7 删除旧索引“保留一版回退”是否满足回退 SLA。

## 5. 签字（已填）

| 角色 | 姓名 | 结论 | 日期 |
| --- | --- | --- | --- |
| 运维手册评审人 | klb3713 | 通过（覆盖 9.1 五项；命令复现证据属实；接受 D15 单类型修复语义与 §9 告警建议值，§7 保留一版回退满足要求） | 2026-11-24 |
