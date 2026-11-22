import { useTranslation } from "react-i18next";

type PaginationProps = {
  page: number;
  totalPages: number;
  total: number;
  onChange: (page: number) => void;
};

export function Pagination({ page, totalPages, total, onChange }: PaginationProps) {
  const { t } = useTranslation();
  if (total === 0) return null;
  const safeTotalPages = Math.max(1, totalPages);

  return (
    <div className="pagination">
      <span className="pagination__total">{t("common.total", { count: total })}</span>
      <div className="pagination__controls">
        <button
          type="button"
          className="button button--ghost button--sm"
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
        >
          {t("common.previous")}
        </button>
        <span className="pagination__page">{t("common.page", { page, totalPages: safeTotalPages })}</span>
        <button
          type="button"
          className="button button--ghost button--sm"
          disabled={page >= safeTotalPages}
          onClick={() => onChange(page + 1)}
        >
          {t("common.next")}
        </button>
      </div>
    </div>
  );
}
