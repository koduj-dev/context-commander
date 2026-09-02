import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth/AuthContext";
import Shell from "./layout/Shell";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import ProjectDetail from "./pages/ProjectDetail";
import OrganizationDetail from "./pages/OrganizationDetail";
import AccountTokens from "./pages/AccountTokens";

export default function App() {
  const { loading, user } = useAuth();

  if (loading) {
    return <div className="flex min-h-screen items-center justify-center bg-neutral-950 text-neutral-400">Loading…</div>;
  }

  if (!user) {
    return (
      <Routes>
        <Route path="*" element={<Login />} />
      </Routes>
    );
  }

  return (
    <Routes>
      <Route element={<Shell />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/projects/:namespace/:slug" element={<ProjectDetail />} />
        <Route path="/organizations/:id" element={<OrganizationDetail />} />
        <Route path="/tokens" element={<AccountTokens />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
