# Traceability

## Stage AC 映射

| Stage AC | Capability | Requirement / Scenario | Evidence | Status |
|---|---|---|---|---|
| S1-AC-04 | `ontology-metadata` | Object Type Creation / Create a valid object type；Reject duplicate object type name in the same tenant；Allow the same object type name in different tenants | `internal/modules/ontology/service/impl_test.go` 的 `TestCreateObjectTypeNormalizesAndGeneratesUUIDv7`、`TestCreateObjectTypeRejectsDuplicateAndInvalidInput`、`TestSameNameIsAllowedForDifferentTenantsAndReadsAreScoped`；`internal/modules/ontology/api/handler_test.go` 的 `TestCreateObjectTypeContract`、`TestFixtureDrivenObjectTypeContract` | verified（单元与 HTTP 契约） |
| S1-AC-04 | `ontology-metadata` | Object Type Retrieval and Listing / Get an existing object type；Get a missing object type；List object types with pagination | `internal/modules/ontology/service/impl_test.go` 的 `TestObjectTypePaginationAndUpdate`、`TestCrossTenantMutationsAreNotFound`；`internal/modules/ontology/api/handler_test.go` 的 `TestListObjectTypesPaginationContract`、`TestServiceErrorMapping` | verified（单元与 HTTP 契约） |
| S1-AC-04 | `ontology-metadata` | Object Type Update and Deletion / Update mutable object type fields；Delete an unreferenced object type；Reject deletion of a referenced object type | `internal/modules/ontology/service/impl_test.go` 的 `TestObjectTypePaginationAndUpdate`、`TestLinkTypeLifecycleAndReferencedObjectTypeDeletion` | verified（单元） |
| S1-AC-04 | `ontology-metadata` | Property Management / Create a property on an existing object type；Reject property creation for a missing object type；Reject duplicate property name within an object type；Update and delete a property | `internal/modules/ontology/service/impl_test.go` 的 `TestPropertyLifecycle`；`internal/modules/ontology/api/handler_test.go` 的 `TestOntologyRouteTable` | verified（单元与路由契约） |
| S1-AC-04 | `ontology-metadata` | Link Type Management / Create a valid link type；Reject link type with missing endpoint；Reject unsupported cardinality；Update and delete a link type | `internal/modules/ontology/service/impl_test.go` 的 `TestLinkTypeLifecycleAndReferencedObjectTypeDeletion`；`internal/modules/ontology/api/handler_test.go` 的 `TestServiceErrorMapping`、`TestOntologyRouteTable` | verified（单元与路由契约） |
| S1-AC-04 | `ontology-metadata` | Dataset Binding Lifecycle / Bind a dataset to an object type；Unbind a dataset from an object type；Reject binding for a missing object type | `internal/modules/ontology/service/impl_test.go` 的 `TestDatasetBindingLifecycleAndMaterialization`；`internal/modules/ontology/api/handler_test.go` 的 `TestOntologyRouteTable` | verified（单元与路由契约） |
| S1-AC-04 | `ontology-metadata` | Binding Materialization Acceptance / Accept materialization for a bound object type；Reject materialization without binding | `internal/modules/ontology/service/impl_test.go` 的 `TestDatasetBindingLifecycleAndMaterialization` | verified（单元） |
| S1-AC-04 | `ontology-metadata` | Tenant Isolation / Cross-tenant read is hidden；Cross-tenant mutation is rejected；Missing tenant context is rejected | `internal/modules/ontology/service/impl_test.go` 的 `TestSameNameIsAllowedForDifferentTenantsAndReadsAreScoped`、`TestCrossTenantMutationsAreNotFound`；`internal/modules/ontology/api/handler_test.go` 的 `TestTenantContextIsRequired`、`TestServiceErrorMapping` | verified（单元与 HTTP 契约） |
| S1-AC-04 | `ontology-metadata` | Validation and Error Responses / Reject malformed request body；Reject invalid object type name；Reject invalid pagination | `internal/modules/ontology/domain/validate_test.go` 的 `TestValidateName`、`TestValidatePageAndNormalizePage`、`TestValidateCardinality`、`TestValidateSyncModeAndPropertyType`；`internal/modules/ontology/api/handler_test.go` 的 `TestMalformedBodiesAreRejected`、`TestInvalidPaginationIsRejected`、`TestServiceErrorMapping` | verified（单元与 HTTP 契约） |
| S1-AC-04 | `ontology-metadata` | Metadata Persistence Boundary / Apply ontology migration repeatedly；Reject project resource management through ontology | `internal/modules/ontology/migrations/schema_test.go` 的 `TestStatementsCreateOwnSchemaAndTenantIsolation`、`TestEveryStatementIsIdempotentOrPolicyReplacement`、`TestDownStatementsAreScopedToOntologySchema`；`internal/modules/ontology/module_test.go` 的 `TestModuleDoesNotExposeProjectRoutes`；`go build ./internal/modules/ontology/...` | verified（迁移语句与代码静态证据；未连接真实 PostgreSQL） |
| S1-AC-04 | `ontology-metadata` | Module Registration Contract / Register ontology module；Report readiness without database；Unknown ontology route remains not found | `internal/modules/ontology/module_test.go` 的 `TestModuleIdentityAndDependencies`、`TestModuleRegistersFailClosedRoutes`、`TestModuleNotReadyWithoutPool`、`TestModuleDoesNotExposeProjectRoutes`；`internal/modules/ontology/api/handler_test.go` 的 `TestOntologyRouteTable` | verified（模块与路由契约） |

## 未覆盖与偏差

- 本 change 不连接真实 PostgreSQL；仓储 SQL、RLS 与迁移的运行时行为只由代码审查、`go build`、`go vet` 与迁移语句测试覆盖，尚未进行集成数据库验证。真实数据库验收必须由集成 change 或授权环境补证，不能把当前证据写成生产验收。
- `MaterializeBinding` 仅受理并返回 `operation_id`，不创建真实异步任务、不执行数据同步；真实物化由后续 datasets/worker change 替换。
- 认证/JWT 与租户解析中间件不在本 change 范围；`api.WithTenantID` 只是平台 authn 落地前的 context 注入点，缺少租户上下文时全部业务路由 fail-closed 返回 `401`。
- 未实现对象实例、查询执行、数据集实际读取与前端页面；这些属于后续 change。
- 本 change 未修改 `go.mod`/`go.sum`、`cmd/`、`internal/app/`、`internal/platform/`、`deploy/` 或其他模块目录；`ontology` 模块接入进程模块列表由集成 change 负责。
- 本 change 尚未归档，也不代表 Stage 1 完成；归档需评审确认并运行 `openspec validate --all --strict`。

## 证据索引

- `go test -count=1 ./internal/modules/ontology/...`：通过（6 个包，35 个顶层测试通过；domain、service、repo、migrations、api 与模块测试均覆盖）。
- `go vet ./internal/modules/ontology/...`：通过，无输出。
- `gofmt -l internal/modules/ontology`：无输出（所有 Go 文件已格式化）。
- `go build ./internal/modules/ontology/...`：通过。
- `openspec validate ontology-definition --strict`：通过，输出 `Change 'ontology-definition' is valid`；`openspec status --change ontology-definition` 为 4/4 artifacts complete。
- `openspec validate --all --strict`：`ontology-definition` 通过；聚合命令仍因其他未完成 change（action-orchestration、frontend-core、object-graph、query-surface、stage-1-core-integration 缺少 delta）非零退出，不属于本 change 范围。
- 契约 fixture：`internal/modules/ontology/api/testdata/ontology_fixture.json`，由 `TestFixtureDrivenObjectTypeContract` 读取并断言请求/响应关键字段。

## 归档检查

- [x] proposal、delta spec、design、tasks、traceability 均存在。
- [x] tasks 的完成项有对应测试或命令证据。
- [ ] `openspec validate --all --strict` 整体通过（本 change 已通过；其他 change 尚未完成 delta，聚合命令仍失败）。
- [ ] 评审人确认并归档 change（本 change 不自行归档、不 commit、不宣称 Stage 完成）。
