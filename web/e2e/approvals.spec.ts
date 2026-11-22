import { expect, test } from "@playwright/test";

/**
 * 审批中心 mock E2E。仅验证前端交互与状态刷新，不替代真实后端审批证据。
 */
test.describe("审批中心", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("button", { name: "登录工作台" }).click();
    await expect(page).toHaveURL(/\/projects$/);
    await page.getByRole("link", { name: "审批", exact: true }).click();
    await expect(page.getByRole("heading", { name: "审批中心", level: 1 })).toBeVisible();
  });

  test("列表 → 批准 → 服务端刷新", async ({ page }) => {
    await expect(page.getByRole("tab", { name: "待审批" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("button", { name: "查看", exact: true }).first()).toBeVisible();

    await page.getByRole("button", { name: "查看", exact: true }).first().click();
    const dialog = page.getByRole("dialog", { name: "审批详情" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "批准", exact: true }).click();

    await expect(page.getByRole("status")).toContainText("已批准，状态已从服务端刷新。");
  });

  test("拒绝需要备注并刷新列表", async ({ page }) => {
    await page.getByRole("button", { name: "查看", exact: true }).first().click();
    const dialog = page.getByRole("dialog", { name: "审批详情" });
    await dialog.getByLabel("备注").fill("mock E2E 拒绝");
    await dialog.getByRole("button", { name: "拒绝", exact: true }).click();

    await expect(page.getByRole("status")).toContainText("已拒绝，状态已从服务端刷新。");
  });

  test("无审批权限时保持任务状态并提示", async ({ page }) => {
    await page.addInitScript(() => {
      (window as unknown as { __MOCK_APPROVAL_FORBIDDEN__?: boolean }).__MOCK_APPROVAL_FORBIDDEN__ = true;
    });
    await page.reload();
    await expect(page.getByRole("heading", { name: "审批中心", level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "查看", exact: true }).first().click();
    const dialog = page.getByRole("dialog", { name: "审批详情" });
    await dialog.getByRole("button", { name: "批准", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText("无审批权限");
    await expect(dialog.getByRole("button", { name: "批准", exact: true })).toBeVisible();
  });

  test("处理完全部待审批后显示空态", async ({ page }) => {
    for (let i = 0; i < 2; i += 1) {
      await page.getByRole("button", { name: "查看", exact: true }).first().click();
      const dialog = page.getByRole("dialog", { name: "审批详情" });
      await dialog.getByRole("button", { name: "批准", exact: true }).click();
      await expect(page.getByRole("status")).toContainText("已批准，状态已从服务端刷新。");
    }
    await expect(page.getByText("当前筛选条件下没有审批任务。")).toBeVisible();
  });
});
