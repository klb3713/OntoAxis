import { describe, expect, it } from "vitest";
import { ApiError } from "./errors";

describe("ApiError", () => {
  it("将统一错误体映射为稳定字段", () => {
    const error = ApiError.fromResponse(
      409,
      {
        error: {
          code: "conflict",
          message: "项目标识已存在",
          metadata: { field: "name" },
          retryable: false,
          request_id: "req_123",
        },
      },
      "req_header",
    );

    expect(error).toBeInstanceOf(ApiError);
    expect(error.code).toBe("conflict");
    expect(error.message).toBe("项目标识已存在");
    expect(error.status).toBe(409);
    expect(error.retryable).toBe(false);
    expect(error.requestId).toBe("req_123");
    expect(error.metadata).toEqual({ field: "name" });
  });

  it("5xx 与 429 默认标记为可重试", () => {
    expect(ApiError.fromResponse(500, undefined).retryable).toBe(true);
    expect(ApiError.fromResponse(429, undefined).retryable).toBe(true);
    expect(ApiError.fromResponse(400, undefined).retryable).toBe(false);
  });

  it("无错误体时生成可读消息与兜底 code", () => {
    const error = ApiError.fromResponse(503, undefined);
    expect(error.code).toBe("http_503");
    expect(error.message).toContain("503");
  });

  it("网络错误可重试且 status 为 0", () => {
    const error = ApiError.network();
    expect(error.code).toBe("network_error");
    expect(error.status).toBe(0);
    expect(error.retryable).toBe(true);
  });
});
