# Traceability: action-temporal

> 规划阶段记录；状态为 `planned` 的条目必须在实现并产生证据后改为 `verified`，不得提前标记通过。真实实例证据只接受在真实 Temporal + PostgreSQL 上产生的运行记录；单元测试、mock、Playwright mock 与本地 dev server 不能冒充真实实例证据。
>
> 2026-11-23：unit 证据见 `openspec/changes/action-temporal/evidence/2026-11-23-action-temporal-unit.md`；本表仅将纯 unit/workflow 条目改为 `verified`，任何含 real-instance/smoke 的条目保持 `planned` 并在证据类型列标注“unit 已完成、E2E 待跑”。

## 1. Stage 2 AC 映射（S2-AC-04）

| Stage AC | AC 原文要点 | 本 change 的行为/能力 | Requirement / Scenario | 计划证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| S2-AC-04 | Action 通过真实 Temporal 执行：至少一个 workflow 包含 activity 失败重试、补偿和人工审批分支；替换 Stage 1 的 no-op adapter | 真实 Temporal workflow/activity 边界；执行定义快照与状态机；重试/退避、saga 补偿、人工审批、取消/超时；默认执行模式切换为 temporal，noop 仅显式降级 | `真实实例端到端验收` / `真实实例覆盖重试、补偿与审批`、`真实实例覆盖拒绝与耗尽路径`、`未执行不冒充通过`；`失败重试与退避` / 全部 3 场景；`补偿` / 全部 3 场景；`人工审批` / `审批通过后继续执行`、`审批拒绝触发补偿`、`审批超时` | `scripts/actions-temporal-smoke.sh` 运行记录；`docs/stages/evidence/stage-2-action-temporal.md`；CI 真实实例 job 日志 | planned |
| S2-AC-04（依赖） | 授权与审计接缝由 `authorization-openfga-cedar` 提供 | 所有 actions 读写与审批决定经过授权守卫；审计 fail-closed | `授权守卫` / 全部 4 场景；`执行审计事件` / 全部 3 场景 | 授权拒绝/跨租户 404/授权不可用 503 的集成测试；审计事件断言；授权变更负责人确认的 action 目录 | planned |
| G1 门禁（change-map） | 真实实例在 Compose profile 或 kind；成功、失败重试、拒绝集成测试；adapter 退化显式；授权跨租户拒绝 | Temporal profile 片段 + worker；显式 noop；跨租户 404 | `执行入口与 Adapter` / `显式 no-op 降级`；`租户边界与错误契约` / `跨租户访问返回未找到`；`真实实例端到端验收` | compose profile 片段与 smoke 输出；跨租户测试；证据文件边界说明 | planned |

## 2. Requirement → 验证方式

| Requirement（delta spec） | 验证方式 | 证据类型 | 状态 |
| --- | --- | --- | --- |
| Action Type 执行定义 | `ValidateDefinition` 单测（合法/未知 activity/步骤上限/越界/缺补偿）；执行快照单测；非 active 类型 Run 拒绝测试 | unit | verified |
| 执行状态与终态定义 | 终态不可变单测；状态集合完整性断言 | unit | verified |
| 状态转换与并发控制 | 状态转换表驱动单测；CAS 并发测试；workflow 成功路径测试 | unit + workflow | verified |
| 幂等与重复提交 | 幂等键同 hash/异 hash/缺 key 单测；`REJECT_DUPLICATE` 测试；activity 重放幂等测试；smoke 重复提交断言 | unit + integration | planned |
| 失败重试与退避 | workflow testsuite 可重试/不可重试错误测试；smoke 断言 attempt 与重试计数；耗尽路径测试 | unit + real-instance | planned |
| 补偿 | workflow 逆序补偿测试；补偿失败 `compensation_failed` 测试；smoke 断言对象快照恢复与残留清单 | unit + real-instance | planned |
| 人工审批 | 审批 service CAS 测试；workflow 信号/定时器测试；handler 授权与幂等测试；Playwright mock E2E；smoke 批准/拒绝/超时分支 | unit + UI + real-instance | planned |
| 取消与超时 | service/handler 取消状态组合测试；workflow 取消与定时器测试；smoke 取消/超时断言 | unit + real-instance | planned |
| 授权守卫 | Authorizer 端口单测（允许/拒绝/不可用/未配置启动失败）；API 顺序测试；跨租户 404 测试；审批决定拒绝测试 | unit + integration | planned |
| 执行审计事件 | 事件字段与脱敏断言；API 审计失败阻断副作用测试；worker 审计失败重试测试；smoke 事件可查 | unit + real-instance | planned |
| 执行可观测性 | 指标标签白名单单测；worker readyz 测试；trace 父子 span 断言；observability smoke | unit + smoke | planned |
| 审批界面 | Vitest 组件测试（加载/空态/错误/无权限）；Playwright mock E2E（批准/拒绝） | UI | planned |
| 真实实例端到端验收 | 真实 Temporal + PostgreSQL 上运行 smoke 与 env-gated 集成测试；证据文件记录命令/环境/执行 ID | real-instance | planned |
| 校验与预演（MODIFIED） | handler/service 预演测试：模式上报、审批要求、步骤摘要、无副作用、无记录 | unit | verified |
| 执行入口与 Adapter（MODIFIED） | service 提交成功/引擎失败投影/显式 noop 测试；smoke 默认 temporal 断言 | unit + real-instance | planned |
| 执行记录与状态操作（MODIFIED） | 取消/重试各状态组合测试；新执行不修改原执行；详情字段断言 | unit | verified |
| 租户边界与错误契约（MODIFIED） | 缺失租户 401、跨租户 404、授权 403、依赖不可用 503、错误信封与脱敏断言 | unit + integration | planned |

## 3. 未覆盖 / 延后

| 条目 | 原因 | 处理 |
| --- | --- | --- |
| `action_execution:approve` 授权目录扩展 | 授权目录由 `authorization-openfga-cedar` 拥有；本变更不能自行扩展授权真源 | 依赖 1.1 决策；若未新增则审批映射为既有 action + 服务端关系校验，并记录风险 |
| 自审批禁止 | 授权文档无此约束，属安全评审项 | design.md Open Question 2；不默认实现 |
| 步骤/审批/幂等记录留存与清理 | 平台无统一归档策略 | design.md Open Question 4；本变更只保证 24h 幂等 TTL |
| ARCHITECTURE §5.1 新状态/新表更新 | 正文归架构文档与评审 | design.md D5/D6 差异记录 + task 10.3 |
| Helm/ArgoCD 编排与生产 Temporal | `deploy/` 归 `production-delivery` | 本变更只提供 compose profile 片段与 worker 源码；真实生产编排不在验收内 |
| 跨模块 outbox / 事件总线 | 不属于 S2-AC-04 范围 | 审计沿用授权变更 Sink；后续独立变更 |
| 真实 OIDC + 真实后端审批 UI E2E | 最小 UI 只承诺 mock E2E；真实后端联调需 Keycloak/授权环境 | 真实实例 smoke 覆盖 API 分支；UI 真实联调列为延后 |
| Action Type 管理 UI | 非本变更目标 | 仅 API 与最小审批 UI |

## 4. 证据索引

| 路径 | 内容 | 状态 |
| --- | --- | --- |
| `docs/stages/evidence/stage-2-action-temporal.md` | 真实实例 smoke 命令、环境、执行 ID、重试/补偿/审批/拒绝/耗尽结果与边界说明 | planned |
| `scripts/actions-temporal-smoke.sh` 输出日志 | 机器可读的 E2E 结果（命令、退出码、断言） | planned |
| CI 真实实例 job 日志 | env-gated 集成测试与 smoke 的 CI 运行链接 | planned |
| `openspec/changes/action-temporal/evidence/2026-11-23-action-temporal-unit.md` | 本 change 已执行的 unit/workflow/API/UI 门禁与环境、限制 | verified（unit 范围） |
| `make build/test/test-race/vet/lint/license-check` 输出 | 静态与单元门禁结果；build/test/race/vet 已跑，lint/license-check 未跑 | partial |
| `make proto-lint/proto-breaking/generate-check` 输出 | 契约兼容与生成物一致性 | planned |
| `cd web && pnpm check/test/build` 与 Playwright 报告 | 前端门禁与审批 UI mock E2E | planned |
| `openspec validate action-temporal --strict` 输出 | 规格与任务一致性 | planned（本文件写入后即可先跑一次规划态校验） |
| 评审记录（授权 action 名、worker 编排归属、状态枚举处理） | 开放决策的书面结论 | planned |

## 5. 备注

- 本表与 `specs/actions/spec.md`、`design.md`、`tasks.md` 同步维护；Requirement/Scenario 名称以 delta spec 为准，重命名时必须同步本表。
- 任务勾选前必须有对应证据；`verified` 状态只能由证据产物支撑，且证据路径必须可复核。
- 集成验证组（tasks 11）只核对整合结果；若某组测试缺失，回到对应组补齐，不在集成组补实现。
