import type { ListPayload } from "./types";

type UnknownRecord = Record<string, unknown>;

function isPlainObject(value: unknown): value is UnknownRecord {
  return isRecord(value) && !Array.isArray(value);
}

function toCamelKey(key: string): string {
  return key.replace(/_([a-z0-9])/g, (_, ch: string) => ch.toUpperCase());
}

function toSnakeKey(key: string): string {
  return key.replace(/[A-Z]/g, (ch) => `_${ch.toLowerCase()}`);
}

/** Convert wire (snake_case) objects to the lowerCamelCase view models used by pages. */
export function camelizeKeys<T>(value: T, preserveKeys: string[] = []): T {
  if (Array.isArray(value)) return value.map((item) => camelizeKeys(item, preserveKeys)) as T;
  if (!isPlainObject(value)) return value;
  const out: UnknownRecord = {};
  for (const [key, child] of Object.entries(value)) {
    const converted = preserveKeys.includes(key) ? child : camelizeKeys(child, preserveKeys);
    out[toCamelKey(key)] = converted;
  }
  return out as T;
}

/** Convert page view models (lowerCamelCase) to the snake_case wire contract. */
export function snakeizeKeys<T>(value: T, preserveKeys: string[] = []): T {
  if (Array.isArray(value)) return value.map((item) => snakeizeKeys(item, preserveKeys)) as T;
  if (!isPlainObject(value)) return value;
  const out: UnknownRecord = {};
  for (const [key, child] of Object.entries(value)) {
    const converted = preserveKeys.includes(key) ? child : snakeizeKeys(child, preserveKeys);
    out[toSnakeKey(key)] = converted;
  }
  return out as T;
}

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readNumber(source: UnknownRecord | undefined, keys: string[]): number | undefined {
  if (!source) return undefined;
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "number" && Number.isFinite(value)) return value;
    if (typeof value === "string" && value.trim() !== "" && Number.isFinite(Number(value))) {
      return Number(value);
    }
  }
  return undefined;
}

function firstArray(source: UnknownRecord, keys: string[]): unknown[] | undefined {
  const candidates: unknown[] = [source.data, source.items, source.results];
  for (const key of keys) candidates.push(source[key]);
  return candidates.find((candidate): candidate is unknown[] => Array.isArray(candidate));
}

/**
 * 列表响应归一化：兼容 `data`/`items`/<resource 名> 数组与 `meta`/`pagination`/`page` 分页信息。
 * 该适配只发生在 API 客户端边界，页面组件不感知后端键名差异。
 */
export function normalizeList<T>(payload: unknown, resourceKeys: string[] = [], preserveKeys: string[] = []): ListPayload<T> {
  if (Array.isArray(payload)) {
    return {
      data: payload as T[],
      meta: { page: 1, perPage: payload.length, total: payload.length, totalPages: 1 },
    };
  }

  if (!isRecord(payload)) {
    throw new TypeError("列表响应必须是数组或对象");
  }

  const data = firstArray(payload, resourceKeys);
  if (!data) {
    throw new TypeError(`列表响应缺少数组字段（期望 data/items/${resourceKeys.join("/")}）`);
  }

  const metaSource = [payload.meta, payload.pagination, payload.pageResponse, payload.page_info, payload.page].find(
    isRecord,
  );
  const page = readNumber(metaSource, ["page", "currentPage", "current_page"]) ?? readNumber(payload, ["page"]) ?? 1;
  const total = readNumber(metaSource, ["total", "totalCount", "total_count"]) ?? readNumber(payload, ["total"]) ?? data.length;
  const perPage =
    readNumber(metaSource, ["perPage", "per_page", "pageSize", "page_size"]) ??
    readNumber(payload, ["perPage", "per_page"]) ??
    (data.length || 50);
  const totalPages =
    readNumber(metaSource, ["totalPages", "total_pages"]) ??
    readNumber(payload, ["totalPages", "total_pages"]) ??
    (perPage > 0 ? Math.max(1, Math.ceil(total / perPage)) : 1);

  return {
    data: camelizeKeys(data, preserveKeys) as T[],
    meta: { page, perPage, total, totalPages },
  };
}

/** 单资源响应归一化：兼容直接对象、`data`、`item`、`<resource>` 包裹。 */
export function normalizeEntity<T>(payload: unknown, resourceKeys: string[] = [], preserveKeys: string[] = []): T {
  if (!isRecord(payload)) {
    throw new TypeError("资源响应必须是对象");
  }

  if (isRecord(payload.data)) return camelizeKeys(payload.data, preserveKeys) as T;
  if (isRecord(payload.item)) return camelizeKeys(payload.item, preserveKeys) as T;
  for (const key of resourceKeys) {
    if (isRecord(payload[key])) return camelizeKeys(payload[key], preserveKeys) as T;
  }
  if (isRecord(payload.result)) return camelizeKeys(payload.result, preserveKeys) as T;
  return camelizeKeys(payload, preserveKeys) as T;
}

/** 读取 custom 响应中的业务数据（兼容 `{data: ...}` 包裹）。 */
export function normalizeCustom<T>(payload: unknown): T {
  if (isRecord(payload) && "data" in payload && Object.keys(payload).length <= 3) {
    return payload.data as T;
  }
  return payload as T;
}
