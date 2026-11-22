# Traceability: Action Orchestration

| Stage AC | Capability | Requirement | Evidence |
|---|---|---|---|
| S1-AC-09 | actions | Action Type 管理 | `service_test.go` 空名称/默认状态；`repo/store.go` 部分唯一索引 |
| S1-AC-09 | actions | 校验与预演 | `service_test.go` 必填参数检查；`Preview` 无副作用 |
| S1-AC-09 | actions | 执行入口与 Adapter | `service_test.go` Run 创建 queued；`NoopAdapter` |
| S1-AC-09 | actions | 执行记录与状态操作 | `service_test.go` Cancel 拒绝；Retry 实现 |
| S1-AC-09 | actions | 租户边界与错误契约 | `api/handler.go` 统一 httperr；待 API 测试补齐 |

## Pending Evidence

- API handler 契约测试。
- 真实 PostgreSQL 迁移与 execution 状态集成测试。
- ontology object type 校验接线。
