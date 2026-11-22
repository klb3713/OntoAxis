import type {
  CreateParams,
  CreateResponse,
  CustomParams,
  CustomResponse,
  DataProvider,
  DeleteOneParams,
  DeleteOneResponse,
  GetListParams,
  GetListResponse,
  GetOneParams,
  GetOneResponse,
  UpdateParams,
  UpdateResponse,
} from "@refinedev/core";
import { ApiError } from "./errors";
import { createFixtureDatabase, type MockDatabase } from "./fixtures";
import type {
  Dataset,
  DatasetPreview,
  LinkType,
  ObjectRecord,
  ObjectType,
  Project,
  QueryResult,
  SavedQuery,
} from "./types";

type ResourceName = keyof Pick<
  MockDatabase,
  "projects" | "objectTypes" | "linkTypes" | "objects" | "datasets" | "savedQueries"
>;

const RESOURCE_TO_STORE: Record<string, ResourceName> = {
  projects: "projects",
  "object-types": "objectTypes",
  "link-types": "linkTypes",
  objects: "objects",
  datasets: "datasets",
  "saved-queries": "savedQueries",
};

function now(): string {
  return new Date().toISOString();
}

function newId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

function matchesSearch(value: unknown, search: string): boolean {
  return JSON.stringify(value).toLowerCase().includes(search.toLowerCase());
}

function paginate<T>(items: T[], page: number, perPage: number): { data: T[]; total: number } {
  const safePage = Math.max(1, page);
  const safePerPage = Math.min(200, Math.max(1, perPage));
  const start = (safePage - 1) * safePerPage;
  return { data: items.slice(start, start + safePerPage), total: items.length };
}

function findOrThrow<T extends { id: string }>(items: T[], id: string, label: string): T {
  const found = items.find((item) => item.id === id);
  if (!found) {
    throw new ApiError({ code: "not_found", message: `${label} 不存在：${id}`, status: 404 });
  }
  return found;
}

function buildQueryResult(sql: string): QueryResult {
  const normalized = sql.trim();
  if (!normalized) {
    throw new ApiError({ code: "empty_query", message: "SQL 不能为空", status: 422 });
  }
  if (!/^select\b/i.test(normalized)) {
    throw new ApiError({ code: "unsupported_statement", message: "当前只支持 SELECT 查询", status: 400 });
  }
  if (/missing_table/i.test(normalized)) {
    throw new ApiError({ code: "table_not_found", message: "查询引用的表不存在：missing_table", status: 404 });
  }

  const limitMatch = /limit\s+(\d+)/i.exec(normalized);
  const limit = limitMatch ? Math.min(Number(limitMatch[1]), 100) : 20;

  if (/customer_profiles/i.test(normalized)) {
    const rows: unknown[][] = [
      ["S", 1],
      ["A", 2],
    ];
    const visible = rows.slice(0, limit);
    return { columns: ["tier", "customer_count"], rows: visible, rowCount: visible.length, elapsedMs: 12 };
  }
  if (/products/i.test(normalized)) {
    const rows: unknown[][] = [
      ["P-01", "Ontology 建模平台", "数据平台"],
      ["P-02", "实时数据集成", "数据集成"],
    ];
    const visible = rows.slice(0, limit);
    return {
      columns: ["product_id", "title", "category"],
      rows: visible,
      rowCount: visible.length,
      elapsedMs: 9,
    };
  }
  if (/orders/i.test(normalized)) {
    const rows: unknown[][] = [
      ["SO-2026-0001", "C-1001", 128000, "paid"],
      ["SO-2026-0002", "C-1002", 76000, "shipped"],
      ["SO-2026-0003", "C-1003", 43000, "created"],
    ];
    const visible = rows.slice(0, limit);
    return {
      columns: ["order_id", "customer_id", "amount", "status"],
      rows: visible,
      rowCount: visible.length,
      elapsedMs: 15,
    };
  }
  return { columns: ["result"], rows: [["mock query executed"]], rowCount: 1, elapsedMs: 5 };
}

export type MockDataProviderOptions = {
  database?: MockDatabase;
  latencyMs?: number;
};

/**
 * 内存 mock DataProvider：仅用于本地开发与测试。
 *
 * 返回结构与 REST adapter 完全一致（Refine DataProvider 契约），因此页面在
 * mock/http 两种模式下无需改变；后续接入真实后端时仅切换 `VITE_API_MODE`。
 */
export function createMockDataProvider(options: MockDataProviderOptions = {}): DataProvider {
  const db = options.database ?? createFixtureDatabase();
  const latency = options.latencyMs ?? 0;
  const wait = () => (latency > 0 ? new Promise<void>((resolve) => setTimeout(resolve, latency)) : Promise.resolve());

  const provider: DataProvider = {
    getApiUrl: () => "mock://api/v1",

    async getList<TData extends { [key: string]: any } = { [key: string]: any }>(
      params: GetListParams,
    ): Promise<GetListResponse<TData>> {
      await wait();
      const storeKey = RESOURCE_TO_STORE[params.resource];
      if (!storeKey) throw new ApiError({ code: "unknown_resource", message: `未知资源：${params.resource}`, status: 400 });

      const filters = params.filters ?? [];
      const q = filters.find((filter) => "field" in filter && filter.field === "q");
      const search = q && "value" in q ? String(q.value ?? "").trim() : "";
      const objectTypeFilter = filters.find((filter) => "field" in filter && filter.field === "object_type_id");
      const objectTypeId = objectTypeFilter && "value" in objectTypeFilter ? String(objectTypeFilter.value ?? "") : "";

      let items: Array<{ id: string }> = [...db[storeKey]] as Array<{ id: string }>;
      if (search) {
        if (storeKey === "projects") {
          items = (items as Project[]).filter(
            (item) =>
              item.name.toLowerCase().includes(search.toLowerCase()) ||
              item.displayName.toLowerCase().includes(search.toLowerCase()) ||
              item.description.toLowerCase().includes(search.toLowerCase()),
          );
        } else if (storeKey === "objects") {
          items = (items as ObjectRecord[]).filter((item) => matchesSearch(item.properties, search));
        } else if (storeKey === "datasets") {
          items = (items as Dataset[]).filter(
            (item) => item.name.toLowerCase().includes(search.toLowerCase()) || item.displayName.toLowerCase().includes(search.toLowerCase()),
          );
        } else if (storeKey === "savedQueries") {
          items = (items as SavedQuery[]).filter(
            (item) => item.name.toLowerCase().includes(search.toLowerCase()) || item.sql.toLowerCase().includes(search.toLowerCase()),
          );
        } else {
          items = items.filter((item) => matchesSearch(item, search));
        }
      }
      if (objectTypeId) {
        items = (items as ObjectRecord[]).filter((item) => item.objectTypeId === objectTypeId);
      }

      const sorter = params.sorters?.[0];
      if (sorter) {
        items = [...items].sort((a, b) => {
          const left = String((a as Record<string, unknown>)[sorter.field] ?? "");
          const right = String((b as Record<string, unknown>)[sorter.field] ?? "");
          return sorter.order === "desc" ? right.localeCompare(left) : left.localeCompare(right);
        });
      }

      const page = params.pagination?.currentPage ?? 1;
      const perPage = params.pagination?.pageSize ?? 50;
      const result = paginate(items, page, perPage);
      return { data: result.data as unknown as TData[], total: result.total };
    },

    async getOne<TData extends { [key: string]: any } = { [key: string]: any }>(
      params: GetOneParams,
    ): Promise<GetOneResponse<TData>> {
      await wait();
      const storeKey = RESOURCE_TO_STORE[params.resource];
      if (!storeKey) throw new ApiError({ code: "unknown_resource", message: `未知资源：${params.resource}`, status: 400 });
      const item = findOrThrow(db[storeKey] as Array<{ id: string }>, String(params.id), params.resource);
      return { data: item as unknown as TData };
    },

    async create<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: CreateParams<TVariables>,
    ): Promise<CreateResponse<TData>> {
      await wait();
      const values = asRecord(params.variables);
      const timestamp = now();

      if (params.resource === "projects") {
        const name = String(values.name ?? "").trim();
        const displayName = String(values.displayName ?? "").trim();
        if (!name) throw new ApiError({ code: "validation_error", message: "项目标识不能为空", status: 422 });
        if (!displayName) throw new ApiError({ code: "validation_error", message: "项目名称不能为空", status: 422 });
        if (db.projects.some((project) => project.name === name)) {
          throw new ApiError({ code: "conflict", message: `项目标识已存在：${name}`, status: 409 });
        }
        const project: Project = {
          id: newId("prj"),
          organizationId: "org_01",
          workspaceId: "ws_01",
          name,
          displayName,
          description: String(values.description ?? ""),
          ownerId: "usr_01",
          createdAt: timestamp,
          updatedAt: timestamp,
        };
        db.projects.unshift(project);
        return { data: project as unknown as TData };
      }

      if (params.resource === "object-types") {
        const name = String(values.name ?? "").trim();
        const displayName = String(values.displayName ?? "").trim();
        if (!name) throw new ApiError({ code: "validation_error", message: "对象类型标识不能为空", status: 422 });
        if (!displayName) throw new ApiError({ code: "validation_error", message: "对象类型名称不能为空", status: 422 });
        if (db.objectTypes.some((objectType) => objectType.name === name)) {
          throw new ApiError({ code: "conflict", message: `对象类型已存在：${name}`, status: 409 });
        }
        const objectType: ObjectType = {
          id: newId("ot"),
          name,
          displayName,
          description: String(values.description ?? ""),
          primaryKeyProperty: "",
          properties: [],
          createdAt: timestamp,
          updatedAt: timestamp,
        };
        db.objectTypes.push(objectType);
        return { data: objectType as unknown as TData };
      }

      if (params.resource === "datasets") {
        const name = String(values.name ?? "").trim();
        const displayName = String(values.displayName ?? "").trim();
        if (!name) throw new ApiError({ code: "validation_error", message: "数据集标识不能为空", status: 422 });
        if (!displayName) throw new ApiError({ code: "validation_error", message: "数据集名称不能为空", status: 422 });
        const dataset: Dataset = {
          id: newId("ds"),
          name,
          displayName,
          description: String(values.description ?? ""),
          ownerId: "usr_01",
          format: values.format === "PARQUET" ? "PARQUET" : "ICEBERG",
          storageLocation: String(values.storageLocation ?? `s3://ontoaxis-dev/warehouse/${name}`),
          createdAt: timestamp,
          updatedAt: timestamp,
        };
        db.datasets.unshift(dataset);
        return { data: dataset as unknown as TData };
      }

      if (params.resource === "saved-queries") {
        const name = String(values.name ?? "").trim();
        const sql = String(values.sql ?? "").trim();
        if (!name) throw new ApiError({ code: "validation_error", message: "查询名称不能为空", status: 422 });
        if (!sql) throw new ApiError({ code: "validation_error", message: "SQL 不能为空", status: 422 });
        const savedQuery: SavedQuery = {
          id: newId("sq"),
          name,
          sql,
          ownerId: "usr_01",
          createdAt: timestamp,
          updatedAt: timestamp,
        };
        db.savedQueries.unshift(savedQuery);
        return { data: savedQuery as unknown as TData };
      }

      throw new ApiError({ code: "not_supported", message: `mock 暂不支持创建资源：${params.resource}`, status: 400 });
    },

    async update<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: UpdateParams<TVariables>,
    ): Promise<UpdateResponse<TData>> {
      await wait();
      const storeKey = RESOURCE_TO_STORE[params.resource];
      if (!storeKey) throw new ApiError({ code: "unknown_resource", message: `未知资源：${params.resource}`, status: 400 });
      const item = findOrThrow(db[storeKey] as Array<{ id: string }>, String(params.id), params.resource) as Record<string, unknown>;
      Object.assign(item, asRecord(params.variables), { updatedAt: now() });
      return { data: item as unknown as TData };
    },

    async deleteOne<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: DeleteOneParams<TVariables>,
    ): Promise<DeleteOneResponse<TData>> {
      await wait();
      const storeKey = RESOURCE_TO_STORE[params.resource];
      if (!storeKey) throw new ApiError({ code: "unknown_resource", message: `未知资源：${params.resource}`, status: 400 });
      const items = db[storeKey] as Array<{ id: string }>;
      const index = items.findIndex((item) => item.id === String(params.id));
      if (index < 0) throw new ApiError({ code: "not_found", message: `资源不存在：${params.id}`, status: 404 });
      const [removed] = items.splice(index, 1);
      return { data: removed as unknown as TData };
    },

    async custom<TData extends { [key: string]: any } = { [key: string]: any }, TQuery = unknown, TPayload = unknown>(
      params: CustomParams<TQuery, TPayload>,
    ): Promise<CustomResponse<TData>> {
      await wait();
      const url = params.url.replace(/^\/+/, "");

      const previewMatch = /^datasets\/([^/]+)\/preview$/.exec(url);
      if (previewMatch && params.method === "get") {
        const datasetId = decodeURIComponent(previewMatch[1] ?? "");
        findOrThrow(db.datasets, datasetId, "数据集");
        const preview = db.datasetPreviews[datasetId];
        if (!preview) throw new ApiError({ code: "not_found", message: `数据集暂无预览：${datasetId}`, status: 404 });
        return { data: preview as unknown as TData };
      }

      if (url === "queries/execute" && params.method === "post") {
        const payload = asRecord(params.payload);
        const result = buildQueryResult(String(payload.sql ?? ""));
        return { data: result as unknown as TData };
      }

      if (url === "queries/explain" && params.method === "post") {
        const payload = asRecord(params.payload);
        const sql = String(payload.sql ?? "");
        if (!sql.trim()) throw new ApiError({ code: "empty_query", message: "SQL 不能为空", status: 422 });
        return {
          data: {
            plan: [
              "Limit",
              "  -> Scan orders",
              "  -> Filter (amount > 100000)",
              "  -> Sort (amount DESC)",
            ].join("\n"),
          } as unknown as TData,
        };
      }

      throw new ApiError({ code: "not_found", message: `mock 未实现该请求：${params.method.toUpperCase()} ${url}`, status: 404 });
    },
  };

  return provider;
}

/** 仅用于测试：验证数据集预览结构。 */
export function getMockPreview(db: MockDatabase, datasetId: string): DatasetPreview | undefined {
  return db.datasetPreviews[datasetId];
}

/** 便于测试构造空库。 */
export function createEmptyMockDatabase(): MockDatabase {
  return {
    projects: [],
    objectTypes: [],
    linkTypes: [],
    objects: [],
    datasets: [],
    datasetPreviews: {},
    savedQueries: [],
  };
}

export type { LinkType };
