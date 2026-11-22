# Stage 2 组合根运行接线证据（2026-11-23）

> 范围：`stage-2-integration` 任务 4.1/4.2（代码级接线与组合测试）。未起 Compose/kind，不覆盖 4.3/4.4/5.x/6.x/7.x。

## 环境

- macOS x86_64；Go 1.26.3 `/tmp/go/bin/go`；`GOMODCACHE=/tmp/gomod`；未连接 OpenFGA/Meilisearch/Temporal 实例。

## 4.1 运行时 adapter 接线

- `internal/app/runtime.go` 新增 `RuntimeAssembly`/`BuildRuntimeAssembly`/`BuildRuntimeModules`：pipeline 配置 → connectors runtime → search 配置 → authz 配置 → `authz.New` → 9 业务模块 → actions `InitError` → `injectDecisionGuard` → `SetSyncRuntime` → search backend 校验 → authz 模块 → PostMigrate 构建 ingestion runtime。
- `internal/app/app.go` 的 `ModuleFactory` 改为 `AssemblyFactory`；`App` 持有 background/closers，`Run` 启动后台运行时，shutdown 先停 background（Wait），再关闭 actions/authorizer、telemetry、pool。
- `internal/app/adapters.go`：对象类型/数据集/连接器 reader 延迟绑定 `Service()`，并映射 not-found。
- `internal/app/modules.go`：保留 `BuildModules` 为 9 模块固定顺序、无环境依赖；`businessModules` 供装配使用。
- 数据集模块补 `SetDecisionGuard` 接缝；`injectDecisionGuard` 覆盖 datasets/tenancy/connectors/ingestion/ontology/objects/pipelines/query/actions。
- `cmd/ontoaxis-core/main.go` 调用 `app.BuildRuntimeAssembly`。

## 4.2 组合测试（fail-fast）

新增 `internal/app/runtime_test.go` 6 个用例：缺 authz 环境失败、非法 pipeline 环境失败、非法 connector 环境失败、无 authz 时 `BuildModules` 仍 9 模块、ObjectTypeFact 映射、DatasetFact 映射；既有 `TestBuildModulesResolvesDependencyOrder` 保持通过。

## 命令与结果

```text
gofmt -l internal/app cmd/ontoaxis-core                     # clean
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...   # exit 0
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -count=1 ./internal/app/...  # ok
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go vet ./internal/app/... ./cmd/ontoaxis-core/...  # exit 0
```

## 明确限制

- 本地无 OpenFGA 实例，`authz.New` 的启动检查未做真实决策验证；authz 集成测试由 `internal/platform/authz/fga_integration_test.go`（testcontainers）单独覆盖。
- ingestion runtime 使用 nil `SnapshotVerifier`：reconciler 不会自动判定成功，需后续接入真实 verifier（已在 `runtime.go` 注释）。
- connectors→ingestion 状态适配只映射 Source/Sink，`Ready`/`FailedMessage` 为空；不改变既有语义。
- 未执行 Compose/kind/真实 Meilisearch/Temporal/OpenFGA E2E；4.3/4.4 及 5.x/6.x/7.x 仍为未通过。

## 3.1 依赖收口

```text
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go mod tidy   # exit 0
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go mod verify # all modules verified
```

注意：工作区包含全部 change 的未提交实现，`git status --porcelain` 非空；本任务按“依赖已 tidy、验证通过”记录，提交差异由后续 review/commit 流程处理，不伪称工作区干净。

## 全仓门（本地）

```text
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go build ./...  # BUILD_OK
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test ./...   # all ok
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go vet ./...    # VET_OK
GOFLAGS=-buildvcs=false GOMODCACHE=/tmp/gomod /tmp/go/bin/go test -race <touched packages>  # ok
cd web && pnpm check && pnpm test -- --run  # 6 files, 33 tests passed
```

## 生成物刷新（2026-11-23）

- `make generate` → exit 0；`make generate-check` → exit 0（无差异）。
- `make edge-generate` → exit 0，生成 `deploy/edge/generated/envoy-httproute.yaml`、`deploy/edge/generated/traefik-dynamic.yaml`；`make edge-generate-check` → exit 0（route output up to date）。

## 配置键文档（2026-11-23）

- `.env.example` 新增 Stage 2 非密钥配置键（authz/search/actions/connector/pipeline）；无 token、密码或生产连接串。
- `docs/getting-started/local-development.md` 新增“Stage 2 运行时配置”说明。
- 任务 3.4 勾选。
