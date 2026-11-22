# Tasks: Action Orchestration

## 1. Change Setup

- [x] 1.1 创建 proposal/spec/design/tasks/traceability

## 2. Domain & Persistence

- [x] 2.1 `domain/action.go`：ActionType/Execution/状态/错误
- [x] 2.2 `repo/store.go`：Action Type 与 Execution PostgreSQL CRUD
- [x] 2.3 `migrations/20261122190000_actions_init.sql` 与 down 脚本

## 3. Service & HTTP

- [x] 3.1 `service/service.go`：CRUD、Validate/Preview、Run、Cancel/Retry、NoopAdapter
- [x] 3.2 `api/handler.go`：`/api/v1/actions` REST 路由与错误映射
- [x] 3.3 `module.go`：模块注册、迁移、路由挂载

## 4. Tests

- [x] 4.1 service 单元测试：空名称、默认状态、必填参数、Run、Cancel 拒绝
- [x] 4.2 module 迁移解析/依赖测试
- [x] 4.3 API handler 契约测试
- [x] 4.4 真实 PostgreSQL 迁移与 execution 状态集成测试

## 5. Integration

- [x] 5.1 注册到 `cmd/ontoaxis-core` 模块列表
- [x] 5.2 ontology object type 校验接线
- [x] 5.3 Stage 1 E2E：创建 Action Type → 校验 → 预演 → 运行（API 级，真实 PostgreSQL + ontology 校验）
