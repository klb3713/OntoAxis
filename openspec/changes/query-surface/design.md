# Design: Query Surface

## Decisions

### D1. 模块边界

`internal/modules/query/` 拥有 SavedQuery、SQL 执行与对象查询。`query` 依赖 `datasets` 与 `objects`；对象查询通过注入 `objectsservice.Service` 只读调用，不直接读表。

### D2. Trino 客户端

`trino.Client` 只实现 `/v1/statement` POST、nextUri GET 与错误解包；不依赖 Trino SDK。`ONTOAXIS_TRINO_URL` 为空时 `Executor` 为 nil，执行/解释返回 `ErrUnavailable`。超时由 `ONTOAXIS_TRINO_TIMEOUT` 控制。

### D3. 只读 SQL 校验

Stage 1 只允许 `SELECT/WITH/SHOW/DESCRIBE/EXPLAIN/VALUES` 开头语句；通过简单首词检查拒绝 DML/DDL。更深层 SQL 权限由 Trino 用户/角色负责。

### D4. 保存查询

`query.saved_queries` 有 `(tenant_id, name)` 部分唯一索引（`deleted_at IS NULL`），软删除；删除后名称不可复用（与平台其他模块一致）。所有 SQL 显式带 `tenant_id`。

### D5. 错误与租户

API 使用 `httperr` 统一信封；租户优先 `authn.RequireTenant`，测试/本地回退 `X-Tenant-Id`。保存查询要求已认证 user ID。错误映射：invalid→400、not_found→404、conflict→409、unavailable→503。

## Risks

- 只读 SQL 检查是首词级，不做完整 AST；写语句可能通过 `WITH ... INSERT` 等复杂语句绕过，Stage 1 由 Trino 只读用户兜底。
- 未做真实 Trino/Postgres 集成验证；由 `stage-1-core-integration` 补证。
- 对象查询直接依赖 objects Service；未注入时 fail-closed。
