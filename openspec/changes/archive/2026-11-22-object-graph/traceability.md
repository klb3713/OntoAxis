# Traceability: Object Graph

| Stage AC | Capability | Requirement | Evidence |
|---|---|---|---|
| S1-AC-05 | objects | 对象创建与读取 | `internal/modules/objects/service/service_test.go`、`api/handler_test.go` |
| S1-AC-05 | objects | 对象列表与过滤 | `service_test.go` 分页归一化；`repo/store.go` 分支查询 |
| S1-AC-05 | objects | 对象更新与删除 | `service_test.go` 空更新拒绝；`repo/store.go` 软删除 |
| S1-AC-05 | objects | 链接管理 | `api/handler_test.go`、`repo/store.go` 部分唯一索引与冲突映射 |
| S1-AC-05 | objects | 租户边界 | `api/handler_test.go` 缺少租户 401 |
| S1-AC-05 | objects | 错误响应契约 | `api/handler.go` 统一 `httperr`；API 测试断言状态码 |

## Pending Evidence

- 真实 PostgreSQL 迁移与唯一冲突测试（`stage-1-core-integration`）。
- ontology object type 存在性联调。
- 前端 Object Explorer E2E。
