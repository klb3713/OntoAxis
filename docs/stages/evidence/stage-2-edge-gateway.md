# edge-gateway 验证证据

日期：2026-11-22
归档：`openspec/changes/archive/2026-11-22-edge-gateway/`；主 spec：`openspec/specs/edge-gateway/spec.md`
环境：macOS + Docker Desktop；本地 Compose profile `edge`（Traefik v3.3）；kind v1.37.0 + Envoy Gateway v1.2.6；Go 1.26.3（Docker）

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
| Envoy kind 安装 | `deploy/edge/kind/install-envoy-gateway.sh` | GatewayClass Accepted=True，Gateway Programmed=True | `ontoaxis-envoy-gateway-install.log` |
| Envoy 清单应用 | `deploy/edge/kind/verify.sh` | `kubectl apply --dry-run=server` + 实际 apply 通过 | `ontoaxis-envoy-kind-verify.log` |
| Envoy smoke | `scripts/edge-smoke-envoy.sh` | 10/10 PASS | `ontoaxis-edge-smoke-envoy.log` |
| Actionlint | `rhysd/actionlint` | EXIT=0 | `ontoaxis-edge-actionlint.log` |
| Shellcheck | `koalaman/shellcheck` | EXIT=0 | `ontoaxis-edge-shellcheck.log` |
| GitHub Actions | CI run 45026353151 | 6/6 jobs success | https://github.com/klb3713/OntoAxis/actions/runs/45026353151 |
| Proto breaking | `make proto-breaking` | EXIT=0 | `ontoaxis-edge-proto.log` |
| 生成检查 | `make generate-check` | EXIT=0 | `ontoaxis-edge-gen.log` |
| Compose 配置 | `docker compose --profile edge config` | COMPOSE_OK | 本文件 |

生成物 `route-table-sha256`：`df684c65978058c8cdae20b7cc5388b706fc329b7a6a9a196a09af6aa1fe07f9`（以当前 `config/edge/routes.yaml` 为准，见生成物 annotation）。

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

## 3. Envoy kind 运行验证

- 集群：kind v0.33.0 / Kubernetes v1.37.0；Envoy Gateway v1.2.6，GatewayClass `eg`，EnvoyProxy `kind-proxy`（Service type NodePort）。
- 安装时发现默认 `kubectl apply` 对超大 CRD 会触发 `metadata.annotations: Too long`；已改用 `kubectl apply --server-side --force-conflicts`，并补充 GatewayClass + EnvoyProxy 资源。
- 应用时发现 Gateway API `HTTPRoute` 最大 16 条 rules；生成器原先 9 条 method rule + 9 条 fallback = 18，导致 server-side dry-run 失败。已合并 fallback matches 为单条 rule（10 条），并补 `BackendTrafficPolicy.rateLimit.type: Local`。
- runtime：`/healthz` 200、`/readyz` 200、`/metrics` 404、未知路由 404、无/坏 JWT 401、有效 JWT 200、错误方法 405、限流 429、auth 后端不可达返回 403 fail-closed。
- 伪造身份头：Envoy 在 extAuth 前剥离客户端身份头，携带 forged header 的请求按 JWT 身份继续（200）；直接调用 `/internal/edge-auth` 携带同一 header 返回 403，证明伪造身份未被信任。该 provider 差异已写入 spec/README。

## 4. 证据边界

- Traefik 的 fail-closed 状态是 500，Envoy extAuth 失败为 403（provider 行为）；spec 只要求非 2xx fail-closed。
- 本地 direct core 仍发布 18080 供 `oa-web` 使用；`edge` profile 的 core 副本 `ontoaxis-core-edge` 不发布宿主端口，只接入 edge 网络。
- 本地 direct core 仍发布 18080 供 `oa-web` 使用；`edge` profile 的 core 副本 `ontoaxis-core-edge` 不发布宿主端口，只接入 edge 网络。
- 跨副本精确租户限流不在本 change 范围；当前是单实例本地限流。
