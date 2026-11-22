# Authorization Integration and Negative-Matrix Evidence

> 状态：unit + 真实 OpenFGA 集成证据，2026-11-23。环境为本地 macOS x86_64 + Docker 27.0.3；不是生产验收。
> Change: `authorization-openfga-cedar`；对应 tasks 6.2、6.3、6.5。

## 环境与固定标识

| 项 | 值 |
| --- | --- |
| OpenFGA 镜像 | `openfga/openfga:v1.23.0`，manifest `sha256:2a1b4ef95f2f27e6d420f11cf14390b4f537e699de8d18b6a07d8b355e11cf02` |
| OpenFGA Go SDK | `github.com/openfga/go-sdk v0.8.4` |
| Cedar | `github.com/cedar-policy/cedar-go v1.9.3` |
| testcontainers-go | `v0.39.0` |
| Go | 1.26.3（`/tmp/go/bin/go`，`GOMODCACHE=/tmp/gomod`） |
| 模型摘要 | `sha256:1e3e94edf683a76fa21cceb312bb90d6044123ea644bb1c8ce30ad097566ec8f` |
| 策略 bundle 摘要 | `sha256:cfeb43a17994ec601dda5db747f47a70b81e0df41692060bf1cd3dd12a414b6b` |

## 12 项真实 handler / 真实引擎覆盖（task 6.2）

测试：`internal/modules/datasets/api/handler_integration_test.go::TestDatasetHandlerWithRealOpenFGA`（真实 OpenFGA testcontainer + 真实 cedar-go + 真实 datasets HTTP handler + 真实 `TenantContext`/`Authorizer`）。

| # | 边界 §11.2 项 | 覆盖方式 | 结果 |
| --- | --- | --- | --- |
| 1 | 租户成员读/写 allow；viewer 读 allow、写 deny | datasets GET viewer 200；PATCH viewer 403 | PASS |
| 2 | 跨租户 404/403 且不返回内容 | 另一 tenant GET → 404，响应不含 dataset 名 | PASS |
| 3 | 非 owner 删除 SavedQuery deny；owner allow | 真实 Authorizer：member owner allow；viewer owner deny；member 非 owner deny | PASS |
| 4 | Object → ObjectType → Tenant 关系链 | 真实 Authorizer：object member read allow、viewer write deny | PASS |
| 5 | Pipeline archived/非 published deny；published allow | published allow；archived/draft deny | PASS |
| 6 | ActionType disabled deny；active allow | active allow；disabled deny | PASS |
| 7 | Connector paused deny；active allow | active allow；paused deny | PASS |
| 8 | IngestJob/Execution 非法状态取消/重试 deny | queued retry deny；succeeded cancel deny | PASS |
| 9 | workspace-admin 组织级管理 deny；tenant-admin allow | workspace-admin org update deny；tenant-admin allow | PASS |
| 10 | 未知动作 deny；空策略集启动失败 | unknown action deny；空策略集/缺 role-matrix 启动失败（negative matrix） | PASS |
| 11 | OpenFGA 超时/Cedar 错误/审计失败 503 | 三项均在 negative matrix 断言 503 | PASS |
| 12 | decision id 与审计 decision_id 一致 | datasets GET 响应头 `X-Ontoaxis-Decision-Id` 与 `authz.decision` 审计行一致 | PASS |

原始输出：`evidence/2026-11-23-authz-handler-integration.txt`、`evidence/2026-11-23-authz-real-openfga.txt`。

## 24+ 条负向矩阵（task 6.3）

测试：`internal/platform/authz/negative_matrix_test.go::TestNegativeMatrix24`。覆盖边界 §11.3 全部行，并额外覆盖 Cedar 引擎错误：

无 JWT 401；JWT 无 tenant 403；租户头不一致 GET/PATCH/DELETE 403；viewer POST/PATCH/DELETE 403；member 组织管理/成员管理 403；workspace-admin 组织创建/更新 403；tenant-admin 审计 200；workspace-admin 审计 403；非 owner / viewer owner 删除 SavedQuery 403；owner member 删除 200；archived Pipeline 触发/更新 403；draft Pipeline 触发 403；disabled ActionType 运行 403；paused Connector 同步 403；queued Execution 重试 403；succeeded Execution 取消 403；未知角色/组主体 403；未登记动作 403；OpenFGA 超时 503；Cedar 错误 503；审计失败 503；空策略集启动失败；store 有元组启动失败。

原始输出：`evidence/2026-11-23-authz-negative-matrix.txt`。

## 审计样例（脱敏）

`authz.decision` 事件包含 `schema_version=1`、`event_id`、`event_type`、`occurred_at`、`tenant_id`、`request_id`、`trace_id`、`actor`、`action`、`resource`、`decision`、`reason_code`、`decision_id`、`policy.{fga_model_id,fga_relation,fga_decision,cedar_policy_ids,cedar_decision,bundle_digest}`、`http`、`latency_ms`。证据中不包含 token、cookie、SQL、请求体、原始 IP、email/name。

## CI

`.github/workflows/ci.yml` 新增 `authz-integration` job，运行：

```bash
go test -tags=integration -run 'TestAuthorizerWithRealOpenFGA|TestDatasetHandlerWithRealOpenFGA' -v \
  ./internal/platform/authz/ ./internal/modules/datasets/api/
```

## 限制

- 本地 Docker/OpenFGA 内存 store；未使用生产 OpenFGA、生产 PostgreSQL 或真实租户数据。
- Cedar 错误用例通过非法 resource type 注入；未做故障注入中间件。
- 24 条矩阵中的 handler 级 404 语义仅 datasets 实测；其他模块为 unit/接缝证据。
- `docs/stages/evidence/stage-2-authorization/` 的最终归档由 `stage-2-integration` 完成。
