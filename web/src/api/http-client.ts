import { ApiError } from "./errors";
import type { ApiErrorPayload } from "./types";

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export type RequestOptions = {
  method?: HttpMethod;
  query?: Record<string, unknown>;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

export type HttpClientOptions = {
  baseUrl: string;
  getAccessToken?: () => string | null | Promise<string | null>;
  fetchImpl?: typeof fetch;
  defaultHeaders?: Record<string, string>;
};

function buildUrl(baseUrl: string, path: string, query?: Record<string, unknown>): string {
  const base = baseUrl.replace(/\/+$/, "");
  const suffix = path.startsWith("/") ? path : `/${path}`;
  const url = `${base}${suffix}`;
  if (!query) return url;

  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    if (Array.isArray(value)) {
      for (const item of value) search.append(key, String(item));
    } else {
      search.set(key, String(value));
    }
  }
  const queryString = search.toString();
  return queryString ? `${url}?${queryString}` : url;
}

function parseBody(text: string): unknown {
  if (!text) return undefined;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return { message: text };
  }
}

/**
 * 轻量 typed HTTP 客户端。统一负责：
 * - `VITE_API_BASE_URL` 基地址拼接；
 * - Bearer token 注入（由 auth adapter 统一刷新）；
 * - 错误响应到 `ApiError` 的归一化；
 * - 204/空响应处理。
 */
export class HttpClient {
  readonly baseUrl: string;
  private readonly getAccessToken?: () => string | null | Promise<string | null>;
  private readonly fetchImpl: typeof fetch;
  private readonly defaultHeaders: Record<string, string>;

  constructor(options: HttpClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, "");
    this.getAccessToken = options.getAccessToken;
    this.fetchImpl = options.fetchImpl ?? globalThis.fetch.bind(globalThis);
    this.defaultHeaders = options.defaultHeaders ?? {};
  }

  buildUrl(path: string, query?: Record<string, unknown>): string {
    return buildUrl(this.baseUrl, path, query);
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = options.method ?? "GET";
    const headers = new Headers({ Accept: "application/json", ...this.defaultHeaders, ...options.headers });

    if (options.body !== undefined && !(options.body instanceof FormData)) {
      headers.set("Content-Type", "application/json");
    }

    const token = await this.getAccessToken?.();
    if (token) headers.set("Authorization", `Bearer ${token}`);

    let response: Response;
    try {
      response = await this.fetchImpl(this.buildUrl(path, options.query), {
        method,
        headers,
        body: options.body === undefined ? undefined : options.body instanceof FormData ? options.body : JSON.stringify(options.body),
        signal: options.signal,
        credentials: "same-origin",
      });
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") throw error;
      throw ApiError.network(undefined, error);
    }

    const text = await response.text();
    const payload = parseBody(text);

    if (!response.ok) {
      throw ApiError.fromResponse(response.status, payload as ApiErrorPayload | undefined, response.headers.get("x-request-id") ?? undefined);
    }

    return payload as T;
  }

  get<T>(path: string, query?: Record<string, unknown>, signal?: AbortSignal): Promise<T> {
    return this.request<T>(path, { method: "GET", query, signal });
  }

  post<T>(path: string, body?: unknown, query?: Record<string, unknown>): Promise<T> {
    return this.request<T>(path, { method: "POST", body, query });
  }

  put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "PUT", body });
  }

  patch<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "PATCH", body });
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "DELETE" });
  }
}
