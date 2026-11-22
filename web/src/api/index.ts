import type { DataProvider } from "@refinedev/core";
import type { AuthAdapter } from "../auth/types";
import type { RuntimeConfig } from "../config/runtime";
import { createMockDataProvider } from "./mock-data-provider";
import { createRestDataProvider } from "./rest-data-provider";

export type ApiRuntime = {
  dataProvider: DataProvider;
  mode: RuntimeConfig["apiMode"];
};

/**
 * API 运行时选择器：mock 与 http 都实现同一 Refine DataProvider 契约。
 * 页面不感知当前模式，切换仅由 `VITE_API_MODE` 决定。
 */
export function createApiRuntime(config: RuntimeConfig, authAdapter: AuthAdapter): ApiRuntime {
  if (config.apiMode === "mock") {
    return { dataProvider: createMockDataProvider(), mode: "mock" };
  }
  return {
    dataProvider: createRestDataProvider({
      baseUrl: config.apiBaseUrl,
      getAccessToken: () => authAdapter.getAccessToken(),
    }),
    mode: "http",
  };
}
