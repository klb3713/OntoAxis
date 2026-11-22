import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCreate, useList, type HttpError } from "@refinedev/core";
import { toErrorMessage } from "../../api/errors";
import type { Project, ProjectInput } from "../../api/types";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { formatDateTime } from "../../lib/format";
import { useDebouncedValue } from "../../lib/useDebouncedValue";

const PAGE_SIZE = 10;
const EMPTY_FORM: ProjectInput = { name: "", displayName: "", description: "" };

export function ProjectsPage() {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search);
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState<ProjectInput>(EMPTY_FORM);
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const { query } = useList<Project>({
    resource: "projects",
    pagination: { currentPage: page, pageSize: PAGE_SIZE },
    filters: debouncedSearch ? [{ field: "q", operator: "contains", value: debouncedSearch }] : [],
    sorters: [{ field: "updatedAt", order: "desc" }],
  });
  const createMutation = useCreate<Project, HttpError, ProjectInput>({ resource: "projects" });

  useEffect(() => {
    setPage(1);
  }, [debouncedSearch]);

  const projects = query.data?.data ?? [];
  const total = query.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const openCreate = () => {
    setForm(EMPTY_FORM);
    setFormError(null);
    setCreateOpen(true);
  };

  const handleCreate = async (event: React.FormEvent) => {
    event.preventDefault();
    setFormError(null);
    if (!form.name.trim() || !form.displayName.trim()) {
      setFormError(t("projects.createFailed"));
      return;
    }
    try {
      await createMutation.mutateAsync({ values: form });
      setCreateOpen(false);
      setNotice(t("projects.createSuccess"));
      await query.refetch();
    } catch (error) {
      setFormError(toErrorMessage(error, t("projects.createFailed")));
    }
  };

  return (
    <div className="page">
      <PageHeader
        eyebrow={t("app.stage")}
        title={t("projects.title")}
        description={t("projects.description")}
        actions={
          <button type="button" className="button button--primary" onClick={openCreate}>
            <span aria-hidden="true">＋</span> {t("projects.create")}
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
            <span className="search-field__icon" aria-hidden="true">
              ⌕
            </span>
            <input
              type="search"
              value={search}
              placeholder={t("projects.searchPlaceholder")}
              aria-label={t("common.search")}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <span className="toolbar__count">{t("projects.count", { count: total })}</span>
        </div>

        {query.isLoading ? <LoadingBlock /> : null}
        {query.isError ? <ErrorBlock error={query.error} onRetry={() => void query.refetch()} /> : null}
        {!query.isLoading && !query.isError && projects.length === 0 ? (
          <EmptyBlock title={t("projects.empty")} action={<button type="button" className="button button--secondary" onClick={openCreate}>{t("projects.create")}</button>} />
        ) : null}

        {projects.length > 0 ? (
          <>
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>{t("common.displayName")}</th>
                    <th>{t("common.description")}</th>
                    <th>{t("projects.workspace")}</th>
                    <th>{t("common.owner")}</th>
                    <th>{t("common.updatedAt")}</th>
                  </tr>
                </thead>
                <tbody>
                  {projects.map((project) => (
                    <tr key={project.id}>
                      <td>
                        <div className="entity-title">
                          <span className="entity-title__mark entity-title__mark--project" aria-hidden="true">
                            {project.displayName.slice(0, 1)}
                          </span>
                          <div>
                            <strong>{project.displayName}</strong>
                            <code>{project.name}</code>
                          </div>
                        </div>
                      </td>
                      <td className="cell-muted">{project.description || "—"}</td>
                      <td>
                        <span className="badge badge--neutral">{project.workspaceId}</span>
                      </td>
                      <td>{project.ownerId}</td>
                      <td className="cell-muted">{formatDateTime(project.updatedAt)}</td>
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
        title={t("projects.create")}
        onClose={() => setCreateOpen(false)}
        footer={
          <>
            <button type="button" className="button button--ghost" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </button>
            <button type="submit" form="create-project-form" className="button button--primary" disabled={createMutation.mutation.isPending}>
              {createMutation.mutation.isPending ? t("common.loading") : t("common.create")}
            </button>
          </>
        }
      >
        <form id="create-project-form" className="form" onSubmit={handleCreate}>
          {formError ? <div className="form-error" role="alert">{formError}</div> : null}
          <label className="field">
            <span>{t("projects.name")}</span>
            <input
              required
              pattern="[a-z0-9][a-z0-9-]*"
              title="小写字母、数字与中划线"
              value={form.name}
              placeholder={t("projects.namePlaceholder")}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("projects.displayName")}</span>
            <input
              required
              value={form.displayName}
              placeholder={t("projects.displayNamePlaceholder")}
              onChange={(event) => setForm({ ...form, displayName: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("common.description")}</span>
            <textarea
              rows={3}
              value={form.description}
              placeholder={t("projects.descriptionPlaceholder")}
              onChange={(event) => setForm({ ...form, description: event.target.value })}
            />
          </label>
        </form>
      </Modal>
    </div>
  );
}
