import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCustom, useCustomMutation, type HttpError } from "@refinedev/core";
import { toErrorMessage } from "../../api/errors";
import type { ApprovalTask } from "../../api/types";
import { ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { formatDateTime } from "../../lib/format";
import { approvalStatusBadgeClass } from "./approvalStatus";

type ApprovalDecisionInput = {
  comment: string;
};

type ApprovalDetailProps = {
  approvalId: string;
  /** 决定成功后通知列表页刷新（只以服务端状态为准，不做乐观更新）。 */
  onDecided: (message: string) => void;
};

function readStatusCode(error: unknown): number {
  if (typeof error !== "object" || error === null) return 0;
  const statusCode = (error as { statusCode?: unknown }).statusCode;
  return typeof statusCode === "number" ? statusCode : 0;
}

export function ApprovalDetail({ approvalId, onDecided }: ApprovalDetailProps) {
  const { t } = useTranslation();
  const [comment, setComment] = useState("");
  const [decisionError, setDecisionError] = useState<string | null>(null);

  const detailQuery = useCustom<ApprovalTask, HttpError>({
    url: `actions/approvals/${encodeURIComponent(approvalId)}`,
    method: "get",
  });
  const decisionMutation = useCustomMutation<ApprovalTask, HttpError, ApprovalDecisionInput>();

  const task = detailQuery.query.data?.data;
  const isPending = task?.status === "pending";
  const isSubmitting = decisionMutation.mutation.isPending;

  const toDecisionError = (error: unknown): string => {
    switch (readStatusCode(error)) {
      case 403:
        return t("approvals.errors.noPermission");
      case 409:
        return t("approvals.errors.alreadyDecided");
      case 503:
        return t("approvals.errors.serviceUnavailable");
      default:
        return toErrorMessage(error, t("approvals.errors.decideFailed"));
    }
  };

  const handleDecision = async (decision: "approve" | "reject") => {
    setDecisionError(null);
    try {
      await decisionMutation.mutateAsync({
        url: `actions/approvals/${encodeURIComponent(approvalId)}/${decision}`,
        method: "post",
        values: { comment },
      });
      // 成功后重新拉取服务端状态，再通知列表刷新；不在本地伪造成功状态。
      await detailQuery.query.refetch();
      onDecided(t(decision === "approve" ? "approvals.approveSuccess" : "approvals.rejectSuccess"));
    } catch (error) {
      setDecisionError(toDecisionError(error));
      const statusCode = readStatusCode(error);
      if (statusCode === 403 || statusCode === 409) {
        // 权限不足或已被他人决定时，刷新以展示真实状态。
        void detailQuery.query.refetch();
      }
    }
  };

  if (detailQuery.query.isLoading) {
    return <LoadingBlock />;
  }

  if (detailQuery.query.isError) {
    return <ErrorBlock error={detailQuery.query.error} onRetry={() => void detailQuery.query.refetch()} />;
  }

  if (!task) {
    return null;
  }

  const disabled = !isPending || isSubmitting;

  return (
    <div className="form">
      <div className="toolbar toolbar--compact">
        <span className={approvalStatusBadgeClass(task.status)}>{t(`approvals.status.${task.status}`)}</span>
        <code className="cell-muted">{task.id}</code>
      </div>

      <dl className="meta-grid">
        <div>
          <dt>{t("approvals.columnExecution")}</dt>
          <dd>
            <code>{task.executionId}</code>
          </dd>
        </div>
        <div>
          <dt>{t("approvals.columnStep")}</dt>
          <dd>{t("approvals.stepValue", { index: task.stepIndex })}</dd>
        </div>
        <div>
          <dt>{t("approvals.requestedAt")}</dt>
          <dd>{formatDateTime(task.requestedAt)}</dd>
        </div>
        <div>
          <dt>{t("approvals.deadlineAt")}</dt>
          <dd>{formatDateTime(task.deadlineAt)}</dd>
        </div>
        <div>
          <dt>{t("approvals.decidedAt")}</dt>
          <dd>{task.decidedAt ? formatDateTime(task.decidedAt) : "—"}</dd>
        </div>
        <div>
          <dt>{t("approvals.decidedBy")}</dt>
          <dd>{task.decidedBy || "—"}</dd>
        </div>
        <div>
          <dt>{t("approvals.comment")}</dt>
          <dd>{task.comment || "—"}</dd>
        </div>
        <div>
          <dt>{t("approvals.reasonCode")}</dt>
          <dd>{task.reasonCode || "—"}</dd>
        </div>
      </dl>

      {decisionError ? (
        <div className="form-error" role="alert">
          {decisionError}
        </div>
      ) : null}

      <p className="cell-muted">{isPending ? t("approvals.pendingHint") : t("approvals.decidedHint")}</p>

      <label className="field">
        <span>{t("approvals.comment")}</span>
        <textarea
          rows={3}
          value={comment}
          placeholder={t("approvals.commentPlaceholder")}
          disabled={disabled}
          onChange={(event) => setComment(event.target.value)}
        />
      </label>

      <div className="toolbar toolbar--compact">
        <button
          type="button"
          className="button button--primary"
          disabled={disabled}
          onClick={() => void handleDecision("approve")}
        >
          {isSubmitting ? t("approvals.submitting") : t("approvals.approve")}
        </button>
        <button
          type="button"
          className="button button--secondary"
          disabled={disabled}
          onClick={() => void handleDecision("reject")}
        >
          {t("approvals.reject")}
        </button>
      </div>
    </div>
  );
}