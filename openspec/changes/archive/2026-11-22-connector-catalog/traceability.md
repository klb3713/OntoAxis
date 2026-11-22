# Traceability

| Stage AC | Requirement / Scenario | Evidence | Status |
|---|---|---|---|
| S1-AC-08 | `connectors` / Connector 创建、同租户重名拒绝、跨租户同名可见 | `internal/modules/connectors/service` 单测与 `internal/modules/connectors/api` httptest（E4） | verified |
| S1-AC-08 | `connectors` / Connector 类型与配置校验、敏感配置拒绝、响应不回显 `secret_ref` | `internal/modules/connectors/domain` 与 API 单测（E4） | verified |
| S1-AC-08 | `connectors` / 查询、分页、软删除过滤、更新与删除 | `internal/modules/connectors/service`、`repo` 编译与 httptest（E3、E4） | verified |
| S1-AC-08 | `connectors` / 租户隔离（缺租户 400、跨租户 404） | `internal/modules/connectors/api` httptest（E4） | verified |
| S1-AC-08 | `connectors` / Test、InferSchema、StartSync 适配器行为与 fail-closed | `internal/modules/connectors/service` 单测（适配器成功/不可用，E4） | verified |
| S1-AC-08 | `ingestion` / SyncJob 创建、查询、列表、取消 | `internal/modules/ingestion/service`、`api` 单测与 httptest（E4） | verified |
| S1-AC-08 | `ingestion` / 状态机与非法迁移拒绝 | `internal/modules/ingestion/domain` 与 `service` 单测（E4） | verified |
| S1-AC-08 | `ingestion` / Watch 在终态立即结束 | `internal/modules/ingestion/api` SSE httptest 与 `service` 单测（E4） | verified |
| S1-AC-08 | `ingestion` / SyncRun 状态、统计、错误与开始/结束时间投影 | `internal/modules/ingestion/service` 单测（E4） | verified |
| S1-AC-08 | 两个模块的 `platform.Module` 契约（Name/Dependencies/Migrate/RegisterRoutes/Ready）与迁移幂等 DDL | 模块测试、迁移测试与 E1–E6 | verified |

## 证据

以下命令在仓库根目录 `/Users/klb3713/work/OntoAxis` 执行，Go 工具链为 `/tmp/gotool/go`（实际 `go version go1.26.0 darwin/amd64`），日期为 2026-11-22。

| ID | 命令 | 结果 |
|---|---|---|
| E1 | `gofmt -l internal/modules/connectors internal/modules/ingestion` | 无输出，退出码 0 |
| E2 | `go vet ./internal/modules/connectors/... ./internal/modules/ingestion/...` | 无输出，退出码 0 |
| E3 | `go build ./...` | 退出码 0 |
| E4 | `go test -count=1 ./internal/modules/connectors/... ./internal/modules/ingestion/...` | 12 个包均通过（10 个包 `ok`：`connectors`、`api`、`domain`、`migrations`、`service`；`ingestion`、`api`、`domain`、`migrations`、`service`；两个 `repo` 包无测试文件），`-json` 统计 68 pass / 0 fail，退出码 0 |
| E5 | `openspec validate connector-catalog --strict` | 输出 `Change 'connector-catalog' is valid`，退出码 0 |
| E6 | `go test ./...` | 全仓测试退出码 0，无 `FAIL` 输出（本 change 落地时快照） |

## 未验证与适配边界

- 未连接真实 PostgreSQL：迁移 DDL 只通过迁移测试的字符串断言与 `go build` 验证，尚未在真实 PG 上执行；RLS、索引与重复执行行为需由集成环境验证。连接池还需要在事务/会话中设置 `app.tenant_id`（平台管理员设置 `app.is_platform_admin`），否则 FORCE RLS 会拒绝访问。
- 未实现真实 Meltano/Kafka Connect/Debezium 适配器：`TestConnector`、`InferSchema`、`StartSync` 在未注入适配器时分别返回 503/501 或 `ErrUnavailable`，不返回伪造成功结果；真实 job 标识与状态映射仍待适配。
- `connectors.StartSync` 没有默认 `SyncStarter`；集成时需把 `ingestion.Service`（其 `StartSync` 签名匹配）注入 connectors，否则该路由保持 503。
- `sync_jobs` 未增加 `message` 持久化列，`IngestJob.message` 目前只在服务状态迁移的返回值中体现，GET 详情/列表返回空字符串；若要求持久化，需在后续迁移中扩展表结构。
- 模块尚未注册到 `internal/app` 与 `cmd/`：这两个目录不在本 change 写入范围，由 `stage-1-core-integration` 负责装配并验证注册顺序 `tenancy → connectors → ingestion`。
- tenancy 的真实服务接口未在模块内直接调用；HTTP 层只校验网关注入的 `X-Ontoaxis-Tenant-Id`，与 tenancy 的集成需由集成 change 验证。
- 本 change 的完成不等于 Stage 1 完成；归档与 Stage 1 退出评审仍需负责人确认。
