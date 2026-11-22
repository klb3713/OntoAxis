import { ApiError } from "../api/errors";
import type { ApiErrorPayload } from "../api/types";
import type { OidcSettings } from "../config/runtime";
import { clearSession, loadSession, saveSession } from "./session";
import type { AuthAdapter, AuthSession, AuthUser } from "./types";

const TRANSACTION_KEY = "ontoaxis.web.oidc.transaction";
const EXPIRY_SKEW_MS = 60_000;

type OidcTransaction = {
  state: string;
  nonce: string;
  codeVerifier: string;
  redirectUri: string;
  returnTo: string;
};

type JwtClaims = Record<string, unknown> & {
  sub?: string;
  email?: string;
  name?: string;
  preferred_username?: string;
  roles?: unknown;
  realm_access?: { roles?: unknown };
  organization_id?: string;
  tenant_id?: string;
  locale?: string;
};

type TokenResponse = {
  access_token?: string;
  refresh_token?: string;
  id_token?: string;
  expires_in?: number;
  error?: string;
  error_description?: string;
};

export function base64UrlEncode(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

export function base64UrlDecode(value: string): string {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "=".repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

export function generateRandomString(byteLength = 32): string {
  const bytes = new Uint8Array(byteLength);
  globalThis.crypto.getRandomValues(bytes);
  return base64UrlEncode(bytes);
}

export async function sha256Base64Url(value: string): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return base64UrlEncode(new Uint8Array(digest));
}

export async function createPkcePair(): Promise<{ codeVerifier: string; codeChallenge: string }> {
  const codeVerifier = generateRandomString(48);
  const codeChallenge = await sha256Base64Url(codeVerifier);
  return { codeVerifier, codeChallenge };
}

export function buildAuthorizeUrl(
  settings: OidcSettings,
  params: { state: string; nonce: string; codeChallenge: string; redirectUri?: string },
): string {
  const authority = settings.authority.replace(/\/+$/, "");
  const url = new URL(`${authority}/protocol/openid-connect/auth`);
  url.searchParams.set("response_type", "code");
  url.searchParams.set("client_id", settings.clientId);
  url.searchParams.set("redirect_uri", params.redirectUri ?? settings.redirectUri);
  url.searchParams.set("scope", settings.scope);
  url.searchParams.set("state", params.state);
  url.searchParams.set("nonce", params.nonce);
  url.searchParams.set("code_challenge", params.codeChallenge);
  url.searchParams.set("code_challenge_method", "S256");
  return url.toString();
}

export function buildLogoutUrl(settings: OidcSettings): string {
  const authority = settings.authority.replace(/\/+$/, "");
  const url = new URL(`${authority}/protocol/openid-connect/logout`);
  url.searchParams.set("client_id", settings.clientId);
  url.searchParams.set("post_logout_redirect_uri", new URL("/login", settings.redirectUri).toString());
  return url.toString();
}

export function readOidcTransaction(storage: Storage = window.sessionStorage): OidcTransaction | null {
  const raw = storage.getItem(TRANSACTION_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<OidcTransaction>;
    if (
      typeof parsed.state === "string" &&
      typeof parsed.nonce === "string" &&
      typeof parsed.codeVerifier === "string" &&
      typeof parsed.redirectUri === "string" &&
      typeof parsed.returnTo === "string"
    ) {
      return parsed as OidcTransaction;
    }
  } catch {
    // fall through
  }
  storage.removeItem(TRANSACTION_KEY);
  return null;
}

export function clearOidcTransaction(storage: Storage = window.sessionStorage): void {
  storage.removeItem(TRANSACTION_KEY);
}

export async function startOidcLogin(settings: OidcSettings, returnTo = "/projects"): Promise<void> {
  const { codeVerifier, codeChallenge } = await createPkcePair();
  const transaction: OidcTransaction = {
    state: generateRandomString(24),
    nonce: generateRandomString(24),
    codeVerifier,
    redirectUri: settings.redirectUri,
    returnTo,
  };
  window.sessionStorage.setItem(TRANSACTION_KEY, JSON.stringify(transaction));
  const authorizeUrl = buildAuthorizeUrl(settings, {
    state: transaction.state,
    nonce: transaction.nonce,
    codeChallenge,
    redirectUri: transaction.redirectUri,
  });
  window.location.assign(authorizeUrl);
}

export function decodeJwtPayload(token: string): JwtClaims {
  const parts = token.split(".");
  if (parts.length < 2) throw new ApiError({ code: "invalid_token", message: "ID Token 格式非法", status: 400 });
  try {
    return JSON.parse(base64UrlDecode(parts[1] ?? "")) as JwtClaims;
  } catch {
    throw new ApiError({ code: "invalid_token", message: "无法解析 ID Token", status: 400 });
  }
}

function readRoles(claims: JwtClaims): string[] {
  const direct = Array.isArray(claims.roles) ? claims.roles : [];
  const realm = claims.realm_access && Array.isArray(claims.realm_access.roles) ? claims.realm_access.roles : [];
  return [...new Set([...direct, ...realm].filter((role): role is string => typeof role === "string"))];
}

export function claimsToUser(claims: JwtClaims): AuthUser {
  if (!claims.sub) {
    throw new ApiError({ code: "invalid_identity", message: "身份提供方未返回 subject", status: 401 });
  }
  return {
    id: claims.sub,
    email: typeof claims.email === "string" ? claims.email : "",
    displayName:
      typeof claims.name === "string"
        ? claims.name
        : typeof claims.preferred_username === "string"
          ? claims.preferred_username
          : claims.sub,
    organizationId: typeof claims.organization_id === "string" ? claims.organization_id : "",
    tenantId: typeof claims.tenant_id === "string" ? claims.tenant_id : "",
    roles: readRoles(claims),
    locale: typeof claims.locale === "string" ? claims.locale : undefined,
  };
}

async function postToken(
  settings: OidcSettings,
  body: URLSearchParams,
  fetchImpl: typeof fetch,
): Promise<TokenResponse> {
  const authority = settings.authority.replace(/\/+$/, "");
  let response: Response;
  try {
    response = await fetchImpl(`${authority}/protocol/openid-connect/token`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded", Accept: "application/json" },
      body,
    });
  } catch (error) {
    throw ApiError.network("无法连接身份提供方", error);
  }
  const text = await response.text();
  let payload: TokenResponse = {};
  try {
    payload = text ? (JSON.parse(text) as TokenResponse) : {};
  } catch {
    payload = {};
  }
  if (!response.ok || payload.error) {
    throw new ApiError({
      code: payload.error ?? `oidc_${response.status}`,
      message: payload.error_description ?? `身份提供方返回 HTTP ${response.status}`,
      status: response.status,
      retryable: response.status >= 500,
    });
  }
  return payload;
}

async function fetchUserInfo(settings: OidcSettings, accessToken: string, fetchImpl: typeof fetch): Promise<JwtClaims> {
  const authority = settings.authority.replace(/\/+$/, "");
  const response = await fetchImpl(`${authority}/protocol/openid-connect/userinfo`, {
    headers: { Authorization: `Bearer ${accessToken}`, Accept: "application/json" },
  });
  if (!response.ok) {
    throw new ApiError({ code: "userinfo_failed", message: "读取用户信息失败", status: response.status });
  }
  return (await response.json()) as JwtClaims;
}

export async function completeOidcLogin(
  settings: OidcSettings,
  callbackUrl: string,
  fetchImpl: typeof fetch = globalThis.fetch.bind(globalThis),
  storage: Storage = window.sessionStorage,
): Promise<AuthSession> {
  const url = new URL(callbackUrl);
  const transaction = readOidcTransaction(storage);
  const state = url.searchParams.get("state");
  const oidcError = url.searchParams.get("error");

  if (!transaction) {
    throw new ApiError({ code: "missing_transaction", message: "OIDC 登录会话不存在或已过期，请重新登录", status: 400 });
  }
  if (state !== transaction.state) {
    clearOidcTransaction(storage);
    throw new ApiError({ code: "invalid_state", message: "OIDC state 校验失败，请重新登录", status: 400 });
  }
  if (oidcError) {
    clearOidcTransaction(storage);
    throw new ApiError({
      code: oidcError,
      message: url.searchParams.get("error_description") ?? "身份提供方拒绝了登录请求",
      status: 401,
    });
  }
  const code = url.searchParams.get("code");
  if (!code) {
    clearOidcTransaction(storage);
    throw new ApiError({ code: "missing_code", message: "OIDC 回调缺少授权码", status: 400 });
  }

  const tokens = await postToken(
    settings,
    new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: transaction.redirectUri,
      client_id: settings.clientId,
      code_verifier: transaction.codeVerifier,
    }),
    fetchImpl,
  );
  if (!tokens.access_token) {
    throw new ApiError({ code: "missing_access_token", message: "身份提供方未返回 access_token", status: 401 });
  }

  let claims: JwtClaims = tokens.id_token ? decodeJwtPayload(tokens.id_token) : {};
  try {
    claims = { ...claims, ...(await fetchUserInfo(settings, tokens.access_token, fetchImpl)) };
  } catch {
    // userinfo 不可用时回退到 ID Token claims；授权码流程已完成，不阻断登录。
  }

  clearOidcTransaction(storage);
  return {
    user: claimsToUser(claims),
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token,
    expiresAt: typeof tokens.expires_in === "number" ? Date.now() + tokens.expires_in * 1000 : undefined,
  };
}

export async function refreshOidcSession(
  settings: OidcSettings,
  session: AuthSession,
  fetchImpl: typeof fetch = globalThis.fetch.bind(globalThis),
): Promise<AuthSession> {
  if (!session.refreshToken) {
    throw new ApiError({ code: "missing_refresh_token", message: "会话缺少 refresh token", status: 401 });
  }
  const tokens = await postToken(
    settings,
    new URLSearchParams({
      grant_type: "refresh_token",
      refresh_token: session.refreshToken,
      client_id: settings.clientId,
    }),
    fetchImpl,
  );
  if (!tokens.access_token) {
    throw new ApiError({ code: "missing_access_token", message: "刷新会话未返回 access_token", status: 401 });
  }
  return {
    user: session.user,
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token ?? session.refreshToken,
    expiresAt: typeof tokens.expires_in === "number" ? Date.now() + tokens.expires_in * 1000 : undefined,
  };
}

/** Keycloak/OIDC 认证适配器。token 刷新统一发生在 getSession 边界。 */
export function createOidcAuthAdapter(settings: OidcSettings): AuthAdapter {
  const adapter: AuthAdapter = {
    mode: "oidc",

    async login(): Promise<AuthSession> {
      throw new ApiError({ code: "use_oidc_redirect", message: "OIDC 模式请使用统一的登录跳转入口", status: 400 });
    },

    async startLogin(returnTo = "/projects"): Promise<void> {
      await startOidcLogin(settings, returnTo);
    },

    async completeLogin(callbackUrl: string): Promise<AuthSession> {
      const session = await completeOidcLogin(settings, callbackUrl);
      saveSession(session);
      return session;
    },

    async logout(): Promise<void> {
      clearSession();
      window.location.assign(buildLogoutUrl(settings));
    },

    async getSession(): Promise<AuthSession | null> {
      const stored = loadSession();
      if (!stored) return null;
      if (stored.expiresAt && Date.now() >= stored.expiresAt - EXPIRY_SKEW_MS) {
        if (!stored.refreshToken) {
          clearSession();
          return null;
        }
        try {
          const refreshed = await refreshOidcSession(settings, stored);
          saveSession(refreshed);
          return refreshed;
        } catch {
          clearSession();
          return null;
        }
      }
      return stored;
    },

    async getAccessToken(): Promise<string | null> {
      const session = await adapter.getSession();
      return session?.accessToken ?? null;
    },
  };
  return adapter;
}

export type { ApiErrorPayload };
