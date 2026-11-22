/** 认证领域的稳定接口。UI 只依赖 AuthAdapter，不直接依赖 Keycloak/OIDC 细节。 */

export type AuthUser = {
  id: string;
  email: string;
  displayName: string;
  organizationId: string;
  tenantId: string;
  roles: string[];
  locale?: string;
};

export type AuthSession = {
  user: AuthUser;
  accessToken: string;
  refreshToken?: string;
  /** epoch milliseconds；mock 会话不设置。 */
  expiresAt?: number;
};

export type LoginCredentials = {
  email: string;
  password?: string;
  displayName?: string;
};

export type LoginOptions = {
  returnTo?: string;
};

export interface AuthAdapter {
  readonly mode: "mock" | "oidc";
  login(credentials: LoginCredentials, options?: LoginOptions): Promise<AuthSession>;
  /** OIDC 模式：跳转到身份提供方。 */
  startLogin?(returnTo?: string): Promise<void>;
  /** OIDC 模式：处理授权码回调。 */
  completeLogin?(callbackUrl: string): Promise<AuthSession>;
  logout(): Promise<void>;
  getSession(): Promise<AuthSession | null>;
  getAccessToken(): Promise<string | null>;
}
