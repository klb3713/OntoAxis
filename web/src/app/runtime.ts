import type { AuthProvider, DataProvider } from "@refinedev/core";
import type { ApiRuntime } from "../api";
import { createApiRuntime } from "../api";
import { createAuthAdapter } from "../auth";
import { createAuthProvider } from "../auth/auth-provider";
import type { AuthAdapter } from "../auth/types";
import { getRuntimeConfig, type RuntimeConfig } from "../config/runtime";

export type AppRuntime = {
  config: RuntimeConfig;
  api: ApiRuntime;
  authAdapter: AuthAdapter;
  authProvider: AuthProvider;
  dataProvider: DataProvider;
};

/** 组装应用运行时。测试可传入显式配置，避免读取构建期环境。 */
export function createAppRuntime(config: RuntimeConfig = getRuntimeConfig()): AppRuntime {
  const authAdapter = createAuthAdapter(config);
  const api = createApiRuntime(config, authAdapter);
  return {
    config,
    api,
    authAdapter,
    authProvider: createAuthProvider(authAdapter),
    dataProvider: api.dataProvider,
  };
}
