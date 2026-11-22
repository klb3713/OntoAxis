import { describe, expect, it } from "vitest";
import { readRuntimeConfig } from "./runtime";

describe("readRuntimeConfig", () => {
  it("开发环境默认 mock API + mock 认证 + zh-CN", () => {
    const config = readRuntimeConfig({}, true);
    expect(config.apiMode).toBe("mock");
    expect(config.authMode).toBe("mock");
    expect(config.apiBaseUrl).toBe("/api/v1");
    expect(config.defaultLocale).toBe("zh-CN");
    expect(config.oidc).toBeNull();
  });

  it("生产环境默认 fail-closed 到真实 API 与 OIDC，但缺少 OIDC 配置时拒绝启动", () => {
    expect(() => readRuntimeConfig({}, false)).toThrow(/VITE_OIDC_AUTHORITY/);
  });

  it("完整 OIDC 配置可用，并去掉 base URL 末尾斜杠", () => {
    const config = readRuntimeConfig(
      {
        VITE_API_MODE: "http",
        VITE_API_BASE_URL: "https://api.example.com/api/v1///",
        VITE_AUTH_MODE: "oidc",
        VITE_OIDC_AUTHORITY: "https://keycloak.example.com/realms/ontoaxis",
        VITE_OIDC_CLIENT_ID: "ontoaxis-web",
        VITE_OIDC_REDIRECT_URI: "https://app.example.com/auth/callback",
      },
      false,
    );
    expect(config.apiBaseUrl).toBe("https://api.example.com/api/v1");
    expect(config.authMode).toBe("oidc");
    expect(config.oidc?.clientId).toBe("ontoaxis-web");
    expect(config.oidc?.scope).toBe("openid profile email");
  });

  it("非法 mode 直接拒绝，避免静默降级", () => {
    expect(() => readRuntimeConfig({ VITE_API_MODE: "fixture" }, true)).toThrow(/VITE_API_MODE 取值非法/);
    expect(() => readRuntimeConfig({ VITE_AUTH_MODE: "none" }, true)).toThrow(/VITE_AUTH_MODE 取值非法/);
  });
});
