# Tasks: Frontend Core Workbench

## 1. 工程骨架与运行配置

- [x] 1.1 创建 `web/package.json`、`pnpm-lock.yaml`、`tsconfig.json`、`vite.config.ts`、`vitest.config.ts`、`index.html`、`web/.env.example` 与 `web/.gitignore`，声明 Node >=22.20.0、dev/build/check/test/test:e2e 脚本及锁定依赖。（验证：`pnpm install --frozen-lockfile` 与 `pnpm check` 通过）
- [x] 1.2 实现运行配置解析，支持 `VITE_API_MODE=mock|http`、`VITE_AUTH_MODE=mock|oidc`、`VITE_API_BASE_URL` 与 OIDC 参数；开发默认 mock、生产默认真实 API + OIDC，非法配置拒绝启动。（验证：`web/src/config/runtime.test.ts` 4 个用例覆盖默认值、OIDC 缺失、完整配置与非法 mode；`pnpm test` 通过）
- [x] 1.3 配置 Vite 开发代理 `/api`、构建输出与 Playwright/Vitest 运行环境。（验证：`pnpm build` 通过并生成 `dist/`；`pnpm test:e2e` 通过）

## 2. API 边界与数据提供器

- [x] 2.1 定义前端领域类型与统一错误模型，错误体兼容 `{error:{code,message,metadata,retryable,request_id}}`。（验证：`web/src/api/errors.test.ts` 4 个用例通过）
- [x] 2.2 实现列表/实体/自定义响应归一化，兼容 `data`、`items`、资源名包裹及 `meta`/`pagination` 分页形状。（验证：`web/src/api/normalize.test.ts` 6 个用例通过）
- [x] 2.3 实现 typed HTTP 客户端与 REST Refine DataProvider，集中资源路径映射，列表使用 `page`/`per_page`，变更请求携带 `Idempotency-Key`，错误统一映射。（验证：`pnpm check` 通过；真实后端联调留给集成 change）
- [x] 2.4 实现 mock DataProvider 与合成夹具，覆盖分页、搜索、创建冲突、数据集预览、SELECT 执行/计划与非 SELECT 拒绝。（验证：`web/src/api/mock-data-provider.test.ts` 7 个用例通过）
- [x] 2.5 通过 `createApiRuntime` 在 mock/http 之间切换，页面只依赖 `DataProvider` 接口。（验证：`pnpm test` 与 `pnpm test:e2e` 均通过）

## 3. 认证与会话

- [x] 3.1 实现会话持久化、读取、清理与未认证抛错，未认证访问受保护路由时重定向登录页。（验证：`pnpm test:e2e` 登录与受保护导航用例通过）
- [x] 3.2 实现 mock 登录适配器：邮箱格式校验、密码必填、有效登录建立本地合成用户会话。（验证：E2E 登录成功与非法邮箱用例通过）
- [x] 3.3 实现 OIDC 授权码 + PKCE 适配器，校验 state/nonce、解析 ID Token、过期前刷新，失败时拒绝登录并清理事务。（验证：`web/src/auth/oidc.test.ts` 5 个用例通过；真实 Keycloak 联调未执行）
- [x] 3.4 将认证适配器桥接为 Refine `AuthProvider`，支持登录、退出、会话检查与权限读取。（验证：`pnpm check` 通过；E2E 登录与退出登录用例通过）

## 4. 页面与导航

- [x] 4.1 实现工作台布局与侧边导航，持续展示 Projects、Ontology、对象浏览、数据集、查询入口，并显示当前用户、运行模式与语言切换。（验证：`web/e2e/core-workbench.spec.ts` 导航用例通过）
- [x] 4.2 实现登录页与 OIDC 回调页；登录后返回原请求页面，回调失败显示可读错误。（验证：E2E 登录成功、非法邮箱用例通过；OIDC 回调由单元测试覆盖协议校验）
- [x] 4.3 实现 Projects 页面：分页、搜索、创建表单、重复标识冲突提示与空态/错误态。（验证：E2E 创建项目后列表出现；mock provider 409 单元测试通过）
- [x] 4.4 实现 Ontology Manager：对象类型与属性查看、创建对象类型、添加属性、关系类型只读展示。（验证：`pnpm check` 通过；E2E 创建对象类型并添加属性用例通过）
- [x] 4.5 实现 Object Explorer：按对象类型过滤、对象列表、详情属性/版本/原始 JSON 与空态。（验证：E2E 选择对象展示属性详情与原始 JSON 用例通过）
- [x] 4.6 实现 Datasets 页面：列表、搜索、分页、创建数据集与预览 Modal（列结构、样例行、行列数、失败重试）。（验证：E2E 数据集预览用例通过）
- [x] 4.7 实现 Queries 页面：SQL 编辑、执行、执行计划、保存查询、载入已保存查询，拒绝空 SQL 与非 SELECT。（验证：E2E SELECT 结果与非 SELECT 拒绝用例通过）
- [x] 4.8 实现 404 页面与懒加载路由，未匹配路由不渲染工作台业务页。（验证：`pnpm build` 通过并按页面生成懒加载 chunk）

## 5. 国际化与体验状态

- [x] 5.1 提供简体中文与英文完整资源，key 镜像一致，覆盖导航、页面、表单、错误与空态文案。（验证：`pnpm check` 通过；E2E 中文默认界面与英文切换用例通过）
- [x] 5.2 实现语言切换与 `localStorage` 持久化，未设置时使用运行配置默认语言。（验证：E2E 切换英文并在刷新后保持用例通过）
- [x] 5.3 实现统一加载、空态、错误与重试组件，页面不静默吞错。（验证：错误映射与重试单元测试通过；页面错误态由 `ErrorBlock` 渲染）

## 6. 测试与质量门禁

- [x] 6.1 增加 Vitest 单元/适配器测试，覆盖错误映射、归一化、运行配置、OIDC 协议与 mock 数据行为。（验证：`pnpm test` 5 个文件、26 个测试全部通过）
- [x] 6.2 增加 Playwright mock 模式 E2E，覆盖登录/退出、六页导航、创建项目、创建对象类型与属性、对象详情、数据集预览、查询执行/空 SQL/非 SELECT/保存载入/执行计划、语言切换。（验证：`pnpm test:e2e` 13 个用例全部通过；Chromium 1255 headless）
- [x] 6.3 通过 TypeScript 静态检查与生产构建。（验证：`pnpm check` 通过；`pnpm build` 通过，366 modules，主 chunk 约 572.6 kB / gzip 约 184.1 kB，仅 >500 kB 警告）
- [x] 6.4 安装 Playwright 浏览器依赖并固化 E2E 启动方式，本地可用 `pnpm test:e2e` 复现。（验证：`pnpm exec playwright install chromium` 成功；`pnpm test:e2e` 13/13 通过）

## 7. OpenSpec 与证据

- [x] 7.1 编写 `proposal.md`，说明 Why、变更范围、Out of scope、`web-core-workbench` capability 与影响。（验证：`openspec validate frontend-core --type change --strict` 通过）
- [x] 7.2 编写 `specs/web-core-workbench/spec.md`，以 9 条 requirement 覆盖六条核心路径、认证、API 适配、错误与国际化，每条含 WHEN/THEN scenario。（验证：`openspec validate frontend-core --type change --strict` 通过）
- [x] 7.3 编写 `design.md`，记录 resource-first 分层、运行模式 fail-closed、认证适配、响应归一化、测试分层与迁移回滚决策。（验证：`openspec validate frontend-core --type change --strict` 通过；人工复核）
- [x] 7.4 编写 `tasks.md`，按交付分组，每条任务写明可复核的验证方式。（验证：本文件；`openspec validate frontend-core --type change --strict` 通过）
- [x] 7.5 编写 `traceability.md`，将 S1-AC-10/11 映射到 requirement/scenario 与本 change 的本地证据，并明确真实后端/CI 未验证的边界。（验证：`openspec validate frontend-core --type change --strict` 通过；人工复核 AC 与未决依赖）
- [x] 7.6 运行 `openspec validate --all --strict`，修复结构或规范错误。（验证：`frontend-core` 通过并显示 `Change 'frontend-core' is valid`；`--all` 仍因其他并行 change 缺少 delta 失败，见交付报告）

## Workflow follow-up

- 由 `stage-1-core-integration` 接入真实 `/api/v1` 后端与 Keycloak，补齐真实后端 E2E、契约测试与生成客户端替换。
- 部署 change 需要为 Caddy 配置 SPA 深链回退，并复核 CSP、token 存储与 OIDC 登出策略。
- 前端 CI job 需在首次提交后实际运行，确认 `pnpm install --frozen-lockfile`、`check`、`build`、`test` 全绿；本 change 仅完成本地等价验证。
