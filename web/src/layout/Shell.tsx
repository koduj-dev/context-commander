import { Link, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

export default function Shell() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  function onLogout() {
    logout();
    navigate("/login", { replace: true });
  }

  return (
    <div className="grid min-h-screen grid-cols-[220px_1fr] bg-neutral-950 text-neutral-100">
      <aside className="flex flex-col border-r border-neutral-800 p-4">
        <Link to="/" className="mb-6 text-sm font-semibold tracking-tight">
          Context Commander
        </Link>
        <nav className="flex flex-col gap-1 text-sm text-neutral-300">
          <Link to="/" className="rounded px-2 py-1.5 hover:bg-neutral-900">
            Dashboard
          </Link>
        </nav>
        <div className="mt-auto pt-4 text-xs text-neutral-500">
          <div className="mb-2 truncate">{user?.email ?? user?.name}</div>
          <button onClick={onLogout} className="rounded px-2 py-1 hover:bg-neutral-900 hover:text-neutral-200">
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
