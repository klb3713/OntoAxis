import type { ApiErrorPayload } from "./types";

export type ApiErrorInit = {
  code: string;
  message: string;
  status?: number;
  metadata?: Record<string, unknown>;
  retryable?: boolean;
  requestId?: string;
  cause?: unknown;
};

/**
 * 所有数据访问失败都归一为该错误类型，页面只关心 code/message/retryable。
 * 与 ARCHITECTURE 16.5 的 ErrorDetail 形状保持一致。
 */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly statusCode: number;
  readonly metadata: Record<string, unknown>;
  readonly retryable: boolean;
  readonly requestId?: string;

  constructor(init: ApiErrorInit) {
    super(init.message);
    this.name = "ApiError";
    this.code = init.code;
    this.status = init.status ?? 0;
    this.statusCode = init.status ?? 0;
    this.metadata = init.metadata ?? {};
    this.retryable = init.retryable ?? false;
    this.requestId = init.requestId;
    if (init.cause !== undefined) {
      this.cause = init.cause;
    }
  }

  static fromResponse(status: number, payload: ApiErrorPayload | undefined, headerRequestId?: string): ApiError {
    const detail = payload?.error;
    const message = detail?.message ?? payload?.message ?? `请求失败（HTTP ${status}）`;
    return new ApiError({
      code: detail?.code ?? payload?.code ?? `http_${status}`,
      message,
      status,
      metadata: detail?.metadata,
      retryable: detail?.retryable ?? (status >= 500 || status === 429),
      requestId: detail?.request_id ?? payload?.request_id ?? headerRequestId,
    });
  }

  static network(message = "无法连接 API，请检查网络或服务状态", cause?: unknown): ApiError {
    return new ApiError({
      code: "network_error",
      message,
      status: 0,
      retryable: true,
      cause,
    });
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export function toErrorMessage(error: unknown, fallback = "操作失败，请稍后重试"): string {
  if (isApiError(error)) return error.message;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}
