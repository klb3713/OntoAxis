# Stage 2 · G1 门禁记录（Wave 1 pass）

- 登记日期：2026-11-24
- 登记人：负责人 klb3713（会话内明确确认）
- 门禁定义：`docs/stages/stage-2-change-map.md` §4「Wave 1 — 真实执行」门 G1
- 结论：**pass**

## 1. 门禁条件与证据

| G1 条件 | 证据 | 结果 |
|---|---|---|
| Wave 1 每个 change 的真实实例在 Compose profile 或 kind 中启动 | `connector-runtime`：`evidence/2026-11-23-cdc-e2e-three-way.md`（真实 Kafka Connect/Trino）；`pipeline-kestra-dbt`：`evidence/2026-11-23-kestra-v2-upgrade-e2e.md`；`action-temporal`：`docs/stages/evidence/stage-2-action-temporal.md`（真实 Temporal 1.26.2 + namespace `ontoaxis`） | pass |
| 成功路径、失败重试、拒绝路径均有集成测试 | `connector-runtime`：负向矩阵 18/18（`evidence/2026-11-23-negative-matrix-6-3.md`）；`pipeline-kestra-dbt`：集成/补偿测试（`evidence/2026-11-23-pipeline-publish-compensation-unit.md`）；`action-temporal`：55/55 tasks（`evidence/2026-11-23-action-temporal-archive-review.md`） | pass |
| 外部实例不可用时 adapter 降级行为明确且不冒充成功 | 三个 change 的 archive review 均记录降级/未执行边界（如 connector paused/error 403、action worker 不使用 noop 冒充成功、pipeline 外部实例不可用返回错误而非成功） | pass |
| 授权 change 的跨租户/越权测试全部拒绝 | `authorization-openfga-cedar`：`evidence/2026-11-23-authz-negative-matrix.txt`、`2026-11-23-authz-integration-matrix.md`（29 条负向） | pass |

## 2. Wave 1 change 完成情况

| change | 任务 | 归档目录 | 归档评审 |
|---|---|---|---|
| `connector-runtime` | 25/25 | `openspec/changes/archive/2026-11-23-connector-runtime/` | `evidence/2026-11-23-connector-runtime-archive-review.md` |
| `pipeline-kestra-dbt` | 69/69 | `openspec/changes/archive/2026-11-23-pipeline-kestra-dbt/` | `evidence/2026-11-23-pipeline-kestra-dbt-archive-review.md` |
| `action-temporal`（含关联授权验证） | 55/55 | `openspec/changes/archive/2026-11-23-action-temporal/` | `evidence/2026-11-23-action-temporal-archive-review.md` |
| `authorization-openfga-cedar`（Wave 0，G1 第 4 条引用） | 43/43 | `openspec/changes/archive/2026-11-23-authorization-openfga-cedar/` | `evidence/2026-11-23-authz-archive-review.md` |

## 3. 边界与限制

1. G1 pass 仅表示 Wave 1 三个 change 已按门禁条件完成并可进入 Wave 2 实施；**不等于 Stage 2 退出**，也不代表各 change 的 AC 之外的运维/生产交付能力已具备。
2. `connector-runtime` 遗留 D-07（Connect 与 Kestra 默认端口均为 18086）仍未修，阻断两套 profile 同栈运行，影响 Wave 2 `stage-2-integration` 的联调，不影响本门禁的独立验证结果（三个 change 分别在各自 profile 下验证）。
3. 证据均为本地 Compose profile + 合成数据，未使用真实客户数据或生产凭据。
4. Wave 2 的 G2 门禁（搜索 E2E、Helm/ArgoCD 部署、备份恢复/回滚、非本地 IdP）尚未满足，须另行登记。

## 4. 复现/追溯

- `openspec validate --all --strict`：19 passed（2026-11-23 复跑）。
- 归档提交：`authorization-openfga-cedar`（见其归档提交）、`pipeline-kestra-dbt`、`action-temporal`、`connector-runtime` `20a6e5e`。
- 本记录写入后，`openspec/changes/production-delivery/tasks.md` 1.1 可勾选并引用本文件。
