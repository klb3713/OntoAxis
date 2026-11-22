import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { PageHeader } from "../../components/PageHeader";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <div className="page">
      <PageHeader title={t("notFound.title")} description={t("notFound.description")} />
      <section className="card">
        <div className="empty-state">
          <p className="empty-state__code">404</p>
          <Link className="button button--primary" to="/projects">{t("notFound.back")}</Link>
        </div>
      </section>
    </div>
  );
}
