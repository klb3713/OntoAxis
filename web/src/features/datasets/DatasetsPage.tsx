import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCreate, useCustom, useList, type HttpError } from "@refinedev/core";
import { toErrorMessage } from "../../api/errors";
import type { Dataset, DatasetPreview } from "../../api/types";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { formatDateTime } from "../../lib/format";
import { useDebouncedValue } from "../../lib/useDebouncedValue";

const PAGE_SIZE = 10;

type DatasetInput = {
  name: string;
  displayName: string;
  description?: string;
  format: "ICEBERG" | "PARQUET";
  storageLocation?: string;
};

const EMPTY_FORM: DatasetInput = {
  name: "",
  displayName: "",
  description: "",
  format: "ICEBERG",
  storageLocation: "",
};

export function DatasetsPage() {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search);
  const [previewId, setPreviewId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState<DatasetInput>(EMPTY_FORM);
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const { query } = useList<Dataset>({
    resource: "datasets",
    pagination: { currentPage: page, pageSize: PAGE_SIZE },
    filters: debouncedSearch ? [{ field: "q", operator: "contains", value: debouncedSearch }] : [],
    sorters: [{ field: "updatedAt", order: "desc" }],
  });
  const createMutation = useCreate<Dataset, HttpError, DatasetInput>({ resource: "datasets" });
  const previewQuery = useCustom<DatasetPreview, HttpError>({
    url: `datasets/${previewId ?? ""}/preview`,
    method: "get",
    queryOptions: { enabled: Boolean(previewId) },
  });

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch]);

  const datasets = query.data?.data ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const preview = previewQuery.query.data?.data;

  const openCreate = () => {
    setForm(EMPTY_FORM);
    setFormError(null);
    setCreateOpen(true);
  };

  const handleCreate = async (event: React.FormEvent) => {
    event.preventDefault();
    setFormError(null);
    if (!form.name.trim() || !form.displayName.trim()) {
      setFormError(t("datasets.createFailed"));
      return;
    }
    try {
      await createMutation.mutateAsync({ values: form });
      setCreateOpen(false);
      setNotice(t("datasets.createSuccess"));
      await query.refetch();
    } catch (error) {
      setFormError(toErrorMessage(error, t("datasets.createFailed")));
    }
  };

  return (
    <div className="page">
      <PageHeader
        eyebrow={t("app.stage")}
        title={t("datasets.title")}
        description={t("datasets.description")}
        actions={
          <button type="button" className="button button--primary" onClick={openCreate}>
            <span aria-hidden="true">＋</span> {t("datasets.create")}
          </button>
        }
      />

      {notice ? (
        <div className="inline-notice" role="status">
          {notice}
          <button type="button" className="inline-notice__close" aria-label={t("common.close")} onClick={() => setNotice(null)}>
            ×
          </button>
        </div>
      ) : null}

      <section className="card">
        <div className="toolbar">
          <label className="search-field">
            <span className="search-field__icon" aria-hidden="true">⌕</span>
            <input
              type="search"
              value={search}
              placeholder={t("datasets.searchPlaceholder")}
              aria-label={t("common.search")}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <span className="toolbar__count">{t("common.total", { count: total })}</span>
        </div>

        {query.isLoading ? <LoadingBlock /> : null}
        {query.isError ? <ErrorBlock error={query.error} onRetry={() => void query.refetch()} /> : null}
        {!query.isLoading && !query.isError && datasets.length === 0 ? <EmptyBlock title={t("datasets.empty")} /> : null}

        {datasets.length > 0 ? (
          <>
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>{t("common.displayName")}</th>
                    <th>{t("datasets.format")}</th>
                    <th>{t("datasets.storage")}</th>
                    <th>{t("datasets.owner")}</th>
                    <th>{t("datasets.updated")}</th>
                    <th>{t("common.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {datasets.map((dataset) => (
                    <tr key={dataset.id}>
                      <td>
                        <div className="entity-title">
                          <span className="entity-title__mark entity-title__mark--dataset" aria-hidden="true">▤</span>
                          <div>
                            <strong>{dataset.displayName}</strong>
                            <code>{dataset.name}</code>
                          </div>
                        </div>
                      </td>
                      <td><span className={`badge ${dataset.format === "ICEBERG" ? "badge--blue" : "badge--violet"}`}>{dataset.format}</span></td>
                      <td><code className="truncate">{dataset.storageLocation}</code></td>
                      <td>{dataset.ownerId}</td>
                      <td className="cell-muted">{formatDateTime(dataset.updatedAt)}</td>
                      <td>
                        <button type="button" className="button button--ghost button--sm" onClick={() => setPreviewId(dataset.id)}>
                          {t("datasets.preview")}
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

      <Modal
        open={createOpen}
        title={t("datasets.create")}
        onClose={() => setCreateOpen(false)}
        footer={
          <>
            <button type="button" className="button button--ghost" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </button>
            <button type="submit" form="create-dataset-form" className="button button--primary" disabled={createMutation.mutation.isPending}>
              {createMutation.mutation.isPending ? t("common.loading") : t("common.create")}
            </button>
          </>
        }
      >
        <form id="create-dataset-form" className="form" onSubmit={handleCreate}>
          {formError ? <div className="form-error" role="alert">{formError}</div> : null}
          <label className="field">
            <span>{t("datasets.name")}</span>
            <input
              required
              value={form.name}
              placeholder={t("datasets.namePlaceholder")}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("datasets.displayName")}</span>
            <input
              required
              value={form.displayName}
              placeholder={t("datasets.displayNamePlaceholder")}
              onChange={(event) => setForm({ ...form, displayName: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("datasets.format")}</span>
            <select
              value={form.format}
              onChange={(event) => setForm({ ...form, format: event.target.value as DatasetInput["format"] })}
            >
              <option value="ICEBERG">{t("datasets.formatIceberg")}</option>
              <option value="PARQUET">{t("datasets.formatParquet")}</option>
            </select>
          </label>
          <label className="field">
            <span>{t("datasets.storage")}</span>
            <input
              value={form.storageLocation}
              placeholder={t("datasets.storagePlaceholder")}
              onChange={(event) => setForm({ ...form, storageLocation: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("common.description")}</span>
            <textarea
              rows={3}
              value={form.description}
              placeholder={t("datasets.descriptionPlaceholder")}
              onChange={(event) => setForm({ ...form, description: event.target.value })}
            />
          </label>
        </form>
      </Modal>

      <Modal
        open={Boolean(previewId)}
        wide
        title={t("datasets.previewTitle")}
        onClose={() => setPreviewId(null)}
        footer={
          <button type="button" className="button button--ghost" onClick={() => setPreviewId(null)}>
            {t("datasets.selectPreview")}
          </button>
        }
      >
        {previewQuery.query.isLoading ? <LoadingBlock /> : null}
        {previewQuery.query.isError ? (
          <ErrorBlock error={previewQuery.query.error} onRetry={() => void previewQuery.query.refetch()} />
        ) : null}
        {preview ? (
          <>
            <div className="preview-meta">
              <span className="badge badge--blue">{t("datasets.rows", { count: preview.rowCount })}</span>
              <span className="badge badge--neutral">{t("datasets.columns", { count: preview.columns.length })}</span>
            </div>
            <div className="table-wrap table-wrap--scroll">
              <table className="data-table data-table--compact">
                <thead>
                  <tr>
                    {preview.columns.map((column) => (
                      <th key={column.name}>
                        {column.name}
                        <small>{column.type}</small>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.map((row, rowIndex) => (
                    <tr key={rowIndex}>
                      {preview.columns.map((column) => (
                        <td key={column.name}>{row[column.name] === null || row[column.name] === undefined ? "—" : String(row[column.name])}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : null}
      </Modal>
    </div>
  );
}
