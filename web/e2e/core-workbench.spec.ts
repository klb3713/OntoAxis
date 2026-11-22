import { expect, test } from "@playwright/test";

/**
 * mock 模式下的页面级 smoke E2E。
 *
 * 该用例只验证 Stage 1 冻结的六条核心路径在浏览器中可达、可交互，
 * 不替代 `stage-1-core-integration` 在真实后端上的 golden E2E。
 */

test.describe("登录", () => {
  test("mock 登录成功后进入项目工作台", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByRole("heading", { name: "欢迎回到元轴" })).toBeVisible();

    await page.getByRole("button", { name: "登录工作台" }).click();

    await expect(page).toHaveURL(/\/projects$/);
    await expect(page.getByRole("main").getByRole("heading", { name: "项目", level: 1 })).toBeVisible();
  });

  test("非法邮箱被前端拒绝且停留登录页", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("邮箱").fill("not-an-email");
    await page.getByRole("button", { name: "登录工作台" }).click();

    await expect(page.getByRole("alert")).toHaveText("请输入有效的邮箱地址");
    await expect(page).toHaveURL(/\/login$/);
  });
});

test.describe("核心工作台", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/login");
    await page.getByRole("button", { name: "登录工作台" }).click();
    await expect(page).toHaveURL(/\/projects$/);
  });

  test("六条核心路径均可导航并渲染资源", async ({ page }) => {
    await expect(page.getByRole("main").getByRole("heading", { name: "项目", level: 1 })).toBeVisible();
    await expect(page.getByRole("cell", { name: "customer-360" })).toBeVisible();

    await page.getByRole("link", { name: "Ontology 管理", exact: true }).click();
    await expect(page.getByRole("main").getByRole("heading", { name: "Ontology 管理", level: 1 })).toBeVisible();
    await expect(page.getByText("Customer", { exact: true }).first()).toBeVisible();

    await page.getByRole("link", { name: "对象浏览", exact: true }).click();
    await expect(page.getByRole("main").getByRole("heading", { name: "对象浏览", level: 1 })).toBeVisible();
    await expect(page.getByLabel("对象类型")).toBeVisible();
    await expect(page.getByText("C-1001").first()).toBeVisible();

    await page.getByRole("link", { name: "数据集", exact: true }).click();
    await expect(page.getByRole("main").getByRole("heading", { name: "数据集", level: 1 })).toBeVisible();
    await expect(page.getByText("orders_raw", { exact: false }).first()).toBeVisible();

    await page.getByRole("link", { name: "查询", exact: true }).click();
    await expect(page.getByRole("main").getByRole("heading", { name: "查询", level: 1 })).toBeVisible();
    await expect(page.getByLabel("SQL 编辑器")).toBeVisible();
  });

  test("创建项目后列表出现新记录", async ({ page }) => {
    const projectName = `e2e-${Date.now().toString(36)}`;

    await page.getByRole("button", { name: "新建项目" }).click();
    const dialog = page.getByRole("dialog", { name: "新建项目" });
    await expect(dialog).toBeVisible();

    await dialog.getByLabel("项目标识").fill(projectName);
    await dialog.getByLabel("项目名称").fill("E2E 项目");
    await dialog.getByLabel("描述").fill("Playwright mock 流程");
    await dialog.getByRole("button", { name: "新建", exact: true }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByText("项目已创建")).toBeVisible();
    await expect(page.getByRole("cell", { name: projectName })).toBeVisible();
  });

  test("选择对象后展示属性详情与原始 JSON", async ({ page }) => {
    await page.getByRole("link", { name: "对象浏览", exact: true }).click();
    await page.getByText("C-1001", { exact: true }).first().click();

    await expect(page.getByText("对象详情", { exact: true })).toBeVisible();
    await expect(page.getByText("customer_id", { exact: true })).toBeVisible();
    await expect(page.locator("pre.json-block")).toContainText("\"customer_id\": \"C-1001\"");
  });

  test("退出登录清除会话并回到登录页", async ({ page }) => {
    await page.getByRole("button", { name: "退出登录", exact: true }).click();

    await expect(page).toHaveURL(/\/login$/);
    await page.goto("/projects");
    await expect(page).toHaveURL(/\/login(?:\?|$)/);
    await expect(page.getByRole("heading", { name: "欢迎回到元轴" })).toBeVisible();
  });

  test("切换英文并在刷新后保持", async ({ page }) => {
    await page.getByLabel("语言").selectOption("en");

    await expect(page.getByRole("link", { name: "Projects", exact: true })).toBeVisible();
    await expect(page.getByRole("main").getByRole("heading", { name: "Projects", level: 1 })).toBeVisible();

    await page.reload();
    await expect(page.getByLabel("Language")).toHaveValue("en");
    await expect(page.getByRole("main").getByRole("heading", { name: "Projects", level: 1 })).toBeVisible();
  });

  test("创建对象类型并添加属性", async ({ page }) => {
    const suffix = Date.now().toString(36);
    const typeName = `e2e_type_${suffix}`;

    await page.getByRole("link", { name: "Ontology 管理", exact: true }).click();
    await page.getByRole("button", { name: "新建对象类型" }).click();

    const createDialog = page.getByRole("dialog", { name: "创建对象类型" });
    await createDialog.getByLabel("类型标识").fill(typeName);
    await createDialog.getByLabel("显示名称").fill("E2E 对象类型");
    await createDialog.getByRole("button", { name: "新建", exact: true }).click();

    await expect(page.getByRole("status")).toContainText("对象类型已创建");
    await expect(page.getByText("E2E 对象类型", { exact: true }).first()).toBeVisible();

    await page.getByRole("button", { name: "添加属性" }).click();
    const propertyDialog = page.getByRole("dialog", { name: "添加属性" });
    await propertyDialog.getByLabel("属性名", { exact: true }).fill("e2e_tier");
    await propertyDialog.getByLabel("属性名称").fill("E2E 等级");
    await propertyDialog.getByRole("button", { name: "保存", exact: true }).click();

    await expect(page.getByRole("status")).toContainText("属性已添加");
    await expect(page.getByRole("cell", { name: "e2e_tier" })).toBeVisible();
  });

  test("数据集预览展示 schema 与行", async ({ page }) => {
    await page.getByRole("link", { name: "数据集", exact: true }).click();
    await page.getByRole("button", { name: "预览", exact: true }).first().click();

    const dialog = page.getByRole("dialog", { name: "数据预览" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("order_id");
    await expect(dialog).toContainText("SO-2026-0001");

    await dialog.getByRole("button", { name: "关闭预览" }).click();
    await expect(dialog).toBeHidden();
  });

  test("保存查询、载入 SQL 并查看执行计划", async ({ page }) => {
    const sql = "SELECT * FROM products LIMIT 1";
    const queryName = `e2e_query_${Date.now().toString(36)}`;

    await page.getByRole("link", { name: "查询", exact: true }).click();
    await page.getByLabel("SQL 编辑器").fill(sql);
    await page.getByRole("button", { name: "保存查询", exact: true }).click();

    const dialog = page.getByRole("dialog", { name: "保存为查询" });
    await dialog.getByLabel("查询名称").fill(queryName);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();

    await expect(page.getByRole("status")).toContainText("查询已保存");
    await page.getByRole("button", { name: queryName }).click();
    await expect(page.getByLabel("SQL 编辑器")).toHaveValue(sql);

    await page.getByRole("button", { name: "执行计划", exact: true }).click();
    await expect(page.getByText("查询计划", { exact: true })).toBeVisible();
    await expect(page.locator("pre.json-block")).toContainText("Scan orders");
  });

  test("执行 SELECT 查询并展示结果", async ({ page }) => {
    await page.getByRole("link", { name: "查询", exact: true }).click();
    await page.getByLabel("SQL 编辑器").fill("SELECT * FROM orders LIMIT 2");
    await page.getByRole("button", { name: "执行查询" }).click();

    await expect(page.getByText("order_id", { exact: true })).toBeVisible();
    await expect(page.getByText("SO-2026-0001")).toBeVisible();
    await expect(page.getByText("2 行")).toBeVisible();
  });

  test("空 SQL 被拒绝并提示错误", async ({ page }) => {
    await page.getByRole("link", { name: "查询", exact: true }).click();
    await page.getByLabel("SQL 编辑器").fill("");
    await page.getByRole("button", { name: "执行查询" }).click();

    await expect(page.getByRole("alert")).toContainText("请输入 SQL 后再执行");
  });

  test("非 SELECT 语句被拒绝并提示错误", async ({ page }) => {
    await page.getByRole("link", { name: "查询", exact: true }).click();
    await page.getByLabel("SQL 编辑器").fill("DELETE FROM orders");
    await page.getByRole("button", { name: "执行查询" }).click();

    await expect(page.getByRole("alert")).toContainText("当前只支持 SELECT 查询");
  });
});
