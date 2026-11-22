# Traceability: Query Surface

| Stage AC | Capability | Requirement | Evidence |
|---|---|---|---|
| S1-AC-07 | query | 保存查询 CRUD | `service_test.go` 名称/SQL 校验；`repo/store.go` 部分唯一索引 |
| S1-AC-07 | query | 执行只读 SQL | `service_test.go` 拒绝非 SELECT；`trino/client_test.go` nextUri |
| S1-AC-07 | query | 解释查询 | `api/handler_test.go` explain 200 |
| S1-AC-07 | query | 对象查询 | `api/handler_test.go` 对象查询待补；`service.QueryObjects` |
| S1-AC-07 | query | 租户边界与错误契约 | `handler_test.go` 401/400；`httperr` |

## Pending Evidence

- API handler 对象查询测试。
- 真实单节点 Trino E2E（`stage-1-core-integration`）。
- 真实 PostgreSQL 迁移与 RLS 验证。
