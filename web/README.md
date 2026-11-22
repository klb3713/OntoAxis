# OntoAxis Web Core Workbench

Stage 1 的最小前端工作台，技术栈为 React 19 + Refine v5 + TypeScript + Vite。当前以 mock 数据与 mock 登录为默认运行模式，页面、DataProvider、认证适配器与 typed API 客户端已按后续接入真实 `/api/v1` 的边界组织。

## 命令

```bash
cd web
pnpm install
pnpm dev          # Vite 开发服务器，默认 http://127.0.0.1:5173
pnpm check        # TypeScript 静态检查
pnpm build        # 类型检查 + 生产构建
pnpm test         # Vitest 单元/适配器测试
pnpm test:e2e     # Playwright mock 模式页面 E2E（首次需 pnpm exec playwright install chromium）
```

## 运行模式

复制 `.env.example` 为 `.env.local` 后按需覆盖：

| 变量 | 可选值 | 默认（开发） | 说明 |
| --- | --- | --- | --- |
| `VITE_API_MODE` | `mock` / `http` | `mock` | `http` 通过 `VITE_API_BASE_URL` 访问真实后端 |
| `VITE_API_BASE_URL` | URL 路径 | `/api/v1` | 同源 API 基础路径 |
| `VITE_AUTH_MODE` | `mock` / `oidc` | `mock` | `oidc` 需要配置以下 Keycloak 参数 |
| `VITE_OIDC_AUTHORITY` | URL | — | 例如 `https://keycloak.example.com/realms/ontoaxis` |
| `VITE_OIDC_CLIENT_ID` | string | — | Keycloak client id |
| `VITE_OIDC_REDIRECT_URI` | URL | `${origin}/auth/callback` | 回调地址 |
| `VITE_OIDC_SCOPES` | string | `openid profile email` | 授权 scope |

生产构建默认 `http` + `oidc`，缺少 `VITE_OIDC_AUTHORITY` 或 `VITE_OIDC_CLIENT_ID` 时应用会拒绝启动，不会回退 mock 或匿名。

## 边界与后续接入

- 页面只消费 Refine hooks 与 `web/src/api/types.ts` 的领域类型，不直接 `fetch`；响应键名与分页差异由 `web/src/api/normalize.ts` 吸收。
- `web/src/api/rest-data-provider.ts` 的资源路径是前端临时契约。proto 未包含 HTTP 注解，真实路径以 `stage-1-core-integration` 与后端对齐结果为准。
- `web/src/generated/ontoaxis/v1/**` 来自平台 proto 生成链；本 change 只声明 `@bufbuild/protobuf` 运行时依赖，后续集成 change 应以生成客户端替换手写边界类型。
- mock 夹具均为合成数据，仅用于本地与 CI 测试，不代表真实客户数据或生产验收。
