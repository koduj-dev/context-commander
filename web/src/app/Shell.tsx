import { NavLink, Outlet, Link } from "react-router-dom";
import { useI18n, useT } from "../i18n";
import { useAuth } from "./auth";
import { useTheme } from "./theme";

/**
 * The frame every screen sits in.
 *
 * The sidebar keeps its items visible at every width — below the narrow
 * breakpoint it becomes a horizontal strip rather than collapsing into a
 * menu. Hiding navigation behind a button would hide the one thing that must
 * stay obvious: which scope you are looking at.
 */

type IconProps = { d: string };
function Icon({ d }: IconProps) {
  return (
    <svg className="ico" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={d} />
    </svg>
  );
}

const ICONS = {
  dashboard: "M3 9.5 9 4l6 5.5M4.5 8.5V15h9V8.5",
  projects: "M2 5.5h5l1.2 1.5H16V14H2zM2 5.5V3.5h4l1 1.5",
  organizations: "M3 15V4.5h5V15M11 15V7.5h4V15M2 15h14M5 7h1M5 9.5h1M13 10h1",
  registry: "M4 3.5h8l2 2.5v9H4zM6 7h6M6 9.5h6M6 12h4",
  administration: "M9 2.5 3.5 5v4.2c0 3 2.3 5.2 5.5 6.3 3.2-1.1 5.5-3.3 5.5-6.3V5z M6.8 9l1.6 1.6L11.6 7",
};

function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}

export function Shell() {
  const t = useT();
  const { locale, setLocale } = useI18n();
  const { theme, setTheme } = useTheme();
  const { me } = useAuth();

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <Link to="/">
            <img
              className="logo-dark"
              src="/brand/cc-logo-horizontal-on-dark.svg"
              alt={t("app.name")}
              hidden={theme === "light"}
            />
            <img
              className="logo-light"
              src="/brand/cc-logo-horizontal-on-light.svg"
              alt={t("app.name")}
              hidden={theme !== "light"}
            />
          </Link>
        </div>

        <nav className="nav" aria-label={t("nav.workspace")}>
          <div className="nav-label">{t("nav.workspace")}</div>
          <NavLink to="/" end className={({ isActive }) => (isActive ? "active" : "")}>
            <Icon d={ICONS.dashboard} />
            <span>{t("nav.dashboard")}</span>
          </NavLink>
          <NavLink to="/projects" className={({ isActive }) => (isActive ? "active" : "")}>
            <Icon d={ICONS.projects} />
            <span>{t("nav.projects")}</span>
          </NavLink>
          <NavLink to="/registry" className={({ isActive }) => (isActive ? "active" : "")}>
            <Icon d={ICONS.registry} />
            <span>{t("nav.registry")}</span>
          </NavLink>

          {me?.is_installation_admin && (
            <>
              <div className="nav-label">{t("nav.installation")}</div>
              <NavLink to="/administration" className={({ isActive }) => (isActive ? "active" : "")}>
                <Icon d={ICONS.administration} />
                <span>{t("nav.administration")}</span>
              </NavLink>
            </>
          )}
        </nav>

        <Link className="sidebar-foot" to="/preferences">
          <div className="avatar" aria-hidden="true">
            {initials(me?.display_name ?? "?")}
          </div>
          <div className="who">
            <b>{me?.display_name}</b>
            <span>{me?.email}</span>
          </div>
        </Link>
      </aside>

      <div className="main">
        <div className="topbar">
          <div className="crumbs" id="crumbs" />
          <div className="spacer" />
          <span className="seg" role="group" aria-label={t("prefs.language")}>
            {(["en", "de", "es"] as const).map((l) => (
              <button key={l} type="button" aria-pressed={l === locale} onClick={() => setLocale(l)}>
                {l.toUpperCase()}
              </button>
            ))}
          </span>
          <span className="seg" role="group" aria-label={t("theme.label")}>
            <button type="button" aria-pressed={theme === "dark"} onClick={() => setTheme("dark")}>
              {t("theme.dark")}
            </button>
            <button type="button" aria-pressed={theme === "light"} onClick={() => setTheme("light")}>
              {t("theme.light")}
            </button>
          </span>
        </div>
        <main className="page">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
