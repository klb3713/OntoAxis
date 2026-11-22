# Traceability: Frontend Core Workbench

## Stage AC 追溯

| Stage AC | 对应行为 | Requirement / Scenario | 计划或已执行证据 | 环境 | 状态 |
| --- | --- | --- | --- | --- | --- |
| `S1-AC-10`：`oa-web` 的登录、Projects、Ontology Manager、Object Explorer、Datasets、Queries 页面通过 Playwright E2E | 六条核心路径可在浏览器中登录、导航、浏览与交互 | `受保护路由与会话`、`登录适配模式`、`项目资源浏览与创建`、`Ontology 模型管理`、`对象浏览`、`数据集目录与预览`、`查询工作台`、`导航与国际化` 的对应 scenario | `cd web && pnpm test:e2e`：13 个 Playwright 用例通过（登录成功、非法邮箱、六页导航、创建项目、创建对象类型并添加属性、对象详情、数据集预览、SELECT 执行、空 SQL 拒绝、非 SELECT 拒绝、保存/载入查询与执行计划、英文切换与持久化、退出登录），Chromium 1255 headless，Vite 4173，`VITE_API_MODE=mock`/`VITE_AUTH_MODE=mock` | 本地 mock 模式；无真实后端、无 Keycloak、无真实数据 | `partial`：mock 基线通过；真实 `/api/v1` 与真实 IdP 的 golden E2E 未执行，待 `stage-1-core-integration` |
| `S1-AC-11`：本地 Compose 可一键启动核心闭环；CI 通过 build、test、契约（含 `buf lint`/`buf breaking`）、前端与许可证门禁 | 前端 check/build/test 门禁可执行，且页面与冻结契约不冲突 | `API 运行时适配与错误处理`、`导航与国际化` 及上述页面 requirement | `cd web && pnpm check` 通过；`pnpm build` 通过（366 modules，主 chunk 约 572.6 kB / gzip 约 184.1 kB，>500 kB 警告）；`pnpm test` 5 文件 / 26 用例通过；`pnpm test:e2e` 13/13 通过 | 本地 Node 26、pnpm 10、Vite 7.3.8、Vitest 3.2.7、Playwright 1.65.1 | `partial`：本地前端门禁通过；CI frontend job、`buf lint`/`buf breaking`、Compose 闭环与许可证门禁未在本 change 执行 |

> 本 change 不声明 Stage 1 退出，也不将上述 `partial` 结果写成 `verified`。Stage AC 的完整验证需要集成 change 与 CI 实际运行。

## Requirement 证据

| Requirement | 覆盖场景 | 证据 | 状态 |
| --- | --- | --- | --- |
| 受保护路由与会话 | 未认证重定向、登录后回原页面、退出清会话 | `web/e2e/core-workbench.spec.ts` 登录与核心工作台用例（13/13） | `verified`（mock 范围） |
| 登录适配模式 | mock 成功、无效邮箱拒绝、OIDC 回调失败拒绝 | E2E 登录/非法邮箱；`web/src/auth/oidc.test.ts` 5 用例；`web/src/api/mock-data-provider.test.ts` | `partial`：mock 与 OIDC 协议单测通过；真实 Keycloak 联调未执行 |
| 项目资源浏览与创建 | 分页/搜索、创建成功、重复标识冲突 | E2E 创建项目；`mock-data-provider.test.ts` 409 用例；`ProjectsPage` 由 `pnpm check` 覆盖类型 | `partial`（mock 范围）：创建与列表通过；重复标识冲突仅单测，UI 输入保留未单独 E2E |
| Ontology 模型管理 | 查看对象类型属性、创建对象类型、添加属性、关系类型只读 | E2E 创建对象类型并添加属性（断言提示、列表与属性表）；E2E 六页导航断言 Ontology 页面与 `Customer` 可见 | `partial`（mock 范围）：对象类型与属性通过；关系类型只读视图未单独 E2E |
| 对象浏览 | 按类型过滤、对象详情、无实例空态 | E2E 六页导航断言对象浏览页与 `C-1001`；E2E 选择对象展示“对象详情”、`customer_id` 与原始 JSON | `partial`（mock 范围）：列表与详情通过；无实例空态未单独 E2E |
| 数据集目录与预览 | 列表字段、预览列/行/行列数、预览失败重试 | E2E 数据集预览展示 `order_id` 与 `SO-2026-0001`；`mock-data-provider.test.ts` 预览用例 | `partial`（mock 范围）：成功预览通过；预览失败与重试未做浏览器级验证 |
| 查询工作台 | SELECT 执行、空 SQL 拒绝、非 SELECT 拒绝、保存/载入、执行计划 | E2E SELECT 结果（`2 行`）、空 SQL 拒绝、非 SELECT 拒绝、保存/载入查询与执行计划；`mock-data-provider.test.ts` 补充 422/400/404 与计划用例 | `verified`（mock 范围）；真实后端未验证 |
| API 运行时适配与错误处理 | 可配置 base/mock/http、响应归一化、非法配置拒绝启动、可重试错误 UI | `web/src/config/runtime.test.ts` 4 用例；`web/src/api/normalize.test.ts` 6 用例；`web/src/api/errors.test.ts` 4 用例 | `partial`：边界单测通过；错误 UI 重试与真实 HTTP 适配未做浏览器级验证 |
| 导航与国际化 | 六条路径导航、中英切换持久化、默认语言 | E2E 六页导航；E2E 切换英文并在刷新后保持；`web/src/i18n/zh.ts`/`en.ts` key 镜像 | `verified`（mock 范围）；真实后端未验证 |

## 未决依赖与边界

- **真实后端未接入**：`VITE_API_MODE=http` 与 `web/src/api/rest-data-provider.ts` 的资源路径尚未与后端实际 `/api/v1` 路由对齐；集成由 `stage-1-core-integration` 负责。
- **真实认证未联调**：OIDC 适配器仅有协议级单元测试，未对 Keycloak realm、真实 client 与回调地址验证。
- **生成客户端未替换**：`web/src/generated/ontoaxis/v1/**` 已由平台生成链产出，但页面仍消费 `web/src/api/types.ts` 的手写领域类型；集成 change 需以生成客户端替换边界并补契约测试。
- **CI 未实际运行**：本 change 未 commit，GitHub Actions frontend job 未触发；本地 `check`/`build`/`test`/`test:e2e` 结果不能替代 CI 结论。
- **合成数据边界**：所有 mock 数据均为合成夹具，只用于本地与 CI 测试，不代表真实客户数据或生产验收。
- **增强组件未实现**：shadcn/ui、TanStack Table、ECharts、Cytoscape、Monaco、完整 golden E2E 与生产发布不在本 change 范围。
