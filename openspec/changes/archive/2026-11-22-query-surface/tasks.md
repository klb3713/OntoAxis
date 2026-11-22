# Tasks: Query Surface

## 1. Change Setup

- [x] 1.1 创建 proposal/spec/design/tasks/traceability

## 2. Persistence & Domain

- [x] 2.1 `domain/query.go`：SavedQuery/QueryResult/只读 SQL 校验
- [x] 2.2 `repo/store.go`：SavedQuery PostgreSQL CRUD
- [x] 2.3 `migrations/20261122190000_query_init.sql` 与 RLS

## 3. Engine & Service

- [x] 3.1 `trino/client.go`：HTTP statement、nextUri、错误
- [x] 3.2 `service/service.go`：Execute/Explain/SavedQuery CRUD/QueryObjects
- [x] 3.3 `api/handler.go`：`/api/v1/query/*` 路由
- [x] 3.4 `module.go`：模块注册、迁移、路由

## 4. Tests

- [x] 4.1 service 单元测试：非法 SQL、未配置引擎、保存校验、分页
- [x] 4.2 Trino client 测试：nextUri、错误
- [x] 4.3 API handler 契约测试：tenant、execute、explain、save、delete
- [x] 4.4 module 迁移/依赖测试

## 5. Integration

- [x] 5.1 注册到 `cmd/ontoaxis-core` 模块列表并注入 objects Service
- [x] 5.2 Compose 启动真实单节点 Trino
- [x] 5.3 Stage 1 E2E：执行真实 SELECT 与对象查询
