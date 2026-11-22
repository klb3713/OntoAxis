import { ApiError } from "../api/errors";
import { clearSession, loadSession, saveSession } from "./session";
import type { AuthAdapter, AuthSession, LoginCredentials } from "./types";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function buildMockSession(credentials: LoginCredentials): AuthSession {
  const email = credentials.email.trim().toLowerCase();
  return {
    user: {
      id: "usr_mock_001",
      email,
      displayName: credentials.displayName?.trim() || email.split("@")[0] || "Mock 用户",
      organizationId: "org_01",
      tenantId: "tenant_demo",
      roles: ["platform_admin", "ontology_editor"],
      locale: "zh-CN",
    },
    accessToken: `mock.${btoa(JSON.stringify({ sub: "usr_mock_001", email, iat: Date.now() }))}`,
  };
}

/**
 * 仅本地开发/测试使用的 mock 认证。
 * 它不校验密码、不连接身份提供方，绝不用于生产环境。
 */
export function createMockAuthAdapter(): AuthAdapter {
  return {
    mode: "mock",

    async login(credentials: LoginCredentials): Promise<AuthSession> {
      const email = credentials.email?.trim() ?? "";
      if (!EMAIL_PATTERN.test(email)) {
        throw new ApiError({ code: "invalid_email", message: "请输入有效的邮箱地址", status: 422 });
      }
      if (!credentials.password) {
        throw new ApiError({ code: "missing_password", message: "请输入密码", status: 422 });
      }
      const session = buildMockSession(credentials);
      saveSession(session);
      return session;
    },

    async logout(): Promise<void> {
      clearSession();
    },

    async getSession(): Promise<AuthSession | null> {
      return loadSession();
    },

    async getAccessToken(): Promise<string | null> {
      return loadSession()?.accessToken ?? null;
    },
  };
}
