import { createContext, useContext } from "react";
import type { RuntimeConfig } from "../config/runtime";
import type { AuthAdapter } from "./types";

export type AuthRuntimeValue = {
  config: RuntimeConfig;
  authAdapter: AuthAdapter;
};

export const AuthRuntimeContext = createContext<AuthRuntimeValue | null>(null);

export function useAuthRuntime(): AuthRuntimeValue {
  const value = useContext(AuthRuntimeContext);
  if (!value) throw new Error("useAuthRuntime 必须在 AuthRuntimeContext.Provider 内使用");
  return value;
}
