# Stage 1 集成证据

日期：2026-11-22
变更：`stage-1-core-integration`

## 1. 构建与静态检查

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 构建 | `go build ./...` | EXIT=0 |
| 单元测试 | `go test ./...` | 全部通过 |
| Race | `go test -race ./...` | 全部通过 |
| Vet | `go vet ./...` | 无问题 |
| Lint | `golangci-lint run ./...` | 0 issues |
| Web 测试 | `pnpm test -- --run` | 26 passed |
| Web 构建 | `pnpm build` | 成功 |
| Proto lint | `buf lint` | 通过 |
| Proto 生成漂移 | `make generate-check` | 通过 |
| Proto breaking | `make proto-breaking` | 通过 |
| OpenSpec | `openspec validate --all --strict` | 12 passed / 0 failed |
| 工作流静态检查 | `actionlint` | 无问题 |
| ShellCheck | `shellcheck scripts/smoke-foundation.sh` | 无问题 |
| 许可证 | `go run github.com/google/go-licenses@v1.6.0 check ./...` | EXIT=0 |

## 2. 运行时循环

Compose 服务：`postgres`、`keycloak`、`trino`、`ontoaxis-core`、`web`。

验证记录：

- `GET /healthz` → 200。
- `GET /readyz` → 10 项检查（9 个业务模块 + database）全部 `ok`。
- 真实 PostgreSQL 创建 11 个 schema：`action`、`connector`、`core`、`dataset`、`ingestion`、`jobs`、`object_data`、`ontology`、`pipeline`、`query`、`public`。
- Keycloak realm `ontoaxis` 可登录；OIDC password grant 返回 token，claims 包含 `aud=ontoaxis-api`、`tenant_id=11111111-1111-4111-8111-111111111111`、`roles`。
- 无 token 访问受保护路由 → 401；伪造 `x-ontoaxis-*` 头 → 403；携带合法 token → 200。
- Trino：`SELECT 42 AS answer` 返回 `42`；`SELECT COUNT(*) FROM tpch.tiny.nation` 返回 `25`。

## 3. 浏览器 E2E

使用 `cd web && PLAYWRIGHT_EXECUTABLE_PATH="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" pnpm test:e2e:real` 完成 OIDC 登录与核心页面验收：

1. `http://127.0.0.1:18082/` → Keycloak 登录 → `/projects`。
2. Projects 显示 `Trail Demo`。
3. Ontology 显示 `trail` object type 与 4 个 properties。
4. Object Explorer 显示 Trail 对象列表（6 条，含重复验收数据）。
5. Datasets 页面正常加载。
6. Queries 执行 `SELECT 42 AS answer`，结果表显示 `42`。

截图：

- [`projects.png`](stage-1-core-integration/projects.png)
- [`ontology.png`](stage-1-core-integration/ontology.png)
- [`object-explorer.png`](stage-1-core-integration/object-explorer.png)
- [`datasets.png`](stage-1-core-integration/datasets.png)
- [`queries.png`](stage-1-core-integration/queries.png)

全部步骤 `ok: true`，无浏览器 console error。

## 4. 镜像与回滚

- 两次相同源码构建得到相同 image id：`sha256:ebf55002ffb562e759df61f8e05f70749e36015d55a2d9469d5c934761d7d834`。
- 回滚流程见 [`deploy/README.md`](../../../deploy/README.md)。

## 5. 已知非阻塞项

- 前端 E2E 中 Trail 对象有重复验收数据（6 条而非 3 条），来自重复执行同一脚本用于截图；不影响页面。
- 当前 OIDC 使用本地 Keycloak realm；生产 IdP 的 JWKS/claims 配置需在部署环境复核。
- Stage 1 exit 需负责人确认后才能归档 change 与宣告完成。
