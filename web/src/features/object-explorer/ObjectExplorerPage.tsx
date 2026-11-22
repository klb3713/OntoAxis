import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useList } from "@refinedev/core";
import type { ObjectRecord, ObjectType } from "../../api/types";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { PageHeader } from "../../components/PageHeader";
import { formatDateTime } from "../../lib/format";
import { useDebouncedValue } from "../../lib/useDebouncedValue";

export function ObjectExplorerPage() {
  const { t } = useTranslation();
  const [selectedTypeId, setSelectedTypeId] = useState<string>("");
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search);
  const [selectedObjectId, setSelectedObjectId] = useState<string | null>(null);

  const objectTypesQuery = useList<ObjectType>({
    resource: "object-types",
    pagination: { currentPage: 1, pageSize: 100 },
    sorters: [{ field: "name", order: "asc" }],
  });
  const objectTypes = objectTypesQuery.query.data?.data ?? [];

  useEffect(() => {
    if (!selectedTypeId && objectTypes.length > 0) {
      setSelectedTypeId(objectTypes[0].id);
    }
  }, [objectTypes, selectedTypeId]);

  const activeType = objectTypes.find((item) => item.id === selectedTypeId) ?? null;

  const objectsQuery = useList<ObjectRecord>({
    resource: "objects",
    pagination: { currentPage: 1, pageSize: 100 },
    filters: [
      ...(selectedTypeId ? [{ field: "object_type_id", operator: "eq" as const, value: selectedTypeId }] : []),
      ...(debouncedSearch ? [{ field: "q", operator: "contains" as const, value: debouncedSearch }] : []),
    ],
  });
  const objects = objectsQuery.query.data?.data ?? [];
  const selectedObject = objects.find((item) => item.id === selectedObjectId) ?? null;

  useEffect(() => {
    setSelectedObjectId(null);
  }, [selectedTypeId, debouncedSearch]);

  const displayColumns = activeType?.properties.slice(0, 3) ?? [];

  const formatValue = (value: unknown): string => {
    if (value === null || value === undefined) return "—";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
  };

  return (
    <div className="page">
      <PageHeader eyebrow={t("app.stage")} title={t("explorer.title")} description={t("explorer.description")} />

      <section className="card">
        <div className="toolbar">
          <label className="field field--inline">
            <span>{t("explorer.selectType")}</span>
            <select
              aria-label={t("explorer.selectType")}
              value={selectedTypeId}
              onChange={(event) => setSelectedTypeId(event.target.value)}
            >
              {objectTypes.length === 0 ? <option value="">{t("explorer.chooseType")}</option> : null}
              {objectTypes.map((objectType) => (
                <option key={objectType.id} value={objectType.id}>
                  {objectType.displayName}（{objectType.name}）
                </option>
              ))}
            </select>
          </label>
          <label className="search-field">
            <span className="search-field__icon" aria-hidden="true">⌕</span>
            <input
              type="search"
              value={search}
              placeholder={t("explorer.searchPlaceholder")}
              aria-label={t("common.search")}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <span className="toolbar__count">{t("explorer.totalObjects", { count: objectsQuery.query.data?.total ?? 0 })}</span>
        </div>

        {objectTypesQuery.query.isLoading || objectsQuery.query.isLoading ? <LoadingBlock /> : null}
        {objectTypesQuery.query.isError ? (
          <ErrorBlock error={objectTypesQuery.query.error} onRetry={() => void objectTypesQuery.query.refetch()} />
        ) : null}
        {objectsQuery.query.isError ? (
          <ErrorBlock error={objectsQuery.query.error} onRetry={() => void objectsQuery.query.refetch()} />
        ) : null}

        {!objectsQuery.query.isLoading && !objectsQuery.query.isError ? (
          <div className="explorer-layout">
            <div className="explorer-list">
              {objects.length === 0 ? (
                <EmptyBlock title={t("explorer.empty")} />
              ) : (
                <div className="table-wrap">
                  <table className="data-table data-table--selectable">
                    <thead>
                      <tr>
                        <th>{t("explorer.objectId")}</th>
                        {displayColumns.map((property) => (
                          <th key={property.id}>{property.displayName}</th>
                        ))}
                        <th>{t("explorer.version")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {objects.map((object) => (
                        <tr
                          key={object.id}
                          className={object.id === selectedObjectId ? "is-selected" : undefined}
                          onClick={() => setSelectedObjectId(object.id)}
                        >
                          <td><code>{object.id}</code></td>
                          {displayColumns.map((property) => (
                            <td key={property.id}>{formatValue(object.properties[property.name])}</td>
                          ))}
                          <td><span className="badge badge--neutral">v{object.version}</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>

            <aside className="explorer-detail">
              {!selectedObject ? (
                <EmptyBlock title={t("explorer.selectObject")} />
              ) : (
                <>
                  <div className="detail-header detail-header--stacked">
                    <div>
                      <p className="detail-header__eyebrow">{t("explorer.detailTitle")}</p>
                      <h2>{activeType?.displayName ?? selectedObject.objectTypeId}</h2>
                      <code>{selectedObject.id}</code>
                    </div>
                  </div>
                  <dl className="meta-grid meta-grid--single">
                    <div>
                      <dt>{t("explorer.version")}</dt>
                      <dd>v{selectedObject.version}</dd>
                    </div>
                    <div>
                      <dt>{t("explorer.updatedAt")}</dt>
                      <dd>{formatDateTime(selectedObject.updatedAt)}</dd>
                    </div>
                  </dl>
                  <h3 className="section-title">{t("explorer.properties")}</h3>
                  <div className="property-list">
                    {Object.entries(selectedObject.properties).map(([key, value]) => (
                      <div className="property-row" key={key}>
                        <span className="property-row__key">{key}</span>
                        <span className="property-row__value">{formatValue(value)}</span>
                      </div>
                    ))}
                  </div>
                  <h3 className="section-title">{t("explorer.rawJson")}</h3>
                  <pre className="json-block">{JSON.stringify(selectedObject.properties, null, 2)}</pre>
                </>
              )}
            </aside>
          </div>
        ) : null}
      </section>
    </div>
  );
}
