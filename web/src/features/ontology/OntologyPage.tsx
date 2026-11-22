import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCreate, useList, type HttpError } from "@refinedev/core";
import { toErrorMessage } from "../../api/errors";
import type { LinkType, ObjectType, PropertyDefinition } from "../../api/types";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../../components/AsyncState";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { formatDateTime } from "../../lib/format";
import { useDebouncedValue } from "../../lib/useDebouncedValue";

type TabKey = "object-types" | "link-types";

type ObjectTypeInput = {
  name: string;
  displayName: string;
  description?: string;
};

type PropertyInput = {
  name: string;
  displayName: string;
  propertyType: string;
  required: boolean;
  unique: boolean;
};

const EMPTY_OBJECT_TYPE: ObjectTypeInput = { name: "", displayName: "", description: "" };
const EMPTY_PROPERTY: PropertyInput = {
  name: "",
  displayName: "",
  propertyType: "string",
  required: false,
  unique: false,
};

export function OntologyPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<TabKey>("object-types");
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebouncedValue(search);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [objectTypeOpen, setObjectTypeOpen] = useState(false);
  const [objectTypeForm, setObjectTypeForm] = useState<ObjectTypeInput>(EMPTY_OBJECT_TYPE);
  const [propertyOpen, setPropertyOpen] = useState(false);
  const [propertyForm, setPropertyForm] = useState<PropertyInput>(EMPTY_PROPERTY);
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const objectTypesQuery = useList<ObjectType>({
    resource: "object-types",
    pagination: { currentPage: 1, pageSize: 100 },
    filters: debouncedSearch ? [{ field: "q", operator: "contains", value: debouncedSearch }] : [],
    sorters: [{ field: "name", order: "asc" }],
  });
  const linkTypesQuery = useList<LinkType>({
    resource: "link-types",
    pagination: { currentPage: 1, pageSize: 100 },
    sorters: [{ field: "name", order: "asc" }],
  });

  const createObjectType = useCreate<ObjectType, HttpError, ObjectTypeInput>({ resource: "object-types" });
  const createProperty = useCreate<PropertyDefinition, HttpError, {
    objectTypeId: string;
    name: string;
    displayName: string;
    propertyType: string;
    required: boolean;
    uniqueConstraint: boolean;
  }>({ resource: "properties" });

  const objectTypes = objectTypesQuery.query.data?.data ?? [];
  const linkTypes = linkTypesQuery.query.data?.data ?? [];
  const activeObjectType = objectTypes.find((item) => item.id === activeId) ?? null;

  useEffect(() => {
    if (objectTypes.length === 0) {
      setActiveId(null);
      return;
    }
    if (!activeId || !objectTypes.some((item) => item.id === activeId)) {
      setActiveId(objectTypes[0].id);
    }
  }, [objectTypes, activeId]);

  const objectTypeName = (id: string) => objectTypes.find((item) => item.id === id)?.displayName ?? id;

  const openObjectTypeModal = () => {
    setObjectTypeForm(EMPTY_OBJECT_TYPE);
    setFormError(null);
    setObjectTypeOpen(true);
  };

  const openPropertyModal = () => {
    setPropertyForm(EMPTY_PROPERTY);
    setFormError(null);
    setPropertyOpen(true);
  };

  const handleCreateObjectType = async (event: React.FormEvent) => {
    event.preventDefault();
    setFormError(null);
    if (!objectTypeForm.name.trim() || !objectTypeForm.displayName.trim()) {
      setFormError(t("ontology.createFailed"));
      return;
    }
    try {
      const created = await createObjectType.mutateAsync({ values: objectTypeForm });
      setObjectTypeOpen(false);
      setNotice(t("ontology.createSuccess"));
      await objectTypesQuery.query.refetch();
      setActiveId(created.data.id);
    } catch (error) {
      setFormError(toErrorMessage(error, t("ontology.createFailed")));
    }
  };

  const handleCreateProperty = async (event: React.FormEvent) => {
    event.preventDefault();
    setFormError(null);
    if (!activeObjectType || !propertyForm.name.trim() || !propertyForm.displayName.trim()) {
      setFormError(t("ontology.propertyFailed"));
      return;
    }
    try {
      await createProperty.mutateAsync({
        values: {
          objectTypeId: activeObjectType.id,
          name: propertyForm.name,
          displayName: propertyForm.displayName,
          propertyType: propertyForm.propertyType,
          required: propertyForm.required,
          uniqueConstraint: propertyForm.unique,
        },
      });
      setPropertyOpen(false);
      setNotice(t("ontology.propertySaved"));
      await objectTypesQuery.query.refetch();
    } catch (error) {
      setFormError(toErrorMessage(error, t("ontology.propertyFailed")));
    }
  };

  return (
    <div className="page">
      <PageHeader
        eyebrow={t("app.stage")}
        title={t("ontology.title")}
        description={t("ontology.description")}
        actions={
          <button type="button" className="button button--primary" onClick={openObjectTypeModal}>
            <span aria-hidden="true">＋</span> {t("ontology.addObjectType")}
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

      <div className="tabs" role="tablist" aria-label={t("ontology.title")}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "object-types"}
          className={`tab${tab === "object-types" ? " tab--active" : ""}`}
          onClick={() => setTab("object-types")}
        >
          {t("ontology.objectTypes")}
          <span className="tab__count">{objectTypes.length}</span>
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "link-types"}
          className={`tab${tab === "link-types" ? " tab--active" : ""}`}
          onClick={() => setTab("link-types")}
        >
          {t("ontology.linkTypes")}
          <span className="tab__count">{linkTypes.length}</span>
        </button>
      </div>

      {tab === "object-types" ? (
        <div className="ontology-layout">
          <section className="card ontology-list">
            <div className="toolbar toolbar--compact">
              <label className="search-field search-field--sm">
                <span className="search-field__icon" aria-hidden="true">⌕</span>
                <input
                  type="search"
                  value={search}
                  placeholder={t("ontology.searchPlaceholder")}
                  aria-label={t("common.search")}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </label>
            </div>
            {objectTypesQuery.query.isLoading ? <LoadingBlock /> : null}
            {objectTypesQuery.query.isError ? (
              <ErrorBlock error={objectTypesQuery.query.error} onRetry={() => void objectTypesQuery.query.refetch()} />
            ) : null}
            {!objectTypesQuery.query.isLoading && objectTypes.length === 0 ? (
              <EmptyBlock title={t("ontology.emptyObjectTypes")} />
            ) : null}
            <div className="ontology-list__items">
              {objectTypes.map((objectType) => (
                <button
                  key={objectType.id}
                  type="button"
                  className={`ontology-item${objectType.id === activeId ? " ontology-item--active" : ""}`}
                  onClick={() => setActiveId(objectType.id)}
                >
                  <span className="ontology-item__glyph" aria-hidden="true">⬡</span>
                  <span className="ontology-item__body">
                    <strong>{objectType.displayName}</strong>
                    <code>{objectType.name}</code>
                  </span>
                  <span className="ontology-item__meta">{t("ontology.fieldCount", { count: objectType.properties.length })}</span>
                </button>
              ))}
            </div>
          </section>

          <section className="card ontology-detail">
            {!activeObjectType ? (
              <EmptyBlock title={t("ontology.selectHint")} />
            ) : (
              <>
                <header className="detail-header">
                  <div>
                    <div className="detail-header__eyebrow">
                      <span className="badge badge--violet">{t("ontology.objectTypes")}</span>
                      <code>{activeObjectType.name}</code>
                    </div>
                    <h2>{activeObjectType.displayName}</h2>
                    <p className="cell-muted">{activeObjectType.description || "—"}</p>
                  </div>
                  <button type="button" className="button button--secondary button--sm" onClick={openPropertyModal}>
                    ＋ {t("ontology.addProperty")}
                  </button>
                </header>
                <dl className="meta-grid">
                  <div>
                    <dt>{t("ontology.primaryKey")}</dt>
                    <dd><code>{activeObjectType.primaryKeyProperty || "—"}</code></dd>
                  </div>
                  <div>
                    <dt>{t("common.updatedAt")}</dt>
                    <dd>{formatDateTime(activeObjectType.updatedAt)}</dd>
                  </div>
                </dl>
                <h3 className="section-title">
                  {t("ontology.properties")}
                  <span className="section-title__count">{activeObjectType.properties.length}</span>
                </h3>
                {activeObjectType.properties.length === 0 ? (
                  <EmptyBlock title={t("ontology.emptyProperties")} />
                ) : (
                  <div className="table-wrap">
                    <table className="data-table data-table--compact">
                      <thead>
                        <tr>
                          <th>{t("ontology.propertyName")}</th>
                          <th>{t("ontology.propertyDisplayName")}</th>
                          <th>{t("ontology.propertyType")}</th>
                          <th>{t("ontology.required")}</th>
                          <th>{t("ontology.unique")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {activeObjectType.properties.map((property) => (
                          <tr key={property.id}>
                            <td><code>{property.name}</code></td>
                            <td>{property.displayName}</td>
                            <td><span className="badge badge--neutral">{property.propertyType}</span></td>
                            <td>{property.required ? "✓" : "—"}</td>
                            <td>{property.unique ? "✓" : "—"}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            )}
          </section>
        </div>
      ) : (
        <section className="card">
          {linkTypesQuery.query.isLoading ? <LoadingBlock /> : null}
          {linkTypesQuery.query.isError ? (
            <ErrorBlock error={linkTypesQuery.query.error} onRetry={() => void linkTypesQuery.query.refetch()} />
          ) : null}
          {!linkTypesQuery.query.isLoading && linkTypes.length === 0 ? <EmptyBlock title={t("ontology.noLinks")} /> : null}
          {linkTypes.length > 0 ? (
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>{t("common.displayName")}</th>
                    <th>{t("common.name")}</th>
                    <th>{t("ontology.source")}</th>
                    <th>{t("ontology.target")}</th>
                    <th>{t("common.updatedAt")}</th>
                  </tr>
                </thead>
                <tbody>
                  {linkTypes.map((linkType) => (
                    <tr key={linkType.id}>
                      <td><strong>{linkType.displayName}</strong></td>
                      <td><code>{linkType.name}</code></td>
                      <td>{objectTypeName(linkType.sourceTypeId)}</td>
                      <td>{objectTypeName(linkType.targetTypeId)}</td>
                      <td className="cell-muted">{formatDateTime(linkType.updatedAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </section>
      )}

      <Modal
        open={objectTypeOpen}
        title={t("ontology.createObjectType")}
        onClose={() => setObjectTypeOpen(false)}
        footer={
          <>
            <button type="button" className="button button--ghost" onClick={() => setObjectTypeOpen(false)}>
              {t("common.cancel")}
            </button>
            <button type="submit" form="create-object-type-form" className="button button--primary" disabled={createObjectType.mutation.isPending}>
              {createObjectType.mutation.isPending ? t("common.loading") : t("common.create")}
            </button>
          </>
        }
      >
        <form id="create-object-type-form" className="form" onSubmit={handleCreateObjectType}>
          {formError ? <div className="form-error" role="alert">{formError}</div> : null}
          <label className="field">
            <span>{t("ontology.name")}</span>
            <input
              required
              value={objectTypeForm.name}
              placeholder={t("ontology.namePlaceholder")}
              onChange={(event) => setObjectTypeForm({ ...objectTypeForm, name: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("ontology.displayName")}</span>
            <input
              required
              value={objectTypeForm.displayName}
              placeholder={t("ontology.displayNamePlaceholder")}
              onChange={(event) => setObjectTypeForm({ ...objectTypeForm, displayName: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("common.description")}</span>
            <textarea
              rows={3}
              value={objectTypeForm.description}
              placeholder={t("ontology.descriptionPlaceholder")}
              onChange={(event) => setObjectTypeForm({ ...objectTypeForm, description: event.target.value })}
            />
          </label>
        </form>
      </Modal>

      <Modal
        open={propertyOpen}
        title={t("ontology.addProperty")}
        onClose={() => setPropertyOpen(false)}
        footer={
          <>
            <button type="button" className="button button--ghost" onClick={() => setPropertyOpen(false)}>
              {t("common.cancel")}
            </button>
            <button type="submit" form="create-property-form" className="button button--primary" disabled={createProperty.mutation.isPending}>
              {createProperty.mutation.isPending ? t("common.loading") : t("common.save")}
            </button>
          </>
        }
      >
        <form id="create-property-form" className="form" onSubmit={handleCreateProperty}>
          {formError ? <div className="form-error" role="alert">{formError}</div> : null}
          <label className="field">
            <span>{t("ontology.propertyName")}</span>
            <input
              required
              value={propertyForm.name}
              placeholder={t("ontology.propertyNamePlaceholder")}
              onChange={(event) => setPropertyForm({ ...propertyForm, name: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("ontology.propertyDisplayName")}</span>
            <input
              required
              value={propertyForm.displayName}
              placeholder={t("ontology.propertyDisplayNamePlaceholder")}
              onChange={(event) => setPropertyForm({ ...propertyForm, displayName: event.target.value })}
            />
          </label>
          <label className="field">
            <span>{t("ontology.propertyType")}</span>
            <select
              value={propertyForm.propertyType}
              onChange={(event) => setPropertyForm({ ...propertyForm, propertyType: event.target.value })}
            >
              <option value="string">{t("ontology.propertyTypeString")}</option>
              <option value="number">{t("ontology.propertyTypeNumber")}</option>
              <option value="boolean">{t("ontology.propertyTypeBoolean")}</option>
              <option value="datetime">{t("ontology.propertyTypeDatetime")}</option>
            </select>
          </label>
          <div className="field-row">
            <label className="checkbox">
              <input
                type="checkbox"
                checked={propertyForm.required}
                onChange={(event) => setPropertyForm({ ...propertyForm, required: event.target.checked })}
              />
              <span>{t("ontology.required")}</span>
            </label>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={propertyForm.unique}
                onChange={(event) => setPropertyForm({ ...propertyForm, unique: event.target.checked })}
              />
              <span>{t("ontology.unique")}</span>
            </label>
          </div>
        </form>
      </Modal>
    </div>
  );
}
