import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCustom, type HttpError } from "@refinedev/core";
import type { ApprovalListPayload, ApprovalStatusFilter } from "../../api/types";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { formatDateTime } from "../../lib/format";
import { ApprovalDetail } from "./ApprovalDetail";
import { approvalStatusBadgeClass, truncateId } from "./approvalStatus";

const PAGE_SIZE = 20;

const FILTERS: ApprovalStatusFilter[] = ["pending", "all", "approved", "rejected", "expired"];

const FILTER_LABEL_KEYS: Record<ApprovalStatusFilter, string> = {
  pending: "approvals.filterPending",
  all: "approvals.filterAll",
  approved: "approvals.filterApproved",
  rejected: "approvals.filterRejected",
  expired: "approvals.filterExpired",
};

type ApprovalListQuery = {
  status: ApprovalStatusFilter;
  limit: number;
  offset: number;
};

export function ApprovalsPage() {
  const { t } = useTranslation();
  const [statusFilter, setStatusFilter] = useState<ApprovalStatusFilter>("pending");
  const [page, setPage] = useState(1);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // 审批端点不属于 Refine 的 resource CRUD 路径，因此走 custom 请求；
  // HTTP 模式下 rest data provider 会拼出 /api/v1/actions/approvals 并复用鉴权头。
  const listQuery = useCustom<ApprovalListPayload, HttpError, ApprovalListQuery>({
    url: "actions/approvals",
    method: "get",
    config: {
      query: { status: statusFilter, limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE },
    },
  });

  const items = listQuery.query.data?.data.items ?? [];
  const total = listQuery.query.data?.data.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  useEffect(() => {
    // 决定后当前页可能因筛选结果减少而越界，回退到第一页。
    if (listQuery.query.isSuccess && items.length === 0 && total > 0 && page > 1) {
      setPage(1);
    }
  }, [items.length, listQuery.query.isSuccess, page, total]);

  const handleFilterChange = (filter: ApprovalStatusFilter) => {
    setStatusFilter(filter);
    setPage(1);
  };

  return (
    <div className="page">
      <PageHeader eyebrow={t("app.stage")} title={t("approvals.title")} description={t("approvals.description")} />

      {notice ? (
        <div className="inline-notice" role="status">
          {notice}
          <button type="button" className="inline-notice__close" aria-label={t("common.close")} onClick={() => setNotice(null)}>
            ×
          </button>
        </div>
      ) : null}

      <section className="card">
        <div className="tabs" role="tablist" aria-label={t("approvals.filterLabel")}>
          {FILTERS.map((filter) => (
            <button
              key={filter}
              type="button"
              role="tab"
              aria-selected={statusFilter === filter}
              className={`tab${statusFilter === filter ? " tab--active" : ""}`}
              onClick={() => handleFilterChange(filter)}
            >
              {t(FILTER_LABEL_KEYS[filter])}
            </button>
          ))}
        </div>

        <div className="toolbar">
          <span className="toolbar__count">{t("common.total", { count: total })}</span>
        </div>

        {listQuery.query.isLoading ? <LoadingBlock /> : null}
        {listQuery.query.isError ? (
          <ErrorBlock error={listQuery.query.error} onRetry={() => void listQuery.query.refetch()} />
        ) : null}
        {!listQuery.query.isLoading && !listQuery.query.isError && items.length === 0 ? (
          <EmptyBlock title={t("approvals.empty")} />
        ) : null}

        {items.length > 0 ? (
          <>
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>{t("approvals.columnExecution")}</th>
                    <th>{t("approvals.columnStep")}</th>
                    <th>{t("approvals.columnRequested")}</th>
                    <th>{t("approvals.columnDeadline")}</th>
                    <th>{t("approvals.columnStatus")}</th>
                    <th>{t("approvals.columnAction")}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((task) => (
                    <tr key={task.id}>
                      <td>
                        <code className="truncate" title={task.executionId}>
                          {truncateId(task.executionId)}
                        </code>
                      </td>
                      <td>{task.stepIndex}</td>
                      <td className="cell-muted">{formatDateTime(task.requestedAt)}</td>
                      <td className="cell-muted">{formatDateTime(task.deadlineAt)}</td>
                      <td>
                        <span className={approvalStatusBadgeClass(task.status)}>{t(`approvals.status.${task.status}`)}</span>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="button button--ghost button--sm"
                          onClick={() => setSelectedId(task.id)}
                        >
                          {t("approvals.view")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <Pagination page={page} totalPages={totalPages} total={total} onChange={setPage} />
          </>
        ) : null}
      </section>

      <Modal open={Boolean(selectedId)} title={t("approvals.detailTitle")} onClose={() => setSelectedId(null)}>
        {selectedId ? (
          <ApprovalDetail
            key={selectedId}
            approvalId={selectedId}
            onDecided={(message) => {
              setNotice(message);
              void listQuery.query.refetch();
            }}
          />
        ) : null}
      </Modal>
    </div>
  );
}