import { describe, expect, it } from "vitest";
import {
  base64UrlDecode,
  base64UrlEncode,
  buildAuthorizeUrl,
  createPkcePair,
  decodeJwtPayload,
  readOidcTransaction,
  sha256Base64Url,
} from "./oidc";
import type { OidcSettings } from "../config/runtime";

const settings: OidcSettings = {
  authority: "https://keycloak.example.com/realms/ontoaxis/",
  clientId: "ontoaxis-web",
  redirectUri: "https://app.example.com/auth/callback",
  scope: "openid profile email",
};

describe("oidc helpers", () => {
  it("base64url 编解码支持 UTF-8", () => {
    const bytes = new TextEncoder().encode("元轴 OntoAxis");
    expect(base64UrlDecode(base64UrlEncode(bytes))).toBe("元轴 OntoAxis");
  });

  it("PKCE 生成 S256 challenge 且 verifier 与 challenge 不同", async () => {
    const pair = await createPkcePair();
    expect(pair.codeVerifier.length).toBeGreaterThan(40);
    expect(pair.codeChallenge).toBe(await sha256Base64Url(pair.codeVerifier));
    expect(pair.codeChallenge).not.toBe(pair.codeVerifier);
    expect(pair.codeChallenge).not.toMatch(/[+/=]/);
  });

  it("授权 URL 带上 state、nonce 与 PKCE 参数", () => {
    const url = new URL(
      buildAuthorizeUrl(settings, {
        state: "state-1",
        nonce: "nonce-1",
        codeChallenge: "challenge-1",
      }),
    );
    expect(url.origin).toBe("https://keycloak.example.com");
    expect(url.pathname).toBe("/realms/ontoaxis/protocol/openid-connect/auth");
    expect(url.searchParams.get("response_type")).toBe("code");
    expect(url.searchParams.get("client_id")).toBe("ontoaxis-web");
    expect(url.searchParams.get("redirect_uri")).toBe(settings.redirectUri);
    expect(url.searchParams.get("state")).toBe("state-1");
    expect(url.searchParams.get("nonce")).toBe("nonce-1");
    expect(url.searchParams.get("code_challenge_method")).toBe("S256");
  });

  it("无效或缺失的 OIDC transaction 返回 null 并清理存储", () => {
    window.sessionStorage.setItem("ontoaxis.web.oidc.transaction", "{bad json");
    expect(readOidcTransaction()).toBeNull();
    expect(window.sessionStorage.getItem("ontoaxis.web.oidc.transaction")).toBeNull();
  });

  it("JWT payload 可解析", () => {
    const payload = base64UrlEncode(new TextEncoder().encode(JSON.stringify({ sub: "user-1", email: "a@b.co" })));
    expect(decodeJwtPayload(`x.${payload}.y`)).toMatchObject({ sub: "user-1", email: "a@b.co" });
  });
});
