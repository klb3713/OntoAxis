# Evidence: object-search-meilisearch (unit slice)

> 状态：实现中证据，非真实 Meilisearch/PostgreSQL E2E 验收。
> 日期：2026-11-23
> 范围：索引命名/映射/客户端/配置/消费者/重建/worker 与 objects outbox + 搜索 API。

## 环境

| 项 | 值 |
| --- | --- |
| Go | 1.26.3，`/tmp/go/bin/go` |
| 模块缓存 | `GOMODCACHE=/tmp/gomod` |
| Meilisearch | 未启动真实实例；client 测试使用 `httptest.Server` |
| PostgreSQL | objects outbox DB 测试使用本地 PG16 `ONTOAXIS_TEST_DATABASE_URL`；search schema 迁移测试需真实 PG，未在本次 CI 证据中执行 |

## 命令与结果

```bash
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...
# exit 0

GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./...
# 所有包 ok；无 FAIL

GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -race ./internal/platform/search/... ./cmd/ontoaxis-search/
# ok

ONTOAXIS_TEST_DATABASE_URL='postgres://ontoaxis:ontoaxis@localhost:15433/ontoaxis?sslmode=disable' \
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./internal/modules/objects/... -count=1
# objects/repo 三个 outbox 事务测试通过；其余 ok

GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go vet ./...
# exit 0
/tmp/go/bin/gofmt -l internal/platform/search cmd/ontoaxis-search internal/modules/objects
# 无输出
```

## 覆盖

| 能力 | 覆盖 |
| --- | --- |
| 索引名/映射 | 单元测试：租户哈希稳定、索引名不含原始 UUID、非法版本拒绝、中文/嵌套/超长/敏感键/确定性键序 |
| Meilisearch 客户端 | httptest：创建索引、设置、批量 upsert/delete、搜索、swap、任务等待、health、错误分类、超时、无重试 |
| 配置 | 单元测试：默认值、enabled 必填 URL/key、非法 URL/超时/版本/批次/重试、disabled 不要求凭据 |
| outbox 生产 | `objects/repo/outbox.go` 单事务写 object + jobs.outbox；DB 测试覆盖 create/update/delete 事件与 outbox 插入失败回滚 |
| 消费者 | 单元测试：已知/未知事件、read-through、重复收敛、失败退避、死信、重放 |
| 重建 | 单元测试：命名/暂存/切换/失败清理/verify 计数；无真实 Meili |
| 搜索 API | 单元测试：参数矩阵、401 仅 X-Tenant-Id、403 无 items/total、503、成功字段、类型多值全部授权、disabled 404 |
| worker/cmd | 单测：health/ready/metrics 端点、命令参数、迁移幂等（单元层） |

## 未覆盖 / 限制

- 未启动真实 Meilisearch 与真实 PostgreSQL 的集成测试；无 create/update/delete→search 命中、重建/租约/swap 的端到端证据。
- `Rebuilder` 带 `objectTypeID` 时会把部分索引 swap 成租户整索引；`Verify` 只对数量，摘要比对未实现；旧暂存索引清理未实现。
- `EnsureIndex` 先异步创建再 PUT settings，真实 Meilisearch 首次创建可能竞态。
- 授权接缝按 `objects/api` 的 `authz.DecisionGuard` 实现；app 装配未完成，因此真实搜索路由未启用。
- `search` compose profile、Helm/ArgoCD、E2E 脚本与 `docs/stages/evidence/stage-2-object-search.md` 未完成（归 production-delivery / stage-2-integration）。
- `go.mod`/`go.sum` 无 search 依赖；工作区中这两文件的其他改动来自前序 change。
- 中文检索评测集与阈值未建立/未签核。

## 文件索引

- `internal/platform/search/{errors,naming,mapping,client,config,migrations,consumer,rebuild}.go`
- `internal/platform/search/migrations/00001_search_schema.up.sql`
- `internal/platform/search/worker/{worker,server,metrics}.go`
- `cmd/ontoaxis-search/main.go`
- `internal/modules/objects/repo/outbox.go`
- `internal/modules/objects/api/search.go`
- `internal/modules/objects/api/handler.go`、`internal/modules/objects/module.go`（`SetSearch`/`SearchStartupError`）
