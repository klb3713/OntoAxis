# Proposal: Query Surface

## Why

Stage 1 需要把 Trino 单节点 SQL 能力暴露为稳定的查询 API，并提供对象查询的统一入口。当前 `object-graph` 已实现 JSONB 对象存储，`dataset-catalog` 已实现数据集元数据；查询模块负责保存查询、执行只读 SQL（真实 Trino HTTP）以及按类型查询对象，避免 UI 直接访问底层引擎。

## What Changes

- 新增 `query` 业务模块，实现 SavedQuery CRUD、只读 SQL Execute/Explain、对象查询。
- 新增 `query.saved_queries` 表。
- 提供 `/api/v1/query` REST API：`/execute`、`/explain`、`/saved-queries`、`/objects`。
- 集成 Trino HTTP 客户端（`/v1/statement`、nextUri 链、错误映射），Trino 未配置时执行 API fail-closed 返回 503。
- 以 fake executor/fake store 和 `httptest` 覆盖成功、非法 SQL、未配置引擎、保存查询校验与对象查询路径。

**Out of scope**

- 不自研 SQL 引擎、不实现 Trino 权限模型、不做复杂 BI/图表。
- 不实现写 SQL（Stage 1 只允许只读语句）。
- 不实现 Flight SQL；仅 Trino HTTP。
- 不接真实对象存储/Trino 的 Stage 1 单元测试（真实 Trino E2E 由 `stage-1-core-integration` 补证）。

## Capabilities

### New Capabilities

- `query`: 保存查询、执行/解释只读 SQL、对象查询的行为契约。

## Impact

- 新增 `internal/modules/query/`：domain/repo/service/api/trino/module 与迁移、测试。
- 新增 `/api/v1/query/*` 路由，由集成 change 注册到模块列表。
- 新增 Trino 配置 `ONTOAXIS_TRINO_URL`、`ONTOAXIS_TRINO_USER`、`ONTOAXIS_TRINO_TIMEOUT`。
- 新增 1 张 `query` schema 表；复用既有依赖，不新增第三方依赖。
