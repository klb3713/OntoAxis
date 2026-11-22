# edge-gateway 验证证据

日期：2026-11-22
环境：macOS + Docker Desktop，本地 Compose profile `edge`（Traefik v3.3），Go 1.26.3（Docker）

## 1. 验证结果

| 验证项 | 命令 | 结果 | 证据 |
| --- | --- | --- | --- |
| 路由生成 | `go run ./tools/edgegen ...` | PASS | `edgegen.log`（本次未单独保存，见生成物 digest） |
| 生成漂移 | `go run ./tools/edgegen ... -check` | EXIT=0 | `ontoaxis-edge-generate-check.log` |
| 路由契约单测 | `go test ./internal/platform/authn/... ./tools/edgegen/...` | PASS | `/tmp/ontoaxis-edge-test.log` 中 authn/edgegen 包 |
| Go 全量测试 | `go test ./...` | EXIT=0 | `ontoaxis-edge-test-final.log` |
| Go race | `CGO_ENABLED=1 go test -race ./...` | EXIT=0 | `ontoaxis-edge-race.log` |
| Lint | `golangci-lint run ./...` | 0 issues | `ontoaxis-edge-lint.log` |
| Edge smoke | `scripts/edge-smoke.sh` | 11/11 PASS | `ontoaxis-edge-smoke.log` |
| Edge smoke + fail-closed | `EDGE_TEST_FAIL_CLOSED=1 scripts/edge-smoke.sh` | 12/12 PASS | `ontoaxis-edge-smoke-failclosed.log` |
| Envoy 清单结构 | `deploy/edge/kind/verify.sh` | 6 个资源结构正确；无集群，服务端 dry-run 未执行 | 本文件 |
| Actionlint | `rhysd/actionlint` | EXIT=0 | `ontoaxis-edge-actionlint.log` |
| Shellcheck | `koalaman/shellcheck` | EXIT=0 | `ontoaxis-edge-shellcheck.log` |
| Proto breaking | `make proto-breaking` | EXIT=0 | `ontoaxis-edge-proto.log` |
| 生成检查 | `make generate-check` | EXIT=0 | `ontoaxis-edge-gen.log` |
| Compose 配置 | `docker compose --profile edge config` | COMPOSE_OK | 本文件 |

生成物 `route-table-sha256`：`df684c65978058c8cdae20b7cc5388b706fc329b7a6a9a196a09af6aa1fe07f9`。

## 2. Traefik smoke 覆盖

`scripts/edge-smoke.sh` 对 `http://127.0.0.1:18085` 执行：

- 公开 `GET /healthz`、`GET /readyz` → 200；`/metrics` → 404
- 未知路由 `/api/v1/unknown` → 404
- 受保护路由无 JWT / 错误 JWT → 401
- Keycloak（`admin` / `admin123`）token → 200
- 伪造 `x-ontoaxis-tenant-id` → 403
- 公开路径错误方法 `POST /healthz` → 405；业务路径错误方法 `PUT /api/v1/tenancy/organizations` → 405
- 200 并发请求 → 出现 429（rate limit 生效），`Retry-After: 1`
- 停止 `ontoaxis-core-edge` 后请求 → 500 fail-closed（不转发业务请求）；恢复后再次 200

## 3. 证据边界

- Envoy Gateway 只在本地生成/结构校验；没有 kind/k3d 集群，`kubectl apply --dry-run=server` 与真实 Envoy 路由未执行。`deploy/edge/kind/install-envoy-gateway.sh` 与 `verify.sh` 提供集群内校验入口。
- Traefik 的 fail-closed 状态是 500（provider 行为）；spec 已改为 5xx，不再绑定精确 503。
- 本地 direct core 仍发布 18080 供 `oa-web` 使用；`edge` profile 的 core 副本 `ontoaxis-core-edge` 不发布宿主端口，只接入 edge 网络。
- 跨副本精确租户限流不在本 change 范围；当前是单实例本地限流。
