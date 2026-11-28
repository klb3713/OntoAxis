# Playwright E2E（任务 7.6）

- 日期：2026-11-28
- 环境：macOS 本地，Playwright 1.65.1，Chromium；`VITE_API_MODE=mock`、`VITE_AUTH_MODE=mock`
- 命令：`cd web && pnpm test:e2e --reporter=list`
- 结果：**17 passed / 0 failed**（2 个 spec 文件，9.5s）
- 原始日志：[`playwright-7.6.log`](playwright-7.6.log)

## 覆盖

| 文件 | 用例数 | 覆盖 |
| --- | --- | --- |
| `web/e2e/core-workbench.spec.ts` | 13 | mock 登录/非法邮箱拒绝；六条核心路径导航；创建项目；对象详情原始 JSON；退出登录；i18n 切换保持；创建对象类型并添加属性；数据集预览；查询保存/载入/执行计划；SELECT 执行；空 SQL 与非 SELECT 拒绝 |
| `web/e2e/approvals.spec.ts` | 4 | 审批列表→批准→服务端刷新；拒绝需备注并刷新；无审批权限提示；处理完空态 |

## 证据边界

- 本套件为 **mock 模式页面级 smoke**，不替代真实后端 golden E2E；真实后端链路由 E2E-01/02/03/07/08（`fullstack-e2e-7x.md`）覆盖。
- 未包含真实 OIDC（`web/e2e/real-oidc.mjs` 为独立手动脚本）。
