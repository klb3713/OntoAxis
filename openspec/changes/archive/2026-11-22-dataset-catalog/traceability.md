# Change Traceability: dataset-catalog

> Stage AC 编号只用于本 Stage 追溯，不写进长期 capability 的 requirement 名称。
> 状态从 `planned` 开始；只有证据存在才能改为 `verified` 或说明为 `partial`。

## 1. Stage AC 映射

| Stage AC | Capability / Requirement | Scenario | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S1-AC-06 | dataset-catalog / Dataset catalog records | 创建数据集；同租户名称重复；非法格式；软删除后不可见 | `internal/modules/datasets/service/service_test.go`（创建、重名、非法格式）、`internal/modules/datasets/repo/memory_test.go`（软删除后不可见）、`internal/modules/datasets/api/api_test.go`（201/400/404/409 与删除后 404）、`go test ./internal/modules/datasets/...` | verified |
| S1-AC-06 | dataset-catalog / Branch management | 查询默认分支；创建子分支；分支名称重复 | `internal/modules/datasets/service/service_test.go`（main 分支）、`internal/modules/datasets/repo/memory_test.go`（branch CRUD 与重复名）、`internal/modules/datasets/api/api_test.go`（创建子分支与 branch HTTP 流程） | verified |
| S1-AC-06 | dataset-catalog / Transaction state machine | 开始事务；提交事务；重复提交被拒绝；中止事务 | `internal/modules/datasets/domain/models_test.go`（Commit/Abort 状态机）、`internal/modules/datasets/service/service_test.go`（Begin/Commit/Abort）、`internal/modules/datasets/api/api_test.go`（事务 HTTP 流程与 409） | verified |
| S1-AC-06 | dataset-catalog / Dataset views | 创建视图；视图名称重复；删除视图 | `internal/modules/datasets/service/service_test.go`（view 创建/查询/更新/删除与重复名）、`internal/modules/datasets/api/api_test.go`（view HTTP 流程） | verified |
| S1-AC-06 | dataset-catalog / S3 file metadata | 登记文件元数据；非法文件元数据；删除文件元数据 | `internal/modules/datasets/service/service_test.go`（file 元数据校验与 CRUD）、`internal/modules/datasets/api/api_test.go`（file HTTP 流程、非法文件元数据 400） | verified |
| S1-AC-06 | dataset-catalog / Tenant-scoped API access | 缺失租户上下文；跨租户读取 | `internal/modules/datasets/api/api_test.go`（缺失或非法 `X-Ontoaxis-Tenant-Id` 返回 401 `unauthorized`）、`internal/modules/datasets/module_test.go`（经平台 `httpserver` 返回 401 `unauthorized` JSON）、`internal/modules/datasets/api/api_test.go`（租户 B GET/PATCH 返回 404）、`internal/modules/datasets/service/service_test.go` 与 `internal/modules/datasets/repo/memory_test.go`（跨租户不可见） | verified（单元/HTTP 层） |
| S1-AC-06 | dataset-catalog / Consistent list and error contract | 分页列表；非法分页；未知路由 | `internal/modules/datasets/api/api_test.go`（分页、`per_page=201` 400、未知路由 404）、`internal/modules/datasets/module_test.go`（经平台 `httpserver` 验证未知路由 404 `not_found` JSON）、`internal/modules/datasets/service/service_test.go`（分页默认值与上限） | verified |
| S1-AC-06 | dataset-catalog / 迁移可重复执行（Stage 1 验收条件） | 迁移 DDL 幂等与 RLS 存在 | `internal/modules/datasets/migrations/migrations_test.go`；真实 Postgres 执行记录待 G1 | partial：仅静态/编译验证，待 G1 本地 Postgres 执行 |

## 2. 未覆盖与偏差

| 项 | 原因 | 处理 | 负责人 |
| --- | --- | --- | --- |
| 真实 Postgres 迁移执行 | 本 change 的测试要求不依赖真实 Postgres，环境未授权启动数据库 | 由集成门 G1 在本地 Postgres 执行 `Migrate` 两次并核对 `dataset` schema 与 RLS 策略 | lead / 集成任务 |
| 真实认证与 identity-edge 联调 | 认证模块不在本 change 写入范围，默认 resolver 只接受内部头 | 在 identity-edge 集成后注入认证后的 `TenantResolver`，补充端到端租户隔离测试 | lead / identity-edge |
| Iceberg/Nessie/S3 适配 | 本 change 只保存 Ontology 语义元数据，不实现底层读写 | 后续 change 或 Stage 内独立任务实现，当前不声称已支持 | 后续 owner |
| RLS 运行时生效 | 无真实 Postgres；`PostgresStore` 在租户事务内执行 `set_config('app.tenant_id', $1, true)`，但未在非超级用户角色下执行 | G1 使用非超级用户角色验证 CRUD、跨租户隔离与策略拒绝 | lead / 集成任务 |
| `PreviewData`/`ListVersions` | 依赖底层表与版次读取，超出元数据最小切片 | 后续 change 实现 | 后续 owner |

## 3. 证据索引

| 证据 | 位置 | 生成方式 | 结果/备注 |
| --- | --- | --- | --- |
| Go 单元与 HTTP 测试 | `internal/modules/datasets/**/*_test.go` | `go test ./internal/modules/datasets/...` | 27 passed（6 packages）；合成数据，使用内存 fake store 与 `httptest`，不访问真实服务 |
| 竞态检测 | 终端输出 | `go test -race ./internal/modules/datasets/...` | 27 passed（6 packages） |
| 测试覆盖率 | 终端输出 | `go test -coverprofile=/tmp/datasets.cover ./internal/modules/datasets/...` | 总覆盖率 51.5%；domain 100%、api 66.2%、migrations 69.0%、service 61.4%、module 63.0%、repo 33.1%（`PostgresStore` 无真实库不可测） |
| 格式与静态检查 | 终端输出 | `gofmt -l internal/modules/datasets`、`go vet ./internal/modules/datasets/...`、`go build ./internal/modules/datasets/...` | 均无输出、退出码 0；只证明编译/格式，不证明运行时行为 |
| 租户事务与 RLS 绑定 | `internal/modules/datasets/repo/postgres.go` | 代码审查 | `withTenantTx` 在事务内设置本地 `app.tenant_id`；无真实库执行证据 |
| 平台服务器错误契约 | `internal/modules/datasets/module_test.go` | `go test ./internal/modules/datasets` | 经 `httpserver.New` 验证未知路由 404 `not_found`、缺失租户 401 `unauthorized` 的 JSON 错误结构 |
| 迁移语句结构测试 | `internal/modules/datasets/migrations/migrations_test.go` | `go test ./internal/modules/datasets/migrations/...` | 验证语句幂等关键字与 RLS 覆盖，不等价于真实数据库执行 |
| OpenSpec 校验 | 终端输出 | `openspec validate dataset-catalog --strict` | `Change 'dataset-catalog' is valid`，4/4 artifacts complete |

## 4. 归档检查

- [x] 所有 requirement 至少有一个 scenario；
- [x] 所有 Stage AC 有映射或明确偏差；
- [x] 证据可复核，真实/合成数据分开；
- [ ] `openspec validate --all --strict` 通过（本 change 单独校验通过；全仓校验需在归档门统一执行）；
- [ ] 主规格归档后已核对（本 change 尚未归档）。
