# Proposal: Frontend Core Workbench

## Why

Stage 1 已批准，后端各业务模块正在并行建设，但仓库尚无 `web/` 应用，无法在浏览器中验证登录、项目、Ontology、对象、数据集与查询这六条核心路径，也无法为后续真实后端接入提供稳定的页面契约。若等到集成阶段再建前端，页面结构、认证模式、API 适配与错误处理会与后端契约同时返工，因此现在先交付一个 resource-first 的薄页面工作台，并以 mock/typed API 适配层冻结页面边界。

## What Changes

- 在 `web/` 建立 React 19 + Refine v5 + TypeScript + Vite 的最小前端工程，包含 `package.json`、Vite/Vitest/Playwright/TypeScript 配置、`index.html` 与锁文件。
- 交付六个核心页面：登录页、Projects、Ontology Manager、Object Explorer、Datasets、Queries；标准 CRUD 通过 Refine resource 配置与 hooks 驱动，复杂工作台使用自定义 React 页面。
- 登录支持 mock 与 Keycloak/OIDC（PKCE、state/nonce 校验）两种适配模式；未认证访问受保护路由时 fail-closed 跳转登录页，会话可持久化与刷新。
- 建立可配置运行模式：`VITE_API_MODE=mock|http`、`VITE_AUTH_MODE=mock|oidc`、`VITE_API_BASE_URL`（默认 `/api/v1`）；开发环境默认 mock，生产构建默认真实 API + OIDC 且缺少 OIDC 配置时拒绝启动。
- 建立 typed API 客户端、统一错误映射、列表/实体响应归一化与 Refine DataProvider；mock adapter 使用合成夹具，真实 HTTP adapter 复用同一页面与资源路径。
- 提供中英文界面与语言切换、统一导航与布局、加载/空态/错误/重试状态。
- 增加 Vitest 单元/适配器测试与 Playwright mock 模式页面级 E2E，覆盖成功、无效输入与关键拒绝路径；`check`、`build`、`test`、`test:e2e` 均可本地执行。
- 补齐本 change 的 proposal、delta spec、design、tasks 与 traceability 文档。

**Out of scope**

- 不实现或修改任何后端模块、Go module、proto 契约、数据库迁移、部署清单与共享根文件。
- 不接入真实 Keycloak、Trino、Kestra、Temporal、PostgreSQL 或真实客户数据；本 change 只提供可替换适配层与合成夹具。
- 不实现 shadcn/ui、TanStack Table、ECharts、Cytoscape、Monaco 等后续增强组件，也不实现完整 golden E2E、生产发布与真实环境验收。
- 不生成或替换正式 OpenAPI/Connect 客户端；`web/src/generated` 的 proto 生成物由平台生成链维护，本 change 仅保证页面通过 typed client 边界消费领域类型，后续由集成 change 切换到生成客户端。

## Capabilities

### New Capabilities

- `web-core-workbench`: 定义前端核心工作台的可观察行为，覆盖登录与受保护导航、项目资源、Ontology 模型、对象浏览、数据集预览、查询工作台、API 运行时适配与错误处理、国际化导航。

### Modified Capabilities

- 无。本 change 不改变任何已归档 capability 的行为契约。

## Impact

- 新增 `web/**` 前端源码、配置、测试与 `web/pnpm-lock.yaml`；不修改 `go.mod`/`go.sum`、`cmd/`、`internal/app/`、`internal/platform/`、`deploy/`、`proto/` 或其他模块目录。
- 前端 CI job 将在 `web/package.json` 存在后启用，执行 `pnpm install --frozen-lockfile`、`pnpm check`、`pnpm build`、`pnpm test`。
- 后续 `stage-1-core-integration` 需要把 mock DataProvider 切换为真实 `/api/v1` 适配器，并以生成客户端替换手写领域类型；届时页面结构与路由不需要重写。
- 本 change 不产生数据库、权限或数据语义变更，无破坏性 API 影响；合成夹具仅用于本地与 CI 测试，不代表真实数据或生产验收。
