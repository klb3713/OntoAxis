# Tasks: edge-gateway

## 1. 路由表契约

- [x] 在 `config/edge/routes.yaml` 增加 `spec.rateLimit` 默认值、`auth.requiredClaims` 与 provider 生成目标，保持 schema 可解析。
- [x] 维护 `config/edge/routes.schema.json`：覆盖 metadata/gateway/upstream/auth/routes/rateLimit/generation，`additionalProperties: false`。
- [x] 生成器校验重复 name/prefix、未知 auth、非法方法、非法 upstream、缺失 public allowlist。
- [x] 更新 `config/edge/README.md`：匹配顺序、生成规则、限流语义、provider 差异。

## 2. 路由生成器

- [x] 新增 `tools/edgegen` 命令：读取 YAML、校验 schema 语义、输出确定性产物。
- [x] 生成 `deploy/edge/generated/envoy-httproute.yaml`（Gateway/HTTPRoute/SecurityPolicy/BackendTrafficPolicy/HTTPRouteFilter）。
- [x] 生成 `deploy/edge/generated/traefik-dynamic.yaml`（routers/services/middlewares）与静态配置。
- [x] 生成物写入 `ontoaxis.dev/route-table-sha256` digest。
- [x] 2.5 新增 `make edge-generate` 与 `make edge-generate-check`；CI 在干净工作区校验生成物（CI run 45025372931 的 build-test 已通过）。

## 3. Envoy Gateway

- [x] 3.1 部署清单被 `kubectl apply --dry-run=server` 与 kind 实际 apply 接受；kind v1.37.0 + Envoy Gateway v1.2.6 验证 Gateway Programmed=True。
- [x] public / internal / protected HTTPRoute 按声明生成，无 catch-all。
- [x] SecurityPolicy extAuth 指向 core `/internal/edge-auth`，`headersToBackend` 映射到 `x-ontoaxis-*`；不启用原生 JWT claimToHeaders。
- [x] 3.4 客户端身份 header 不会被信任，未知路由 404，错误方法 405；Traefik 返回 403，Envoy 在 extAuth 前剥离并按 JWT 身份继续（200）。
- [x] 3.5 限流返回 429；Traefik 附带 `Retry-After`，Envoy 本地限流仅返回 429（provider 差异已记录）。

## 4. Traefik

- [x] core 新增内部 `/internal/edge-auth` 端点：复用 `authn` 校验 JWT，成功回写 `x-ontoaxis-*`，失败返回 401/403；该路由不暴露到 edge 公网路由表。
- [x] Traefik 静态/动态配置使用 `forwardAuth` 指向 core auth 端点，`authResponseHeaders` 注入身份头，`headers` 清理客户端身份头，`ratelimit` 做本地限流。
- [x] 无 JWT 401、伪造 header 403、未知 404、错误方法 405、超限 429。
- [x] core auth 端点不可达时 Traefik 必须 fail-closed 返回 5xx（当前 500），不转发业务请求；集成测试覆盖。

## 5. 部署

- [x] `deploy/compose/compose.yaml` 新增 `oa-edge`（Traefik），端口 18085，core 不暴露公网直连。
- [x] `deploy/edge/kind/` 提供 Envoy Gateway 安装/验证脚本，不打包 controller。
- [x] 5.3 两种 provider 均独立启动并暴露 health/readiness；Traefik Compose 与 Envoy kind 均已验证。

## 6. 测试与证据

- [x] `scripts/edge-smoke.sh` 对 Envoy/Traefik 跑同一组正向/负向用例。
- [x] Go 单元测试覆盖生成器、schema、路由排序、限流配置。
- [x] 6.3 集成测试覆盖两种 provider 的 404/405/401/403/429 与公开 allowlist；Traefik 覆盖 403，Envoy 覆盖剥离语义（伪造 header 返回 200 但不被信任）；新增 `scripts/edge-smoke-envoy.sh` 并在 kind 通过 10/10。
- [x] `docs/stages/evidence/stage-2-edge-gateway.md` 记录命令、环境、结果与截图/日志。

## 7. 文档与治理

- [x] 更新 `ARCHITECTURE.md` 15.3：oa-edge 路由契约状态从“已定稿”推进到“已实现”。
- [x] 更新 `docs/architecture/runtime-topology.md` 与 `docs/architecture/services-and-ports.md` 的 edge 入口。
- [x] Stage 2 change map 标记 `edge-gateway` 进度与证据链接。
- [x] 更新 `README.md` / `docs/getting-started/local-development.md` 的 oa-edge 启动说明。

## 8. 评审

- [x] `go build ./...`、`go test ./...`、`go test -race ./...` 通过。
- [x] `make edge-generate-check`、`make proto-breaking`、`make generate-check` 通过。
- [x] `golangci-lint`、`actionlint`、`shellcheck scripts/edge-smoke.sh` 通过。
- [ ] 8.4 GitHub Actions 全绿；证据与 traceability 更新（CI edge job 已添加，待推送运行）。
- [ ] 8.5 负责人评审通过后方可归档 change（待评审）。
