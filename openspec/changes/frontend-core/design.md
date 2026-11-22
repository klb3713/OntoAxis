# Design: Frontend Core Workbench

## Context

参见 `proposal.md` 的 Why。当前仓库没有 `web/` 应用，后端六条核心路径仍在并行建设；`proto/ontoaxis/v1` 已冻结领域消息，但未包含 `google.api.http` 注解，REST 资源路径尚未由服务端生成。Stage 1 要求先用轻量前端冻结页面、认证、分页与错误处理边界，再由集成 change 切换到真实 `/api/v1` 后端。

本 change 的约束：

- 只写 `web/**` 与 `openspec/changes/frontend-core/**`，不修改后端、proto、部署与共享根文件。
- 不得引入默认 Principal、`AUTH_DISABLED` 或匿名降级；认证必须 fail-closed。
- 页面必须 resource-first：标准列表/创建/更新走 Refine 资源与 hooks，复杂工作台用自定义 React。
- API 响应键名差异（`data`/`items`/资源名、`meta`/`pagination`）、错误体、分页参数必须在客户端边界统一，不能散落页面。
- 测试必须覆盖成功、无效输入与关键拒绝路径，并至少有一个页面级 E2E。

## Goals / Non-Goals

**Goals**

- 冻结六个核心页面及其导航、会话与错误状态的页面契约。
- 将数据访问收敛到 `DataProvider` / typed client / 归一化层，页面不直接依赖后端响应形状。
- 用 mock 适配器让页面、测试与 CI 在无后端、无 Keycloak、无真实数据时独立运行。
- 保留一条低改动成本的真实化路径：切换环境变量、替换 data provider 与生成客户端，页面不重写。

**Non-Goals**

- 不定义最终 REST/Connect 路由契约，也不生成 OpenAPI 文档。
- 不实现生产级 BFF、CSP、SSO 单点登出、权限 UI 或细粒度 RBAC。
- 不实现富表格、图表、图探索、Monaco 编辑器和设计系统组件库。
- 不进行真实数据、真实身份或真实查询引擎验收。

## Decisions

### 1. React 19 + Vite + Refine v5，标准 CRUD 与自定义页面分层

选择 Vite SPA + React 19 + Refine v5：Refine 的 resource/data provider/auth provider 抽象正好覆盖 resource-first 要求，Vite 提供最小构建面。登录、Projects、Ontology、Datasets、Queries 的列表与创建走 `useList`/`useCreate`/`useUpdate`/`useCustom`，对象详情、SQL 工作台、属性编辑等交互用自定义 React 组件。

替代方案：Next.js（引入 SSR/路由/部署复杂度，本阶段无 SEO 与服务端渲染需求）；纯 TanStack Router + Query（更轻，但需要自建 resource 抽象，违背 resource-first 目标）。

### 2. 运行模式显式且 fail-closed

`VITE_API_MODE=mock|http`、`VITE_AUTH_MODE=mock|oidc` 显式解析为运行配置；开发默认 mock/mock，生产默认 http/oidc。非法 mode 或 OIDC 缺少 `authority`/`client_id` 时在创建运行时前抛错并阻止应用启动，不静默回退 mock 或匿名。

替代方案：自动探测后端可用性后降级。该方案会在生产配置错误时把真实用户带入合成数据，违反 fail-closed，因此拒绝。

### 3. 数据访问四层：生成契约底稿 → 领域类型 → 归一化 → DataProvider

`web/src/generated/ontoaxis/v1/**`（平台生成链产出）只作为契约底稿，不在页面中出现。`web/src/api/types.ts` 定义前端领域类型（camelCase，时间字段扁平化），`normalize.ts` 吸收 `data`/`items`/资源名与 `meta`/`pagination` 差异，`rest-data-provider.ts`/`mock-data-provider.ts` 实现同一 `DataProvider` 接口，页面只消费 Refine hooks。

这样页面不感知响应键名；集成阶段只需替换 `RESOURCE_PATHS`、领域类型映射与生成客户端，页面无需改动。替代方案：页面直接 `fetch` 并各自解包。该方案会把契约差异散落到每个页面，架构 16.6 明确禁止。

REST 路径暂以集中映射表达（如 `/projects`、`/ontology/object-types`、`/datasets`、`/queries/saved`）。由于 proto 无 HTTP 注解，这些路径是前端适配层的临时契约，集成 change 必须与后端实际路由一次对齐；集中映射使调整只改一处。

### 4. 认证适配器接口 + PKCE，mock 与 OIDC 隔离

`AuthAdapter` 抽象 `login`/`startLogin`/`completeLogin`/`logout`/`getSession`/`getAccessToken`；mock 适配器仅用于本地与 E2E，OIDC 适配器使用授权码 + PKCE S256，校验 state、nonce、ID Token，并在过期前刷新。Refine `AuthProvider` 只做桥接，不承载协议细节。

替代方案：直接使用 Refine Keycloak 插件。它会引入未评估依赖并隐藏 token 刷新细节；自建适配器可以显式测试 state/nonce/PKCE 拒绝路径。

会话暂存于 `localStorage`，包含用户与短期 token。生产部署前需配合 CSP 与更严格的 token 存储策略（BFF 或内存态）复核，这是已知的加固项而非本 change 的验收项。

### 5. i18n 使用 i18next 静态资源 + 持久化 locale

中英文资源在构建期打包，默认语言来自运行配置（默认 `zh-CN`），切换写入 `localStorage`。不使用 URL 前缀路由，避免路由与权限逻辑复杂化；后续若需要分享特定语言链接，可在网关或路由层补充。

替代方案：仅中文硬编码。架构 16.6 要求至少 en/zh，且英文资源已镜像全量 key，测试可校验一致性。

### 6. 测试分层：Vitest 单元/适配器 + Playwright mock E2E

Vitest + jsdom 覆盖错误映射、响应归一化、运行配置拒绝、OIDC PKCE/state/JWT 解析、mock provider 的分页/搜索/冲突与查询拒绝路径。Playwright 在 mock 模式启动真实 Vite 页面，覆盖登录、六页导航、创建项目、数据集预览、SELECT 执行与非 SELECT 拒绝。

替代方案：只做组件快照或只做 E2E。前者无法证明真实路由/会话行为，后者反馈慢且难以覆盖错误分支；两层组合在成本与证据强度间平衡。真实后端 E2E 属于 `stage-1-core-integration`，本 change 的 E2E 明确以 mock 为环境边界，不冒充真实数据验收。

### 7. 懒加载路由与 SPA 回退

页面按 feature 目录懒加载，主包约 572 kB（gzip 184 kB），当前仅触发 Vite >500 kB 警告。使用 BrowserRouter，部署时需要服务端回退到 `index.html`（Caddy 配置在后续部署 change 中补）。替代方案：HashRouter 免服务端配置，但 URL 形态与后续 Caddy/观测不统一，选择 BrowserRouter。

### 8. 变更请求带 Idempotency-Key

REST data provider 的 POST/PATCH/DELETE 在边界生成 `Idempotency-Key`，与架构 16.5 对齐；mock provider 忽略该头。替代方案：等后端要求后再加，会迫使所有页面改动，故前置到适配层。

## Risks / Trade-offs

- [手写领域类型与冻结 proto 漂移] → 领域类型集中在 `web/src/api/types.ts`，生成物已在 `web/src/generated`；集成 change 用生成客户端替换该边界，并补契约测试。
- [临时 REST 路径与后端实际路由不一致] → 路径集中在 `RESOURCE_PATHS`，集成时一次对齐；E2E 仅断言页面行为，不把 mock URL 当契约。
- [mock 夹具与真实数据语义偏离] → 夹具带 `synthetic` 语义、仅用于测试；真实数据验证留给集成 Stage，报告不得将 mock 结果写成生产验收。
- [OIDC 未对真实 Keycloak 验证] → 单元测试覆盖 PKCE/state/nonce/JWT 解析与失败拒绝；真实 IdP 联调列为集成依赖。
- [localStorage 会话的 XSS 风险] → 仅存短期 token、无长期 refresh token；生产前复核 CSP 与 BFF/内存态方案。
- [主包 572 kB 触发体积警告] → 路由懒加载已启用；后续按需拆分 Refine/vendor chunk，不阻塞本 change 门禁。
- [Node 26 下 Vitest localStorage 实验警告] → 仅警告，测试通过；CI 使用 Node 22，预计不出现。
- [`@bufbuild/protobuf` 运行时依赖来自并行生成链] → 若平台回退生成物，可移除该依赖；当前保留以通过 `tsc` 与构建。

## Migration Plan

1. 本 change 为纯新增：落地 `web/**` 与 OpenSpec artifacts，不触碰生产数据或后端。
2. 启用 CI frontend job（`web/package.json` 存在时执行 `pnpm install --frozen-lockfile`、`pnpm check`、`pnpm build`、`pnpm test`）。
3. 集成阶段：配置真实 `VITE_API_BASE_URL`、`VITE_AUTH_MODE=oidc` 与 Keycloak 参数；将 `createMockDataProvider` 切换为真实 REST/生成客户端适配器；按后端实际路由更新集中映射。
4. 回滚：将 `VITE_API_MODE`/`VITE_AUTH_MODE` 切回 mock，或回退本 change 新增的 `web/**`；无数据库迁移、无数据清理、无破坏性接口。

## Open Questions

- 真实后端的 REST/Connect 路由与 OpenAPI 生成物何时冻结？由 `stage-1-core-integration` 回答，不改变本 change 的页面行为。
- 生产 OIDC client 配置与登出策略（RP-initiated logout）由部署与安全评审确定。
- 是否需要 URL 级语言前缀与深链分享？当前不需要，若需要可在路由层增补而不改页面。
