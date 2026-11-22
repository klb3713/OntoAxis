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
import { HttpClient } from "./http-client";
import { camelizeKeys, normalizeCustom, normalizeEntity, normalizeList, snakeizeKeys } from "./normalize";

/** REST 路径映射：proto 未带 google.api.http 注解，本适配层是唯一的路径事实来源。 */
export const RESOURCE_PATHS: Record<string, string> = {
  projects: "/tenancy/projects",
  properties: "/ontology/properties",
  "object-types": "/ontology/object-types",
  "link-types": "/ontology/link-types",
  objects: "/objects",
  datasets: "/datasets",
  "saved-queries": "/query/saved-queries",
};

const ENTITY_KEYS: Record<string, string[]> = {
  projects: ["project"],
  properties: ["property"],
  "object-types": ["object_type", "objectType"],
  "link-types": ["link_type", "linkType"],
  objects: ["object"],
  datasets: ["dataset"],
  "saved-queries": ["saved_query", "savedQuery"],
};

function resourcePath(resource: string): string {
  const path = RESOURCE_PATHS[resource];
  if (!path) throw new ApiError({ code: "unknown_resource", message: `未知资源：${resource}`, status: 400 });
  return path;
}

function idempotencyKey(): string {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi && typeof cryptoApi.randomUUID === "function") return cryptoApi.randomUUID();
  return `idem-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function filtersToQuery(params: GetListParams): Record<string, unknown> {
  const query: Record<string, unknown> = {
    page: params.pagination?.currentPage ?? 1,
    per_page: params.pagination?.pageSize ?? 50,
  };

  for (const filter of params.filters ?? []) {
    if (!("field" in filter)) {
      throw new ApiError({ code: "unsupported_filter", message: "暂不支持嵌套逻辑过滤器", status: 400 });
    }
    if (filter.operator !== "eq" && filter.operator !== "contains") {
      throw new ApiError({ code: "unsupported_filter", message: `暂不支持过滤器操作符：${filter.operator}`, status: 400 });
    }
    query[filter.field === "q" ? "q" : filter.field] = filter.value;
  }

  const sorter = params.sorters?.[0];
  if (sorter) {
    query.sort = sorter.field;
    query.order = sorter.order;
  }

  return query;
}

function preserveKeys(resource: string): string[] {
  // Object records hold user-defined property keys; never rewrite those keys.
  return resource === "objects" ? ["properties"] : [];
}

export type RestDataProviderOptions = {
  baseUrl: string;
  getAccessToken?: () => string | null | Promise<string | null>;
  fetchImpl?: typeof fetch;
};

/**
 * Refine DataProvider 的 REST 实现。
 *
 * 页面通过 `useList`/`useCreate` 等 Refine hooks 访问资源；本适配器负责
 * HTTP 路径、分页参数、错误形状与响应包裹的转换。
 */
export function createRestDataProvider(options: RestDataProviderOptions): DataProvider {
  const http = new HttpClient({
    baseUrl: options.baseUrl,
    getAccessToken: options.getAccessToken,
    fetchImpl: options.fetchImpl,
  });

  return {
    getApiUrl: () => http.baseUrl,

    async getList<TData extends { [key: string]: any } = { [key: string]: any }>(
      params: GetListParams,
    ): Promise<GetListResponse<TData>> {
      const payload = await http.get<unknown>(resourcePath(params.resource), filtersToQuery(params));
      const normalized = normalizeList<TData>(payload, [params.resource.replace(/-/g, "_"), params.resource], preserveKeys(params.resource));
      return { data: normalized.data, total: normalized.meta.total };
    },

    async getOne<TData extends { [key: string]: any } = { [key: string]: any }>(
      params: GetOneParams,
    ): Promise<GetOneResponse<TData>> {
      const payload = await http.get<unknown>(`${resourcePath(params.resource)}/${encodeURIComponent(String(params.id))}`);
      return { data: normalizeEntity<TData>(payload, ENTITY_KEYS[params.resource] ?? [], preserveKeys(params.resource)) };
    },

    async create<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: CreateParams<TVariables>,
    ): Promise<CreateResponse<TData>> {
      const payload = await http.request<unknown>(resourcePath(params.resource), {
        method: "POST",
        body: snakeizeKeys(params.variables, preserveKeys(params.resource)),
        headers: { "Idempotency-Key": idempotencyKey() },
      });
      return { data: normalizeEntity<TData>(payload, ENTITY_KEYS[params.resource] ?? [], preserveKeys(params.resource)) };
    },

    async update<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: UpdateParams<TVariables>,
    ): Promise<UpdateResponse<TData>> {
      const payload = await http.request<unknown>(`${resourcePath(params.resource)}/${encodeURIComponent(String(params.id))}`, {
        method: "PATCH",
        body: snakeizeKeys(params.variables, preserveKeys(params.resource)),
        headers: { "Idempotency-Key": idempotencyKey() },
      });
      return { data: normalizeEntity<TData>(payload, ENTITY_KEYS[params.resource] ?? [], preserveKeys(params.resource)) };
    },

    async deleteOne<TData extends { [key: string]: any } = { [key: string]: any }, TVariables = {}>(
      params: DeleteOneParams<TVariables>,
    ): Promise<DeleteOneResponse<TData>> {
      const payload = await http.request<unknown>(`${resourcePath(params.resource)}/${encodeURIComponent(String(params.id))}`, {
        method: "DELETE",
        headers: { "Idempotency-Key": idempotencyKey() },
      });
      if (payload === undefined || payload === null) {
        return { data: { id: params.id } as unknown as TData };
      }
      return { data: normalizeEntity<TData>(payload, ENTITY_KEYS[params.resource] ?? [], preserveKeys(params.resource)) };
    },

    async custom<TData extends { [key: string]: any } = { [key: string]: any }, TQuery = unknown, TPayload = unknown>(
      params: CustomParams<TQuery, TPayload>,
    ): Promise<CustomResponse<TData>> {
      const path = params.url.startsWith("/") ? params.url : `/${params.url}`;
      const payload = await http.request<unknown>(path, {
        method: params.method.toUpperCase() as "GET" | "POST" | "PUT" | "PATCH" | "DELETE",
        body: params.payload,
        query: params.query as Record<string, unknown> | undefined,
        headers: params.method === "post" || params.method === "patch" || params.method === "put"
          ? { "Idempotency-Key": idempotencyKey() }
          : undefined,
      });
      return { data: camelizeKeys(normalizeCustom<TData>(payload)) };
    },
  };
}
