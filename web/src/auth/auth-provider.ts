import type { AuthProvider } from "@refinedev/core";
import { isApiError } from "../api/errors";
import type { AuthAdapter } from "./types";

/**
 * 将认证适配器桥接为 Refine AuthProvider。
 * Refine 只负责路由守卫和身份展示，真正的登录流程仍由 AuthAdapter 统一处理。
 */
export function createAuthProvider(adapter: AuthAdapter): AuthProvider {
  return {
    async login(params: { email?: string; password?: string; displayName?: string }): Promise<{ success: boolean; redirectTo: string }> {
      await adapter.login({
        email: params?.email ?? "",
        password: params?.password,
        displayName: params?.displayName,
      });
      return { success: true, redirectTo: "/projects" };
    },

    async logout(): Promise<{ success: boolean; redirectTo: string }> {
      await adapter.logout();
      return { success: true, redirectTo: "/login" };
    },

    async check(): Promise<{ authenticated: boolean; redirectTo?: string }> {
      const session = await adapter.getSession();
      if (!session) return { authenticated: false, redirectTo: "/login" };
      return { authenticated: true };
    },

    async onError(error: unknown): Promise<{ logout?: boolean; redirectTo?: string; error?: Error }> {
      if (isApiError(error) && error.status === 401) {
        return { logout: true, redirectTo: "/login", error };
      }
      return { error: error instanceof Error ? error : new Error("请求失败") };
    },

    async getIdentity(): Promise<{ id: string; name: string; email: string; roles: string[] } | null> {
      const session = await adapter.getSession();
      if (!session) return null;
      return {
        id: session.user.id,
        name: session.user.displayName,
        email: session.user.email,
        roles: session.user.roles,
      };
    },

    async getPermissions(): Promise<string[]> {
      const session = await adapter.getSession();
      return session?.user.roles ?? [];
    },
  };
}
