import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright 基础 E2E 配置。
 *
 * Stage 1 的完整 golden E2E 由 `stage-1-core-integration` 在真实后端 + Compose 上运行。
 * 本 change 提供 mock 模式下的页面级 smoke 用例，保证页面结构与路由可验证。
 *
 * 运行方式：
 *   pnpm exec playwright install chromium   # 首次安装浏览器
 *   pnpm test:e2e
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: "pnpm dev --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: {
      VITE_API_MODE: "mock",
      VITE_AUTH_MODE: "mock",
    },
  },
});
