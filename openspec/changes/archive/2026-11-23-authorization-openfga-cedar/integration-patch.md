# Integration patch: app/cmd wiring for authz

> 本文件是 `authorization-openfga-cedar` 交给 `stage-2-integration` 的接线清单。
> 按 change map 所有权，本 change 不直接修改 `internal/app/`、`cmd/ontoaxis-core/`、`go.mod`/`go.sum`、`deploy/`。

## 1. 构造顺序（`internal/app/app.go`）

1. `observability.SetupTelemetry` 之后、`db.OpenWithTracer` 之前或之后均可：
   - `authzCfg, err := authz.LoadConfigFromEnv()`；失败直接返回错误（fail-closed）。
   - `authzCfg.SourceCommit = cfg.Commit`（显式注入，避免依赖 build info）。
   - `authzCfg.PolicyDir` 留空使用 embed；生产如需外部策略，由部署注入 `ONTOAXIS_AUTHZ_POLICY_DIR`。
2. 在构造业务模块前：
   - `authorizer, err := authz.New(ctx, authz.Options{Config: authzCfg, Tracer: telemetry.Tracer()})`。
   - `authzModule := authz.NewModule(authorizer)`。
3. 把 `authzModule` 加入模块列表（`factory(pool, authzModule.Guard())` 或先构造后传入）。
   - 建议把 `ModuleFactory` 改为 `func(*pgxpool.Pool, authz.DecisionGuard) []module.Module`，由业务模块用 `WithDecisionGuard(guard)` 注入。
4. `httpserver.Options` 不需要新增字段：模块列表中的 `authz.Module` 提供：
   - `Ready(ctx)` → `/readyz` 的 authz 维度（store/model/零元组）；
   - `RegisterRoutes` → `POST /api/v1/authorization/explain`。
5. 关闭顺序：`App.Run` 退出时调用 `authorizer.Close()`（在 `telemetry.Shutdown` 前）。

## 2. 配置

新增必填环境变量（生产/kind/Compose 由 `production-delivery` 注入）：

```text
ONTOAXIS_AUTHZ_OPENFGA_API_URL
ONTOAXIS_AUTHZ_OPENFGA_STORE_ID
ONTOAXIS_AUTHZ_OPENFGA_MODEL_ID
ONTOAXIS_AUTHZ_OPENFGA_API_TOKEN   # 生产必填，本地可空
ONTOAXIS_AUTHZ_OPENFGA_TIMEOUT     # 默认 200ms
ONTOAXIS_AUTHZ_DECISION_TIMEOUT    # 默认 300ms
ONTOAXIS_AUTHZ_AUDIT_SINK          # 仅 stdout
ONTOAXIS_AUTHZ_SOURCE_COMMIT       # 可选，默认 cfg.Commit
```

禁止出现 `AUTH_DISABLED`、`ONTOAXIS_AUTHZ_DISABLED`、`ONTOAXIS_AUTHZ_FAIL_OPEN`；`authz.LoadConfigFromEnv` 会直接报错。

## 3. 业务模块接缝

本 change 已在每个模块的 `api` 包加入 `WithDecisionGuard(authz.DecisionGuard)` 与 variadic 构造函数，并在模块根加入 `SetDecisionGuard(guard)`（tenancy 另有 `WithGuard(ModuleOption)`）。`nil` guard 仅保留给既有测试；生产必须注入 non-nil guard。

在 `internal/app/modules.go` 构造模块后、`RegisterRoutes` 之前逐个注入：

```go
datasets.New(pool, resolver, datasetsapi.WithDecisionGuard(guard)) // 既有参数
tenancy.New(pool, tenancy.WithGuard(guard))
ontology.New(); ontologyModule.SetDecisionGuard(guard)
objects.New(pool); objectsModule.SetDecisionGuard(guard); objectsModule.SetObjectTypeReader(objectTypeReader)
... ontologyModule.SetDatasetReader(datasetReader)
connectors.New(); connectorsModule.SetDecisionGuard(guard)
ingestion.New(); ingestionModule.SetDecisionGuard(guard); ingestionModule.SetConnectorReader(connectorReader)
pipelines.New(pool, ...); pipelinesModule.SetDecisionGuard(guard)
actions.New(pool); actionsModule.SetDecisionGuard(guard)
query.New(pool, ...); queryModule.SetDecisionGuard(guard)
```

需要 app 提供的两个 adapter（均为 tenant-scoped）：
- `objects.api.ObjectTypeReader`: 调 `ontology.Service().GetObjectType(ctx, tenantID, objectTypeID)`；`ontology/domain.ErrNotFound` → `objects/domain.ErrNotFound`；返回 `objectsapi.ObjectTypeFact{ID, TenantID}`。
- `ingestion.api.ConnectorReader`: 调 `connectors.Service().GetConnector(ctx, tenantID, id)`；`connectors/domain.ErrNotFound` → `ingestion/domain.ErrNotFound`。
- `ontology.api.DatasetReader`: 调 `datasets.Service().GetDataset(ctx, tenantID, datasetID)`；`datasets/domain.ErrNotFound` → `ontologyapi.ErrDatasetNotFound`；返回 `ontologyapi.DatasetFact{ID, TenantID}`。用于在 `BindDataset` 前拒绝跨租户 dataset_id。
- `objects.api.LinkReader` 可选：v1 未配置时 `DELETE /links/{id}` 以 `Tenant` 为授权资源（与 Stage 1 等效，删除仍按租户过滤并 404）；配置后按源 `Object` 判定。集成阶段优先不新增 objects service/repo 读取方法。

所有模块的 `SetDecisionGuard` 必须在 `RegisterRoutes` 之前调用；pipelines 的 API 在 `New` 中构造，`SetDecisionGuard` 会同步更新已构造的 API。`authz.Module` 必须在业务模块之前构造，以保证 readyz 与 Explain 路由可用；业务模块可依赖 `"authorization"` 模块名（如需拓扑顺序）。

## 4. 测试影响

- `internal/app` 现有测试若直接 `New(...)`，需要注入 fake FGA/Authorizer，或改为通过 `app.Options` 接收 `*authz.Authorizer`；不要用 `AUTHZ_DISABLED` 绕过。
- 生产/集成测试建议使用 testcontainers OpenFGA v1.23.0（与 `internal/platform/authz/fga_integration_test.go` 相同镜像）或以 kind 中的 OpenFGA 服务替代。
- `go.mod`：本 change 已加入 `cedar-go`、`openfga/go-sdk`、`openfga/language`、`testcontainers-go`；`go mod tidy` 由 integration 在接线完成后执行。

## 5. 验收

- `go build ./...`、`go test ./...`、`go test -race ./internal/platform/authz/`。
- 启动时缺 OpenFGA/策略 → 进程退出；运行期 OpenFGA 故障 → `/readyz` 失败，受保护请求 503，不返回业务数据。
- `POST /api/v1/authorization/explain` 仅 tenant-admin/platform-admin；响应头 `X-Ontoaxis-Decision-Id` 与 `authz.explain` 审计 `decision_id` 一致。
