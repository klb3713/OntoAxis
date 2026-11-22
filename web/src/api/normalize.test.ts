import { describe, expect, it } from "vitest";
import { normalizeCustom, normalizeEntity, normalizeList } from "./normalize";

describe("normalizeList", () => {
  it("兼容 items + meta 分页响应", () => {
    const result = normalizeList<{ id: string }>({
      items: [{ id: "a" }, { id: "b" }],
      meta: { page: 2, perPage: 2, total: 7, totalPages: 4 },
    });

    expect(result.data).toHaveLength(2);
    expect(result.meta).toEqual({ page: 2, perPage: 2, total: 7, totalPages: 4 });
  });

  it("兼容资源键数组与 pagination", () => {
    const result = normalizeList<{ id: string }>(
      {
        projects: [{ id: "p1" }],
        pagination: { current_page: 3, page_size: 1, total_count: 5 },
      },
      ["projects"],
    );

    expect(result.data[0]?.id).toBe("p1");
    expect(result.meta.page).toBe(3);
    expect(result.meta.perPage).toBe(1);
    expect(result.meta.total).toBe(5);
    expect(result.meta.totalPages).toBe(5);
  });

  it("裸数组返回单页元数据", () => {
    const result = normalizeList([{ id: "x" }]);
    expect(result.meta).toMatchObject({ page: 1, perPage: 1, total: 1, totalPages: 1 });
  });

  it("缺少数组字段时拒绝响应", () => {
    expect(() => normalizeList({ meta: { total: 0 } })).toThrow(/列表响应缺少数组字段/);
  });
});

describe("normalizeEntity", () => {
  it("兼容直接对象、data 与资源键包裹", () => {
    expect(normalizeEntity<{ id: string }>({ id: "direct" }).id).toBe("direct");
    expect(normalizeEntity<{ id: string }>({ data: { id: "wrapped" } }).id).toBe("wrapped");
    expect(normalizeEntity<{ id: string }>({ projects: { id: "keyed" } }, ["projects"]).id).toBe("keyed");
  });
});

describe("normalizeCustom", () => {
  it("解开 data 包裹并保留直接负载", () => {
    expect(normalizeCustom<{ plan: string }>({ data: { plan: "Scan" } })).toEqual({ plan: "Scan" });
    expect(normalizeCustom<{ plan: string }>({ plan: "Scan" })).toEqual({ plan: "Scan" });
  });
});
