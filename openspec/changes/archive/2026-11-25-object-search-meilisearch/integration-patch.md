# Integration patch: object-search-meilisearch

> 状态：2026-11-23；核心与 objects 接缝已实现，app/部署由对应 owner 合并。

## 1. core 装配（`internal/app/`，owner stage-2-integration）

1. 读取 `search.LoadConfigFromEnv()`；`objects.New(pool)` 已默认使用 `repo.NewOutbox(pool)`（outbox 生产始终开启，无需配置）。
2. 构建 `platform/search.Client`：`search.NewClient(cfg.URL, cfg.APIKey, search.WithTimeout(cfg.Timeout))`。
3. 实现 `objects/api.SearchBackend` 适配器（建议放 `internal/app/search_adapter.go`）：
   - `Search(ctx, api.SearchRequest) (api.SearchResponse,error)`：索引 UID 用 `search.IndexName(cfg.IndexVersion, req.TenantID.String())`；filter 至少 `search.TenantFilter(req.TenantID.String())`，有 `ObjectTypeIDs` 时追加 `object_type_id IN [...]`；`Sort` 用 client 的 `[]string` 语法（`updated_at:desc`）；limit/offset 透传；结果 `Document` 映射为 `api.SearchHit`；`EstimatedTotalHits>maxTotalHits` 时 `Truncated`。
   - `Healthy(ctx) error` 调 `client.Health(ctx)`。
4. 在 `RegisterRoutes` 前调用 `objectsModule.SetSearch(adapter, cfg.Enabled)`；若 `cfg.Enabled` 且 `objectsModule.SearchStartupError()!=nil`，启动失败/报告（禁止允许一切替身）。授权 guard 用 `objectsModule.SetDecisionGuard(authzGuard)`（与对象 CRUD 同一个）。
5. `ONTOAXIS_SEARCH_ENABLED=false` 时不得注册 `/api/v1/objects/search`（返回 404），objects 既有路由与行为不变。

## 2. 搜索 worker（`cmd/ontoaxis-search`，owner production-delivery）

- 构建 `cmd/ontoaxis-search`；启动前运行 `search.Migrate`（worker 自管 `search` schema）。
- `consume`：`ONTOAXIS_DATABASE_URL`、`ONTOAXIS_SEARCH_URL`、`ONTOAXIS_SEARCH_API_KEY`；health `:8081`（`ONTOAXIS_SEARCH_HTTP_ADDR`），metrics `:9094`（可选）。
- `rebuild --tenant <uuid> [--object-type <uuid>] [--verify]`：先对目标租户回填；`replay --limit` 重放死信。
- 搜索不可用不得使 core `/readyz` 失败；worker `/readyz` 反映 DB + Meilisearch。
- 需要 compose `search` profile（meilisearch:v1.12 固定版本 + worker）与 Helm/ArgoCD Deployment/Service/PVC/Secret/Probe；主 compose 与 Helm 由 production-delivery 合并，设计见 D11/部署片段。

## 3. 数据库与事件

- worker 迁移创建 `search.outbox_attempt`、`search.index_state`；不改 `jobs.outbox` 与 `object_data.objects` 表结构。
- `objects` 写路径新事务：object 写 + `jobs.outbox` 事件（`objects.object.created.v1|updated.v1|deleted.v1`）；失败回滚业务写。
- outbox 行清理策略未实现，保留供重放与证据。

## 4. 授权

- 搜索 API 使用 `object:read` 对 `object_type` 或 `tenant` fact 判定；`authorization-openfga-cedar` 已实现 `authz.DecisionGuard`，app 注入同一 guard。
- 拒绝不得以空结果掩盖；guard 缺失且 enabled → 503/启动失败，禁止 allow-all 替身。

## 5. 证据待办

- 真实 PostgreSQL + Meilisearch 集成、`scripts/object-search-e2e.sh`、CI job、`docs/stages/evidence/stage-2-object-search.md` 未完成。
- 中文检索评测阈值未签核。
- 真实 Meilisearch 首次 `EnsureIndex` 设置竞态需在集成测试中观察并修复（若出现）。
- 带 `objectTypeID` 的 rebuild 目前会以部分索引覆盖整租户索引；生产重建必须不传 `--object-type`，或后续修正。
