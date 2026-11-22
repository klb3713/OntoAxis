import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import type { SupportedLocale } from "../config/runtime";
import { useAuthRuntime } from "../auth/AuthContext";
import { loadSession } from "../auth/session";
import { currentLocale, setLocale } from "../i18n";

type NavItem = {
  to: string;
  labelKey: string;
  icon: ReactNode;
};

const iconProps = {
  width: 18,
  height: 18,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
};

const NAV_ITEMS: NavItem[] = [
  {
    to: "/projects",
    labelKey: "nav.projects",
    icon: (
      <svg {...iconProps}>
        <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H10l2 2h6.5A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5z" />
      </svg>
    ),
  },
  {
    to: "/ontology",
    labelKey: "nav.ontology",
    icon: (
      <svg {...iconProps}>
        <circle cx="12" cy="5" r="2.2" />
        <circle cx="5" cy="18" r="2.2" />
        <circle cx="19" cy="18" r="2.2" />
        <path d="M12 7.4v3.1M5 15.8v-2.2a3 3 0 0 1 3-3h8a3 3 0 0 1 3 3v2.2" />
      </svg>
    ),
  },
  {
    to: "/object-explorer",
    labelKey: "nav.objectExplorer",
    icon: (
      <svg {...iconProps}>
        <circle cx="11" cy="11" r="6.5" />
        <path d="m16 16 4 4M8.5 11h5M11 8.5v5" />
      </svg>
    ),
  },
  {
    to: "/datasets",
    labelKey: "nav.datasets",
    icon: (
      <svg {...iconProps}>
        <ellipse cx="12" cy="5.5" rx="7" ry="2.8" />
        <path d="M5 5.5v6c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8v-6M5 11.5v6c0 1.5 3.1 2.8 7 2.8s7-1.3 7-2.8v-6" />
      </svg>
    ),
  },
  {
    to: "/queries",
    labelKey: "nav.queries",
    icon: (
      <svg {...iconProps}>
        <path d="m8 8-4 4 4 4M16 8l4 4-4 4M14 5l-4 14" />
      </svg>
    ),
  },
];

function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      <span />
      <span />
      <span />
    </span>
  );
}

export function AppLayout() {
  const { t } = useTranslation();
  const { config, authAdapter } = useAuthRuntime();
  const [session, setSession] = useState(() => loadSession());
  const navigate = useNavigate();
  const location = useLocation();

  const activeItem = [...NAV_ITEMS]
    .sort((a, b) => b.to.length - a.to.length)
    .find((item) => location.pathname === item.to || location.pathname.startsWith(`${item.to}/`));

  const handleLogout = async () => {
    await authAdapter.logout();
    setSession(null);
    if (config.authMode === "mock") navigate("/login", { replace: true });
  };

  const handleLocaleChange = (locale: SupportedLocale) => {
    void setLocale(locale);
  };

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <BrandMark />
          <div>
            <div className="brand__name">{t("app.name")}</div>
            <div className="brand__tagline">{t("app.tagline")}</div>
          </div>
        </div>

        <nav className="sidebar__nav" aria-label={t("nav.sectionMain")}>
          <p className="sidebar__section">{t("nav.sectionMain")}</p>
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) => `nav-link${isActive ? " nav-link--active" : ""}`}
            >
              {item.icon}
              <span>{t(item.labelKey)}</span>
            </NavLink>
          ))}
        </nav>

        <div className="sidebar__footer">
          <div className="runtime-badges">
            <span className={`badge ${config.apiMode === "mock" ? "badge--amber" : "badge--green"}`}>
              {config.apiMode === "mock" ? t("layout.mockBadge") : t("layout.httpBadge")}
            </span>
            <span className="badge badge--neutral">
              {config.authMode === "oidc" ? t("layout.oidcBadge") : t("layout.mockAuthBadge")}
            </span>
          </div>
          <p className="sidebar__stage">{t("app.stage")}</p>
        </div>
      </aside>

      <div className="main-column">
        <header className="topbar">
          <div>
            <p className="topbar__eyebrow">{t("nav.sectionMain")}</p>
            <h1 className="topbar__title">{activeItem ? t(activeItem.labelKey) : t("app.name")}</h1>
          </div>
          <div className="topbar__actions">
            <label className="locale-select">
              <span className="sr-only">{t("layout.language")}</span>
              <select
                aria-label={t("layout.language")}
                value={currentLocale()}
                onChange={(event) => handleLocaleChange(event.target.value as SupportedLocale)}
              >
                <option value="zh-CN">简体中文</option>
                <option value="en">English</option>
              </select>
            </label>
            {session ? (
              <div className="user-chip">
                <span className="user-chip__avatar" aria-hidden="true">
                  {session.user.displayName.slice(0, 1).toUpperCase()}
                </span>
                <span className="user-chip__meta">
                  <strong>{session.user.displayName}</strong>
                  <small>{session.user.email}</small>
                </span>
              </div>
            ) : null}
            <button type="button" className="button button--ghost button--sm" onClick={handleLogout}>
              {t("layout.logout")}
            </button>
          </div>
        </header>

        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
