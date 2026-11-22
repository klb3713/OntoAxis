# 2026-11-22 authorization-openfga-cedar 验证记录

> 环境：macOS x86_64、Docker 27.0.3、Go 1.26.3（`/tmp/go/bin/go`）、GOMODCACHE `/tmp/gomod`、`GOFLAGS=-buildvcs=false`。
> OpenFGA 镜像 `openfga/openfga:v1.23.0`（manifest `sha256:2a1b4ef95f2f27e6d420f11cf14390b4f537e699de8d18b6a07d8b355e11cf02`）；Go SDK v0.8.4、language v0.3.2、cedar-go v1.9.3、testcontainers-go v0.39.0。
> 说明：本记录为本地/预发合成夹具验证，不是生产验收。

## 构建与单测

命令：

```bash
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./...
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -race ./internal/platform/authz/
```

结果（2026-11-22）：`go build ./...` exit 0；`go test ./...` 所有包 `ok`；`go test -race ./internal/platform/authz/` `ok`。

覆盖要点：

- `internal/platform/authz/catalog_test.go`：88 个叶子动作，组计数 read 27/write 21/delete 11/invoke 10/admin 17/audit 2；未登记动作 deny。
- `authorizer_test.go`：允许/拒绝、跨租户短路、未知动作、OpenFGA 超时、审计写失败、资源状态、记忆化、审计信封、元组链与上限。
- `role_matrix_test.go`：5 角色 × 6 组 30 用例。
- `resource_state_test.go`：9 个状态用例。
- `explain_test.go`：tenant-admin allow、viewer 403。
- `observability_test.go`：span 属性白名单、Ready fail-closed、source commit 必填。
- `startup_policy_test.go`：启动发出 `authz.policy_change`，写失败则启动失败。
- 模块接缝测试：datasets、tenancy、ontology、objects、connectors、ingestion、pipelines、actions、query 的 allow/deny/跨租户 404/状态拒绝用例。

## 真实 OpenFGA 集成测试

命令：

```bash
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -tags=integration -run TestAuthorizerWithRealOpenFGA -v ./internal/platform/authz/
```

结果（2026-11-22）：PASS，9.24s。使用 testcontainers 启动固定版本 OpenFGA，创建 store、上传 `policies/openfga/model.fga` 生成的模型，真实 `authz.New` + `Authorize` 验证 viewer dataset read allow、viewer update deny、跨租户 deny；span 记录 `authz.cedar_eval`、`authz.fga_check`、`authz.check`，decision id 在 decision、`authz.decision` 审计与 span 三处一致。

## 当前未完成项

- 负向矩阵 24 条未形成独立原始输出文件；部分覆盖已分散在单测中。
- 边界 §11.3 12 项集成用例尚未全部落到真实业务 handler。
- `internal/app`/`cmd` 接线与 readyz 注册归 `stage-2-integration`，本 change 提供 patch 文档。
- 证据归档位置 `docs/stages/evidence/stage-2-authorization/` 由 integration 统一落盘。
