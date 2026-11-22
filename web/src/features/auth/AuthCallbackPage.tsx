import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import { toErrorMessage } from "../../api/errors";
import { useAuthRuntime } from "../../auth/AuthContext";

export function AuthCallbackPage() {
  const { t } = useTranslation();
  const { authAdapter } = useAuthRuntime();
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    const complete = async () => {
      try {
        if (!authAdapter.completeLogin) {
          throw new Error("当前认证模式不支持 OIDC 回调");
        }
        await authAdapter.completeLogin(window.location.href);
        if (active) navigate("/projects", { replace: true });
      } catch (callbackError) {
        if (active) setError(toErrorMessage(callbackError, t("authCallback.failed")));
      }
    };
    void complete();
    return () => {
      active = false;
    };
  }, [authAdapter, navigate, t]);

  return (
    <div className="callback-page">
      <section className="card callback-card">
        <p className="login-hero__eyebrow">{t("app.name")}</p>
        <h1>{t("authCallback.title")}</h1>
        {error ? (
          <>
            <div className="form-error" role="alert">{error}</div>
            <Link className="button button--primary" to="/login">{t("authCallback.backToLogin")}</Link>
          </>
        ) : (
          <>
            <div className="spinner" aria-hidden="true" />
            <p className="hint">{t("authCallback.completing")}</p>
          </>
        )}
      </section>
    </div>
  );
}
