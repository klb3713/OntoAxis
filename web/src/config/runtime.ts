export type ApiMode = "mock" | "http";
export type AuthMode = "mock" | "oidc";
export type SupportedLocale = "zh-CN" | "en";

export type OidcSettings = {
  authority: string;
  clientId: string;
  redirectUri: string;
  scope: string;
};

export type RuntimeConfig = {
  apiMode: ApiMode;
  apiBaseUrl: string;
  authMode: AuthMode;
  oidc: OidcSettings | null;
  defaultLocale: SupportedLocale;
  appName: string;
};

export type RuntimeEnv = Record<string, string | boolean | undefined>;

function readString(env: RuntimeEnv, key: string): string | undefined {
  const value = env[key];
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}

function readMode<T extends string>(value: string | undefined, allowed: readonly T[], fallback: T, key: string): T {
  if (value === undefined) return fallback;
  if ((allowed as readonly string[]).includes(value)) return value as T;
  throw new Error(`${key} 取值非法：${value}（允许：${allowed.join(" | ")}）`);
}

/**
 * 从 Vite 环境变量读取运行配置。
 *
 * 默认值保持“本地可跑、生产显式配置”的 fail-closed 姿态：
 * - 开发环境未配置时使用 mock API + mock 认证；
 * - 生产构建未配置时回退到真实 `/api/v1` + OIDC，避免生产误用 mock。
 */
export function readRuntimeConfig(env: RuntimeEnv, isDev: boolean): RuntimeConfig {
  const apiMode = readMode(readString(env, "VITE_API_MODE"), ["mock", "http"] as const, isDev ? "mock" : "http", "VITE_API_MODE");
  const authMode = readMode(readString(env, "VITE_AUTH_MODE"), ["mock", "oidc"] as const, isDev ? "mock" : "oidc", "VITE_AUTH_MODE");

  const apiBaseUrl = (readString(env, "VITE_API_BASE_URL") ?? "/api/v1").replace(/\/+$/, "");
  const defaultLocale = readMode(
    readString(env, "VITE_DEFAULT_LOCALE"),
    ["zh-CN", "en"] as const,
    "zh-CN",
    "VITE_DEFAULT_LOCALE",
  );

  let oidc: OidcSettings | null = null;
  if (authMode === "oidc") {
    const authority = readString(env, "VITE_OIDC_AUTHORITY");
    const clientId = readString(env, "VITE_OIDC_CLIENT_ID");
    const redirectUri = readString(env, "VITE_OIDC_REDIRECT_URI") ?? `${globalThis.location?.origin ?? ""}/auth/callback`;
    if (!authority || !clientId) {
      throw new Error("VITE_AUTH_MODE=oidc 时必须配置 VITE_OIDC_AUTHORITY 与 VITE_OIDC_CLIENT_ID");
    }
    oidc = {
      authority,
      clientId,
      redirectUri,
      scope: readString(env, "VITE_OIDC_SCOPES") ?? "openid profile email",
    };
  }

  return {
    apiMode,
    apiBaseUrl,
    authMode,
    oidc,
    defaultLocale,
    appName: readString(env, "VITE_APP_NAME") ?? "OntoAxis 元轴",
  };
}

let cached: RuntimeConfig | null = null;

export function getRuntimeConfig(): RuntimeConfig {
  if (!cached) {
    cached = readRuntimeConfig(import.meta.env as RuntimeEnv, Boolean(import.meta.env.DEV));
  }
  return cached;
}
