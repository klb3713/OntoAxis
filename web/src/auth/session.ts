import { ApiError } from "../api/errors";
import type { AuthSession } from "./types";

export const SESSION_STORAGE_KEY = "ontoaxis.web.session";

function defaultStorage(): Storage | null {
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

export function isAuthSession(value: unknown): value is AuthSession {
  if (typeof value !== "object" || value === null) return false;
  const session = value as Partial<AuthSession>;
  const user = session.user;
  return (
    typeof session.accessToken === "string" &&
    session.accessToken.length > 0 &&
    typeof user === "object" &&
    user !== null &&
    typeof user.id === "string" &&
    typeof user.email === "string" &&
    typeof user.displayName === "string" &&
    typeof user.organizationId === "string" &&
    typeof user.tenantId === "string" &&
    isStringArray(user.roles)
  );
}

export function loadSession(storage: Storage | null = defaultStorage()): AuthSession | null {
  if (!storage) return null;
  const raw = storage.getItem(SESSION_STORAGE_KEY);
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (isAuthSession(parsed)) return parsed;
  } catch {
    // 损坏的本地会话按未登录处理，并清除脏数据。
  }
  storage.removeItem(SESSION_STORAGE_KEY);
  return null;
}

export function saveSession(session: AuthSession, storage: Storage | null = defaultStorage()): void {
  if (!storage) return;
  storage.setItem(SESSION_STORAGE_KEY, JSON.stringify(session));
}

export function clearSession(storage: Storage | null = defaultStorage()): void {
  storage?.removeItem(SESSION_STORAGE_KEY);
}

export function requireSession(session: AuthSession | null): AuthSession {
  if (!session) {
    throw new ApiError({ code: "unauthenticated", message: "登录状态已失效，请重新登录", status: 401 });
  }
  return session;
}
