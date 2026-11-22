import { describe, expect, it } from "vitest";
import { createFixtureDatabase } from "./fixtures";
import { createMockDataProvider } from "./mock-data-provider";
import type { DatasetPreview, Project, QueryResult } from "./types";

function provider() {
  return createMockDataProvider({ database: createFixtureDatabase(), latencyMs: 0 });
}

function providerWithCustom() {
  const dataProvider = provider();
  const custom = dataProvider.custom;
  if (!custom) {
    throw new Error("mock data provider 必须实现 custom");
  }
  return { dataProvider, custom };
}

describe("mock DataProvider", () => {
  it("按 page/perPage 返回项目与总数", async () => {
    const result = await provider().getList<Project>({
      resource: "projects",
      pagination: { currentPage: 1, pageSize: 2 },
    });

    expect(result.total).toBe(3);
    expect(result.data).toHaveLength(2);
    expect(result.data[0]?.name).toBe("customer-360");
  });

  it("q 搜索匹配项目名称", async () => {
    const result = await provider().getList<Project>({
      resource: "projects",
      pagination: { currentPage: 1, pageSize: 10 },
      filters: [{ field: "q", operator: "contains", value: "risk" }],
    });

    expect(result.data.map((project) => project.name)).toEqual(["risk-control"]);
  });

  it("重复项目标识返回 409", async () => {
    await expect(
      provider().create({
        resource: "projects",
        variables: { name: "customer-360", displayName: "重复项目" },
      }),
    ).rejects.toMatchObject({ status: 409, code: "conflict" });
  });

  it("创建项目后可从列表读取", async () => {
    const dataProvider = provider();
    const created = await dataProvider.create<Project>({
      resource: "projects",
      variables: { name: "new-domain", displayName: "新域", description: "demo" },
    });
    const listed = await dataProvider.getList<Project>({
      resource: "projects",
      pagination: { currentPage: 1, pageSize: 10 },
      filters: [{ field: "q", operator: "contains", value: "new-domain" }],
    });

    expect(created.data.name).toBe("new-domain");
    expect(listed.data).toHaveLength(1);
  });

  it("数据集预览返回 schema 与行数据", async () => {
    const { custom } = providerWithCustom();
    const result = await custom<DatasetPreview>({
      url: "datasets/ds_orders_raw/preview",
      method: "get",
    });

    expect(result.data.columns.length).toBeGreaterThan(0);
    expect(result.data.rows.length).toBeGreaterThan(0);
  });

  it("查询执行仅接受 SELECT，并拒绝空 SQL 与缺失表", async () => {
    const { custom } = providerWithCustom();
    const result = await custom<QueryResult>({
      url: "queries/execute",
      method: "post",
      payload: { sql: "SELECT * FROM orders LIMIT 10" },
    });
    expect(result.data.columns).toContain("order_id");
    expect(result.data.rows.length).toBeGreaterThan(0);

    await expect(
      custom({ url: "queries/execute", method: "post", payload: { sql: "" } }),
    ).rejects.toMatchObject({ status: 422, code: "empty_query" });

    await expect(
      custom({ url: "queries/execute", method: "post", payload: { sql: "DELETE FROM orders" } }),
    ).rejects.toMatchObject({ status: 400, code: "unsupported_statement" });

    await expect(
      custom({ url: "queries/execute", method: "post", payload: { sql: "SELECT * FROM missing_table" } }),
    ).rejects.toMatchObject({ status: 404, code: "table_not_found" });
  });

  it("执行计划返回可读计划文本", async () => {
    const { custom } = providerWithCustom();
    const result = await custom<{ plan: string }>({
      url: "queries/explain",
      method: "post",
      payload: { sql: "SELECT * FROM orders" },
    });
    expect(result.data.plan).toContain("Scan orders");
  });
});
