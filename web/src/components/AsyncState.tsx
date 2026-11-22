import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toErrorMessage } from "../api/errors";

export function LoadingBlock({ label }: { label?: string }) {
  const { t } = useTranslation();
  return (
    <div className="state-block" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      <span>{label ?? t("common.loading")}</span>
    </div>
  );
}

export function ErrorBlock({ error, onRetry, title }: { error: unknown; onRetry?: () => void; title?: string }) {
  const { t } = useTranslation();
  return (
    <div className="state-block state-block--error" role="alert">
      <div>
        <strong>{title ?? t("common.errorTitle")}</strong>
        <p>{toErrorMessage(error, t("errors.requestFailed"))}</p>
      </div>
      {onRetry ? (
        <button type="button" className="button button--secondary button--sm" onClick={onRetry}>
          {t("common.retry")}
        </button>
      ) : null}
    </div>
  );
}

export function EmptyBlock({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="state-block state-block--empty">
      <span className="empty-glyph" aria-hidden="true">
        ◇
      </span>
      <div>
        <strong>{title}</strong>
        {description ? <p>{description}</p> : null}
      </div>
      {action}
    </div>
  );
}
