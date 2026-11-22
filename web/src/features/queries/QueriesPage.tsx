import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCreate, useCustomMutation, useList, type HttpError } from "@refinedev/core";
import { toErrorMessage } from "../../api/errors";
import type { QueryResult, SavedQuery } from "../../api/types";
import { EmptyBlock, LoadingBlock } from "../../components/AsyncState";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { useDebouncedValue } from "../../lib/useDebouncedValue";

const DEFAULT_SQL = "SELECT * FROM orders LIMIT 20";

type QueryPlan = { plan: string };

export function QueriesPage() {
  const { t } = useTranslation();
  const [sql, setSql] = useState(DEFAULT_SQL);
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search);
  const [result, setResult] = useState<QueryResult | null>(null);
  const [plan, setPlan] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saveOpen, setSaveOpen] = useState(false);
  const [saveName, setSaveName] = useState("");
  const [notice, setNotice] = useState<string | null>(null);

  const savedQuery = useList<SavedQuery>({
    resource: "saved-queries",
    pagination: { currentPage: 1, pageSize: 50 },
    filters: debouncedSearch ? [{ field: "q", operator: "contains", value: debouncedSearch }] : [],
    sorters: [{ field: "updatedAt", order: "desc" }],
  });
  const executeMutation = useCustomMutation<QueryResult, HttpError, { sql: string; limit?: number }>();
  const explainMutation = useCustomMutation<QueryPlan, HttpError, { sql: string }>();
  const createSavedQuery = useCreate<SavedQuery, HttpError, { name: string; sql: string }>({ resource: "saved-queries" });

  const savedQueries = savedQuery.query.data?.data ?? [];

  const handleRun = async () => {
    setError(null);
    setPlan(null);
    if (!sql.trim()) {
      setError(t("queries.emptySql"));
      return;
    }
    try {
      const response = await executeMutation.mutateAsync({
        url: "query/execute",
        method: "post",
        values: { sql, limit: 200 },
      });
      setResult(response.data);
    } catch (runError) {
      setResult(null);
      setError(toErrorMessage(runError, t("queries.error")));
    }
  };

  const handleExplain = async () => {
    setError(null);
    if (!sql.trim()) {
      setError(t("queries.emptySql"));
      return;
    }
    try {
      const response = await explainMutation.mutateAsync({ url: "query/explain", method: "post", values: { sql } });
      setPlan(response.data.plan);
    } catch (explainError) {
      setPlan(null);
      setError(toErrorMessage(explainError, t("queries.error")));
    }
  };

  const openSave = () => {
    setSaveName("");
    setSaveOpen(true);
  };

  const handleSave = async (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!saveName.trim()) {
      setError(t("queries.saveFailed"));
      return;
    }
    try {
      await createSavedQuery.mutateAsync({ values: { name: saveName.trim(), sql } });
      setSaveOpen(false);
      setNotice(t("queries.saveSuccess"));
      await savedQuery.query.refetch();
    } catch (saveError) {
      setError(toErrorMessage(saveError, t("queries.saveFailed")));
    }
  };

  const loadSaved = (query: SavedQuery) => {
    setSql(query.sql);
    setResult(null);
    setPlan(null);
    setError(null);
  };

  return (
    <div className="page">
      <PageHeader eyebrow={t("app.stage")} title={t("queries.title")} description={t("queries.description")} />

      {notice ? (
        <div className="inline-notice" role="status">
          {notice}
          <button type="button" className="inline-notice__close" aria-label={t("common.close")} onClick={() => setNotice(null)}>
            ×
          </button>
        </div>
      ) : null}

      <div className="queries-layout">
        <section className="card saved-queries">
          <div className="card-heading">
            <h2>{t("queries.saved")}</h2>
            <span className="badge badge--neutral">{savedQueries.length}</span>
          </div>
          <label className="search-field search-field--sm">
            <span className="search-field__icon" aria-hidden="true">⌕</span>
            <input
              type="search"
              value={search}
              placeholder={t("common.searchPlaceholder")}
              aria-label={t("common.search")}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          {savedQuery.query.isLoading ? <LoadingBlock /> : null}
          {!savedQuery.query.isLoading && savedQueries.length === 0 ? <EmptyBlock title={t("queries.emptySaved")} /> : null}
          <div className="saved-queries__list">
            {savedQueries.map((query) => (
              <button key={query.id} type="button" className="saved-query" onClick={() => loadSaved(query)}>
                <span className="saved-query__name">{query.name}</span>
                <code className="saved-query__sql">{query.sql}</code>
              </button>
            ))}
          </div>
          <p className="hint">{t("queries.loadHint")}</p>
        </section>

        <section className="card query-workbench">
          <div className="card-heading">
            <h2>{t("queries.editor")}</h2>
            <div className="button-row">
              <button type="button" className="button button--ghost button--sm" onClick={openSave}>
                {t("queries.save")}
              </button>
              <button type="button" className="button button--secondary button--sm" onClick={() => void handleExplain()} disabled={explainMutation.mutation.isPending}>
                {t("queries.explain")}
              </button>
              <button type="button" className="button button--primary button--sm" onClick={() => void handleRun()} disabled={executeMutation.mutation.isPending}>
                {executeMutation.mutation.isPending ? t("queries.running") : t("queries.run")}
              </button>
            </div>
          </div>

          <textarea
            className="sql-editor"
            aria-label={t("queries.editor")}
            value={sql}
            spellCheck={false}
            onChange={(event) => setSql(event.target.value)}
            placeholder={t("queries.sqlPlaceholder")}
          />
          <p className="hint">{t("queries.limit")}</p>

          {error ? <div className="form-error" role="alert">{error}</div> : null}
          {plan ? (
            <div className="query-plan">
              <h3 className="section-title">{t("queries.explainTitle")}</h3>
              <pre className="json-block">{plan}</pre>
            </div>
          ) : null}

          <h3 className="section-title">{t("queries.results")}</h3>
          {!result ? (
            <EmptyBlock title={t("queries.emptyResults")} />
          ) : (
            <>
              <div className="preview-meta">
                <span className="badge badge--green">{t("queries.rowCount", { count: result.rowCount })}</span>
                <span className="badge badge--neutral">{t("queries.elapsed", { ms: result.elapsedMs })}</span>
              </div>
              <div className="table-wrap table-wrap--scroll">
                <table className="data-table data-table--compact">
                  <thead>
                    <tr>
                      {result.columns.map((column) => (
                        <th key={column}>{column}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {result.rows.map((row, rowIndex) => (
                      <tr key={rowIndex}>
                        {row.map((cell, cellIndex) => (
                          <td key={cellIndex}>{cell === null || cell === undefined ? "—" : String(cell)}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </section>
      </div>

      <Modal
        open={saveOpen}
        title={t("queries.saveQuery")}
        onClose={() => setSaveOpen(false)}
        footer={
          <>
            <button type="button" className="button button--ghost" onClick={() => setSaveOpen(false)}>
              {t("common.cancel")}
            </button>
            <button type="submit" form="save-query-form" className="button button--primary" disabled={createSavedQuery.mutation.isPending}>
              {createSavedQuery.mutation.isPending ? t("common.loading") : t("common.save")}
            </button>
          </>
        }
      >
        <form id="save-query-form" className="form" onSubmit={handleSave}>
          <label className="field">
            <span>{t("queries.queryName")}</span>
            <input
              required
              value={saveName}
              placeholder={t("queries.queryNamePlaceholder")}
              onChange={(event) => setSaveName(event.target.value)}
            />
          </label>
          <pre className="json-block json-block--compact">{sql}</pre>
        </form>
      </Modal>
    </div>
  );
}
