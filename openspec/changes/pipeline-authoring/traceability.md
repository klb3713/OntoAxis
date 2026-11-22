# Change Traceability: pipeline-authoring

> Stage AC 编号只用于本 Stage 追溯，不写进长期 capability 的 requirement 名称。
> 状态从 `planned` 开始；只有证据存在才能改为 `verified` 或说明为 `partial`。

## 1. Stage AC 映射

| Stage AC | Capability / Requirement | Scenario | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S1-AC-09 | pipelines / Pipeline definition management | 创建 Pipeline；同租户名称重复；非法类型或定义；软删除后不可见 | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go`、`internal/modules/pipelines/repo/memory_test.go` | verified |
| S1-AC-09 | pipelines / Pipeline publishing and immutable versions | 首次发布；重新发布递增版本；归档拒绝发布；修改已发布定义回到草稿 | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go`、`internal/modules/pipelines/repo/memory_test.go` | verified |
| S1-AC-09 | pipelines / Pipeline run entry | 触发已发布 Pipeline；草稿 Pipeline 拒绝触发；adapter 失败 | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go` | verified |
| S1-AC-09 | pipelines / Pipeline run query | 查询运行列表；跨租户读取运行 | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go` | verified |
| S1-AC-09 | pipelines / Schedule management | 创建 Schedule；非法 cron 或时区；暂停与恢复；删除 Schedule | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go`、`internal/modules/pipelines/domain/cron_test.go` | verified |
| S1-AC-09 | pipelines / Kestra adapter boundary | 默认内存 adapter；adapter 未配置 | `internal/modules/pipelines/kestra/adapter_test.go`、`internal/modules/pipelines/service/service_test.go` | verified（仅 Stage 1 内存骨架；真实 Kestra 见偏差） |
| S1-AC-09 | pipelines / Dataset dependency validation | 引用不存在 dataset；resolver 未注入 | `internal/modules/pipelines/service/service_test.go`、`internal/modules/pipelines/api/api_test.go` | verified |
| S1-AC-09 | pipelines / Tenant-scoped API access | 缺失租户上下文；跨租户读取 Pipeline | `internal/modules/pipelines/api/api_test.go`、`internal/modules/pipelines/service/service_test.go` | verified |
| S1-AC-09 | pipelines / Consistent list and error contract | 分页列表；非法分页 | `internal/modules/pipelines/api/api_test.go`、`internal/modules/pipelines/service/service_test.go` | verified |
| S1-AC-09 | pipelines / 迁移可重复执行（Stage 1 验收条件） | 迁移 DDL 幂等与 RLS 存在 | `internal/modules/pipelines/migrations/migrations_test.go`；真实 Postgres 执行记录 | partial（仅语句结构验证；真实 Postgres 待 G1） |

## 2. 未覆盖与偏差

| 项 | 原因 | 处理 | 负责人 |
| --- | --- | --- | --- |
| 真实 Postgres 迁移执行 | 本 change 的测试要求不依赖真实 Postgres，环境未授权启动数据库 | 由集成门 G1 在本地 Postgres 执行 `Migrate` 两次并核对 `pipeline` schema 与 RLS 策略 | lead / 集成任务 |
| RLS 会话上下文 | 四张表启用 `FORCE ROW LEVEL SECURITY`；连接未设置 `app.tenant_id` 时查询 fail-closed 返回空集 | 由平台在请求/事务中设置租户会话变量；集成门补真实 RLS 读写与跨租户拒绝测试 | lead / 集成任务 |
| 真实 Kestra 适配 | Stage 1 明确不接真实实例，只留接口与内存骨架 | 后续真实适配 change 实现 Flow YAML 转换、Kestra API 与运行状态回写 | 后续 owner |
| 真实认证与 identity-edge 联调 | 认证模块不在本 change 写入范围，默认 resolver 拒绝所有请求 | 在 identity-edge 集成后注入认证后的 `TenantResolver`，补充端到端租户隔离测试 | lead / identity-edge |
| 模块注册到主程序 | `cmd/`、`internal/app/` 不在本 change 写入范围 | 由 `stage-1-core-integration` 完成注册、迁移顺序与依赖注入 | lead / 集成任务 |
| 归档状态 API 与运行状态回写 | proto 未定义归档 RPC，且无真实执行引擎 | 删除置 `archived` 并软删除；运行状态回写留待 Kestra 适配 change | 后续 owner |

## 3. 证据索引

| 证据 | 位置 | 生成方式 | 备注 |
| --- | --- | --- | --- |
| Go 单元与 HTTP 测试 | `internal/modules/pipelines/**/*_test.go` | `go test ./internal/modules/pipelines/...` | 合成数据，使用内存 fake store 与 `httptest`，不访问真实服务 |
| 全仓编译与测试 | 终端输出 | `go build ./...`、`go test ./...` | 证明本模块与当前仓库依赖可编译、测试通过 |
| 格式与静态检查 | 终端输出 | `gofmt -l internal/modules/pipelines`、`go vet ./internal/modules/pipelines/...` | 只证明编译/格式，不证明运行时行为 |
| 迁移语句结构测试 | `internal/modules/pipelines/migrations/migrations_test.go` | `go test ./internal/modules/pipelines/migrations/...` | 验证语句幂等关键字与 RLS 覆盖，不等价于真实数据库执行 |
| OpenSpec 校验 | 终端输出 | `openspec validate pipeline-authoring --strict` | 验证规格结构与 requirement/scenario 完整性 |

## 4. 归档检查

- [ ] 所有 requirement 至少有一个 scenario；
- [ ] 所有 Stage AC 有映射或明确偏差；
- [ ] 证据可复核，真实/合成数据分开；
- [ ] `openspec validate --all --strict` 通过；
- [ ] 主规格归档后已核对。
