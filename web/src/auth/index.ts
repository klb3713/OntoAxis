import type { RuntimeConfig } from "../config/runtime";
import { createMockAuthAdapter } from "./mock-auth";
import { createOidcAuthAdapter } from "./oidc";
import type { AuthAdapter } from "./types";

/** 根据运行配置创建唯一认证适配器；OIDC 配置缺失时由 runtime config 提前拒绝。 */
export function createAuthAdapter(config: RuntimeConfig): AuthAdapter {
  if (config.authMode === "mock") return createMockAuthAdapter();
  if (!config.oidc) {
    throw new Error("VITE_AUTH_MODE=oidc 缺少 OIDC 配置");
  }
  return createOidcAuthAdapter(config.oidc);
}
