# Traceability: Tenancy Workspaces

## Stage 1 验收条件映射

| Stage AC | 要求（Requirement） | 场景（Scenario） | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S1-AC-03 | 组织管理 | 创建组织；按租户隔离组织列表；拒绝无效组织输入 | `internal/modules/tenancy/service_test.go` 的 `TestServiceOrganizationLifecycle`、`TestServiceValidation`；`internal/modules/tenancy/api_test.go` 的 `TestAPIOrganizationCRUDAndTenantErrors`、`TestAPIUpdateAndDeleteRoutes` | verified |
| S1-AC-03 | 工作区管理 | 创建组织下的工作区；拒绝跨租户组织绑定 | `internal/modules/tenancy/service_test.go` 的 `TestServiceWorkspaceProjectAndResourceLifecycle`、`TestServiceValidation`；`internal/modules/tenancy/api_test.go` 的 `TestAPIWorkspaceProjectAndResourceFlow`、`TestAPIUpdateAndDeleteRoutes` | verified |
| S1-AC-03 | 成员关系管理 | 添加成员关系；拒绝非法主体类型；移除成员关系 | `internal/modules/tenancy/service_test.go` 的 `TestServiceMembership`；`internal/modules/tenancy/api_test.go` 的 `TestAPIMembershipFlow` | verified |
| S1-AC-03 | 项目管理 | 创建项目；更新项目；跨租户读取项目被拒绝 | `internal/modules/tenancy/service_test.go` 的 `TestServiceWorkspaceProjectAndResourceLifecycle`、`TestServiceTenantIsolation`；`internal/modules/tenancy/api_test.go` 的 `TestAPIUpdateAndDeleteRoutes` | verified |
| S1-AC-03 | 项目资源绑定 | 绑定项目资源；重复绑定冲突；解绑项目资源 | `internal/modules/tenancy/service_test.go` 的 `TestServiceWorkspaceProjectAndResourceLifecycle`；`internal/modules/tenancy/api_test.go` 的 `TestAPIWorkspaceProjectAndResourceFlow` | verified |
| S1-AC-03（租户隔离部分） | 租户边界 | 缺少租户标识；租户标识格式非法；上下文与请求头冲突 | `internal/modules/tenancy/service_test.go` 的 `TestServiceTenantIsolation`；`internal/modules/tenancy/api_test.go` 的 `TestAPIOrganizationCRUDAndTenantErrors`、`TestAPIContextTenantIsUsed`、`TestAPIAuthnTenantContextIsUsed` | verified |
| S1-AC-03（错误契约部分） | 错误响应契约 | 资源不存在；内部错误不泄漏细节 | `internal/modules/tenancy/api_test.go` 的 `TestAPIOrganizationCRUDAndTenantErrors`、`TestAPIInternalErrorDoesNotLeakDetails`；`internal/platform/httperr` 统一信封 | verified |

## 证据记录

- 测试命令：`docker run --rm -v /Users/klb3713/work/OntoAxis:/src -w /src golang:1.26.3 sh -c 'gofmt -w internal/modules/tenancy/*.go && go test -v ./internal/modules/tenancy/... && go vet ./internal/modules/tenancy/...'`。结果：全部测试 PASS，`ok github.com/klb3713/ontoaxis/internal/modules/tenancy 0.005s`，`go vet` 退出码 0。
- 最近一次复验：`gofmt -l internal/modules/tenancy` 输出为空；`go vet ./internal/modules/tenancy/...` 退出码 0；`go test ./internal/modules/tenancy/...` 输出 `ok github.com/klb3713/ontoaxis/internal/modules/tenancy 0.004s`。
- 测试覆盖：19 个顶层测试函数（含 `TestServiceValidation` 的 12 个表驱动子测试与 `TestStoreErrMappings` 的 4 个子测试），覆盖成功、无效输入、租户隔离、404/409/500 与认证上下文路径；测试使用内存 `fakeStore` + `httptest`，不依赖真实 Postgres。
- OpenSpec 状态：`openspec status --change tenancy-workspaces` 返回 `Progress: 4/4 artifacts complete`；`openspec validate tenancy-workspaces --strict` 返回 `Change 'tenancy-workspaces' is valid`。仓库级 `openspec validate --all --strict` 为 7 passed / 5 failed，失败均来自其他尚未编写 delta specs 的在途 change（`action-orchestration`、`frontend-core`、`object-graph`、`query-surface`、`stage-1-core-integration`），本 change 不在失败列表。

## 未决风险

- RLS 与 `db.WithTenant` 未实现，租户隔离当前依赖 repo 显式 `tenant_id` 过滤，需在平台/集成 change 补强。
- REST 适配层为临时协议层，最终 Connect handler 切换需在 `stage-1-core-integration` 验证字段兼容性。
- 真实 PostgreSQL 迁移与约束行为未验证，需在集成环境执行 Up/Down 并记录结果。
- `X-Tenant-Id` 兜底仅用于认证中间件尚未装配的本地/集成前阶段；生产路径应只信任 `authn.FromContext` 注入的租户身份。
