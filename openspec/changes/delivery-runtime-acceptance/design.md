# Design: delivery-runtime-acceptance

> 仅记录跨环境/接口/迁移相关决策与取舍；不重复 tasks 与证据。

## 背景与决策

1. **为何独立成 change 而非 Stage**：负责人 2026-11-28 决策「只新建 change，不建 Stage」。交付运行态验收是一次性的环境性验证工作，不引入新业务 capability，用 Stage 会过重。
2. **与既有 change 的分工**（避免重复）：
   - 本 change：交付资产**基线运行态验收**（6.1–6.5、7.7、E2E-10、S2-AC-08）。
   - `production-delivery-hardening`：生产级**加固**（GitOps 健康门控、真实 `--sync` 回滚、离线完整 bootstrap、verify 正常退出、k3d 等价、CI runner、多副本）。
   - `production-idp-staging`：真实 staging IdP；`jaeger-v2`：Jaeger v2 迁移。
3. **GitOps 源选择**：优先真实 `repoURL` + 只读凭据；若不可用，用固定 SHA 的本地只读 git 服务等价替代，并在证据中标注替代关系（不得冒充真实远端）。

## 替代方案与取舍

- 替代 A：把 6.x/7.7 留在 `stage-2-integration` 直到完成 —— 否决，理由是镜像/环境阻塞会无限期拖住 Stage 2 退出，且发布尚远。
- 替代 B：把 6.x/7.7 并入 `production-delivery-hardening` —— 否决，理由是后者聚焦生产级加固，混入基线验收会模糊边界；拆开使两者可独立验证与归档。

## 风险与回滚

- 风险：本 change 长期不实施 → 交付资产无运行态证据，正式发布前必须补齐（发布强制门）。
- 回滚：本 change 只新增验证证据与最小修复，不改 `production-delivery` 已归档行为；如需撤销仅回退新增脚本/证据，不影响主 spec。

## 证据边界

- 本地/受控 runner + 合成数据，不等于生产验收；未覆盖组合必须在报告中显式标 `not covered`。
