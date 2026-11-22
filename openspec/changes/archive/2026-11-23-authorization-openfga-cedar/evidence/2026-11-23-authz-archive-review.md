# Authorization Change Archive Review Package

> 状态：待负责人/安全评审确认，2026-11-23。**未归档、未 commit。**
> Change: `authorization-openfga-cedar`

## 1. 完成度

- tasks：**42/43**，仅 7.5（提交归档评审）未勾选。
- `openspec validate authorization-openfga-cedar --strict`：通过。
- `openspec validate --all --strict`：20/20 通过。
- `go build ./...`、`go test ./...`、`go test -race ./internal/platform/authz/ ./internal/modules/datasets/api/`：通过。

## 2. 证据索引

| 证据 | 路径 |
| --- | --- |
| 单元/集成基础验证 | `evidence/2026-11-22-authz-verification.md` |
| 真实 datasets handler + 真实 OpenFGA | `evidence/2026-11-23-authz-handler-integration.txt` |
| 真实 OpenFGA Authorizer 集成 | `evidence/2026-11-23-authz-real-openfga.txt` |
| 24+ 条负向矩阵原始输出 | `evidence/2026-11-23-authz-negative-matrix.txt` |
| 12 项/矩阵/审计/CI 汇总 | `evidence/2026-11-23-authz-integration-matrix.md` |
| 契约 | `docs/architecture/authorization-boundary.md`（定稿，负责人+安全评审 2026-11-22） |
| 追溯 | `traceability.md` |

## 3. 12 项真实实例覆盖

真实 OpenFGA `v1.23.0` testcontainer、真实 `cedar-go v1.9.3`、真实 datasets HTTP handler、真实 `TenantContext`/`Authorizer`。覆盖租户成员读写、跨租户 404、SavedQuery owner、Object→ObjectType→Tenant、Pipeline/ActionType/Connector/Execution 状态、workspace-admin/tenant-admin、未知动作、依赖故障、decision id 与审计一致。

## 4. 24+ 负向矩阵

`TestNegativeMatrix24`：29 个子用例 PASS，覆盖边界 §11.3 全部行，额外覆盖 Cedar 错误 503。

## 5. 限制（必须由评审接受）

- 本地 Docker/OpenFGA 内存 store；未使用生产 OpenFGA、生产 PostgreSQL、真实租户数据。
- 24 条矩阵中 handler 级 404 仅 datasets 实测；其他模块为 unit/接缝证据。
- CI job `authz-integration` 已加入 `.github/workflows/ci.yml`；本地等价命令通过，远端 CI 尚未运行。
- 生产部署、OpenFGA 供给、审计集中收集由 `production-delivery`/`stage-2-integration` 负责。
- `docs/stages/evidence/stage-2-authorization/` 最终归档位置由 `stage-2-integration` 落盘。

## 6. 评审请求

请负责人与安全评审确认：

1. 上述 42/43 与证据是否满足归档门禁；
2. 第 5 节限制是否可接受；
3. 是否同意将 7.5 标记完成并进入 `openspec archive` 流程（归档动作本身仍需单独确认，不自动执行）。
