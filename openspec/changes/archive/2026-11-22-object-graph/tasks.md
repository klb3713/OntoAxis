# Tasks: Object Graph

## 1. Change Setup

- [x] 1.1 创建 `openspec/changes/object-graph/` proposal/spec/design/tasks/traceability
- [x] 1.2 确认对象/链接数据所有权与 `object_data` schema 口径

## 2. Domain & Persistence

- [x] 2.1 `internal/modules/objects/domain/object.go`：Object/Link/校验/错误
- [x] 2.2 `internal/modules/objects/repo/store.go`：PostgreSQL 对象/链接 CRUD、分页、软删除、冲突映射
- [x] 2.3 `internal/modules/objects/migrations/20261122180000_objects_init.sql`：`object_data` schema、表、索引、部分唯一索引
- [x] 2.4 `internal/modules/objects/migrations/20261122180000_objects_init.down.sql`

## 3. Service & HTTP

- [x] 3.1 `internal/modules/objects/service/service.go`：接口与默认实现、分页归一化、校验
- [x] 3.2 `internal/modules/objects/api/handler.go`：对象/链接 REST、错误映射、租户解析
- [x] 3.3 `internal/modules/objects/module.go`：模块注册、迁移执行、路由挂载

## 4. Tests

- [x] 4.1 service 单元测试覆盖空 properties、分页归一化、空更新、非法链接
- [x] 4.2 API 测试覆盖缺少租户、创建对象、非法 object_type_id
- [x] 4.3 module 迁移解析/名称/依赖测试
- [x] 4.4 真实 PostgreSQL 迁移与唯一冲突集成测试（由 `stage-1-core-integration` 执行）

## 5. Integration

- [x] 5.1 注册到 `cmd/ontoaxis-core` 模块列表与 `internal/app`
- [x] 5.2 与 ontology 模块联调 object_type_id 校验/绑定
- [x] 5.3 前端 Object Explorer 联调
- [x] 5.4 归档证据：`go test ./internal/modules/objects/...` 输出、API 契约测试、集成测试日志
