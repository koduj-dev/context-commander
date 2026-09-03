import { Link, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { useTheme, type Theme } from "../theme/ThemeContext";

const THEMES: { value: Theme; label: string; icon: string }[] = [
  { value: "system", label: "System theme", icon: "◐" },
  { value: "light", label: "Light theme", icon: "☀" },
  { value: "dark", label: "Dark theme", icon: "☾" },
];

export default function Shell() {
  const { user, logout } = useAuth();
  const { theme, setTheme, resolvedTheme } = useTheme();
  const navigate = useNavigate();

  function onLogout() {
    logout();
    navigate("/login", { replace: true });
  }

  return (
    <div className="grid min-h-screen grid-cols-[220px_1fr] bg-cc-bg text-cc-text">
      <aside className="flex flex-col border-r border-cc-border p-4">
        <Link to="/" className="mb-6">
          <img
            src={resolvedTheme === "dark" ? "/brand/cc-logo-horizontal-on-dark.svg" : "/brand/cc-logo-horizontal-on-light.svg"}
            alt="Context Commander"
            className="h-6"
          />
        </Link>
        <nav className="flex flex-col gap-1 text-sm text-cc-text-secondary">
          <Link to="/" className="rounded px-2 py-1.5 hover:bg-cc-surface-2">
            Dashboard
          </Link>
          <Link to="/tokens" className="rounded px-2 py-1.5 hover:bg-cc-surface-2">
            API Tokens
          </Link>
          {user?.is_installation_admin && (
            <Link to="/administration" className="rounded px-2 py-1.5 hover:bg-cc-surface-2">
              Administration
            </Link>
          )}
        </nav>
        <div className="mt-auto pt-4 text-xs text-cc-text-muted">
          <div className="mb-2 flex gap-1">
            {THEMES.map((t) => (
              <button
                key={t.value}
                title={t.label}
                aria-label={t.label}
                aria-pressed={theme === t.value}
                onClick={() => setTheme(t.value)}
                className={`flex h-6 w-6 items-center justify-center rounded ${
                  theme === t.value ? "bg-cc-surface-3 text-cc-text" : "hover:bg-cc-surface-2"
                }`}
              >
                {t.icon}
              </button>
            ))}
          </div>
          <div className="mb-2 truncate">{user?.email ?? user?.name}</div>
          <button onClick={onLogout} className="rounded px-2 py-1 hover:bg-cc-surface-2 hover:text-cc-text">
            Sign out
          </button>
        </div>
      </aside>
      <main className="overflow-y-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
