import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router-dom";
import { toErrorMessage } from "../../api/errors";
import { useAuthRuntime } from "../../auth/AuthContext";
import type { SupportedLocale } from "../../config/runtime";
import { currentLocale, setLocale } from "../../i18n";

type LoginLocationState = { from?: string };

export function LoginPage() {
  const { t } = useTranslation();
  const { config, authAdapter } = useAuthRuntime();
  const navigate = useNavigate();
  const location = useLocation();
  const [email, setEmail] = useState("demo@ontoaxis.local");
  const [password, setPassword] = useState("demo");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [redirecting, setRedirecting] = useState(false);

  const returnTo = (location.state as LoginLocationState | null)?.from ?? "/projects";
  const isOidc = config.authMode === "oidc";

  const handleLocaleChange = (locale: SupportedLocale) => {
    void setLocale(locale);
  };

  const handleMockLogin = async (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setError(t("login.invalidEmail"));
      return;
    }
    if (!password) {
      setError(t("login.failed"));
      return;
    }
    setSubmitting(true);
    try {
      await authAdapter.login({ email, password, displayName: displayName || undefined });
      navigate(returnTo, { replace: true });
    } catch (loginError) {
      setError(toErrorMessage(loginError, t("login.failed")));
    } finally {
      setSubmitting(false);
    }
  };

  const handleOidcLogin = async () => {
    setError(null);
    setRedirecting(true);
    try {
      if (!authAdapter.startLogin) throw new Error("OIDC 适配器未提供登录跳转入口");
      await authAdapter.startLogin(returnTo);
    } catch (loginError) {
      setRedirecting(false);
      setError(toErrorMessage(loginError, t("login.failed")));
    }
  };

  return (
    <div className="login-page">
      <section className="login-hero">
        <div className="login-hero__brand">
          <span className="brand-mark brand-mark--light" aria-hidden="true">
            <span />
            <span />
            <span />
          </span>
          <span>{t("app.name")}</span>
        </div>
        <div className="login-hero__content">
          <p className="login-hero__eyebrow">{t("login.eyebrow")}</p>
          <h1>{t("login.title")}</h1>
          <p>{t("login.subtitle")}</p>
          <ul className="feature-list">
            <li>
              <strong>{t("login.feature1Title")}</strong>
              <span>{t("login.feature1Text")}</span>
            </li>
            <li>
              <strong>{t("login.feature2Title")}</strong>
              <span>{t("login.feature2Text")}</span>
            </li>
            <li>
              <strong>{t("login.feature3Title")}</strong>
              <span>{t("login.feature3Text")}</span>
            </li>
          </ul>
        </div>
        <p className="login-hero__copyright">{t("login.copyright")}</p>
      </section>

      <section className="login-panel">
        <div className="login-panel__top">
          <label className="locale-select">
            <span className="sr-only">{t("login.language")}</span>
            <select
              aria-label={t("login.language")}
              value={currentLocale()}
              onChange={(event) => handleLocaleChange(event.target.value as SupportedLocale)}
            >
              <option value="zh-CN">简体中文</option>
              <option value="en">English</option>
            </select>
          </label>
        </div>

        <div className="login-card">
          <div className="login-card__heading">
            <span className={`badge ${isOidc ? "badge--green" : "badge--amber"}`}>
              {isOidc ? t("layout.oidcBadge") : t("layout.mockAuthBadge")}
            </span>
            <h2>{isOidc ? t("login.oidc") : t("login.signIn")}</h2>
          </div>

          {error ? <div className="form-error" role="alert">{error}</div> : null}

          {isOidc ? (
            <>
              <p className="hint">{t("login.oidcHint")}</p>
              <button type="button" className="button button--primary button--block" onClick={() => void handleOidcLogin()} disabled={redirecting}>
                {redirecting ? t("login.signingIn") : t("login.oidc")}
              </button>
            </>
          ) : (
            <>
              <p className="hint">{t("login.mockHint")}</p>
              <form className="form" noValidate onSubmit={handleMockLogin}>
                <label className="field">
                  <span>{t("login.email")}</span>
                  <input
                    type="email"
                    required
                    autoComplete="username"
                    value={email}
                    placeholder={t("login.emailPlaceholder")}
                    onChange={(event) => setEmail(event.target.value)}
                  />
                </label>
                <label className="field">
                  <span>{t("login.password")}</span>
                  <input
                    type="password"
                    required
                    autoComplete="current-password"
                    value={password}
                    placeholder={t("login.passwordPlaceholder")}
                    onChange={(event) => setPassword(event.target.value)}
                  />
                </label>
                <label className="field">
                  <span>{t("login.displayName")} <small>{t("common.optional")}</small></span>
                  <input
                    value={displayName}
                    placeholder={t("login.displayNamePlaceholder")}
                    onChange={(event) => setDisplayName(event.target.value)}
                  />
                </label>
                <button type="submit" className="button button--primary button--block" disabled={submitting}>
                  {submitting ? t("login.signingIn") : t("login.signIn")}
                </button>
              </form>
              <p className="login-card__demo">{t("login.demoHint")}</p>
            </>
          )}
        </div>
      </section>
    </div>
  );
}
