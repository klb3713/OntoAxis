# Stage 2 剩余 change 批量评审包

日期：2026-11-22<br>
Stage：Stage 2（`in_progress`，2026-11-22 批准）<br>
状态：**规划评审已通过（2026-11-22）**。7 个 change 均未实施、未归档、未 commit；所有 tasks 未勾选。实施需按 change 单独启动。

## 1. 本批 change

| Change | Wave | Stage AC | 规划文件 | Task 数 | 关键评审点 |
| --- | --- | --- | --- | ---: | --- |
| `authorization-openfga-cedar` | 0 | S2-AC-03 | proposal / specs / design / tasks / traceability | 43 | 授权边界文档安全评审；OpenFGA/Cedar 单一判定点；默认拒绝与审计 |
| `connector-runtime` | 1 | S2-AC-06 | proposal / specs（connectors + ingestion） / design / tasks / traceability | 25 | Kafka Connect/Debezium 生命周期；快照完成判定；Iceberg sink 插件可用性 |
| `pipeline-kestra-dbt` | 1 | S2-AC-05 | proposal / specs（pipelines） / design / tasks / traceability | 69 | Kestra 版本/端点；Flow IR 映射；状态对账；dbt 执行边界 |
| `action-temporal` | 1 | S2-AC-04 | proposal / specs（actions） / design / tasks / traceability | 55 | Temporal worker/namespace；重试/补偿；审批信号与最小 UI；幂等表 TTL |
| `object-search-meilisearch` | 2 | S2-AC-07 | proposal / specs（object-search） / design / tasks / traceability | 52 | 租户索引隔离；outbox 一致性；重建/回滚；敏感字段进入全文索引 |
| `production-delivery` | 2 | S2-AC-08、S2-AC-10 | proposal / specs / design / tasks / traceability | 66 | kind/k3d 证据栈；Secret 供给；备份/恢复与回滚；staging IdP |
| `stage-2-integration` | 3 | S2-AC-01、S2-AC-11 | proposal / design / tasks / traceability（`skip_specs: true`） | 52 | 7 个前置 change 归档后才可实施；共享文件所有权；全栈 E2E 与 Stage 报告 |

## 2. 验证

- `openspec validate --all --strict`：`20 passed / 0 failed`。
- 每个 change：`openspec status --change <name>` 均为 4/4 artifacts（`stage-2-integration` 为 3/3，specs skipped）。
- 所有 tasks 均为未勾选状态，没有伪完成；没有代码/部署资产被修改。

## 3. 负责人评审结论（2026-11-22）

1. **授权边界**：`docs/architecture/authorization-boundary.md` 已定稿；负责人确认安全评审通过（其他会话）。`authorization-openfga-cedar` 仍需单独实施授权。
2. **门禁记录**：G0/G1 均不在规划阶段标记 `pass`。G0 在 authz 实现并通过负向测试后登记；G1 在 Wave 1 三个 change 完成后登记。
3. **版本与形态**：OpenFGA、Kestra、Temporal、Meilisearch 固定具体稳定 patch + 镜像 digest，不使用 `latest`；Stage 2 使用单实例 + PostgreSQL，HA/生产拓扑后续演进。
4. **共享文件归属**：`internal/app`/`cmd`/`go.mod` 由 `stage-2-integration` 收口；`deploy/` 由 `production-delivery` 收口；各 change 只交 patch。
5. **审批 UI**：`action-temporal` 纳入最小 `/approvals` 页面（列表、详情、Approve/Reject + 备注），不做流程设计器/批量审批。
6. **主证据栈**：`kind` 为唯一 CI/Stage 验收栈；`k3d` 仅开发者可选，不进入验收。
7. **证据边界**：本地 kind/Compose 通过不等于生产验收；外部 IdP、真实 secret provider、真实云账号均不在 Stage 2 证据范围。

## 4. 建议评审顺序

1. 先审 `authorization-openfga-cedar` 与授权边界（Wave 0 唯一阻塞项）。
2. 再审 Wave 1 三个 change 的真实运行时边界与版本钉死项。
3. 再审 Wave 2 的搜索索引与生产交付。
4. 最后审 `stage-2-integration` 的装配顺序、共享文件所有权与 E2E 矩阵。

> 本评审包只汇总规划状态；批准某个 change 后才可按其 tasks 实施。Stage 2 退出仍需全部 change 归档、AC 证据与负责人确认。
