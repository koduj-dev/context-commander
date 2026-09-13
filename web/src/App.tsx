import { Navigate, Route, Routes } from "react-router-dom";
import { Shell } from "./app/Shell";
import { useAuth } from "./app/auth";
import { Loading } from "./ui";
import { SignIn } from "./pages/SignIn";
import { Dashboard } from "./pages/Dashboard";
import { Projects } from "./pages/Projects";
import { ProjectLayout } from "./pages/ProjectLayout";
import { ProjectOverview } from "./pages/ProjectOverview";
import { ProjectMembers } from "./pages/ProjectMembers";
import { ProjectArtifacts } from "./pages/ProjectArtifacts";
import { ProjectSecrets } from "./pages/ProjectSecrets";
import { ProjectPolicies } from "./pages/ProjectPolicies";
import { ProjectSkills } from "./pages/ProjectSkills";
import { ProjectActivity } from "./pages/ProjectActivity";
import { Registry } from "./pages/Registry";
import { SkillDetail } from "./pages/SkillDetail";
import { Organizations } from "./pages/Organizations";
import { Tokens } from "./pages/Tokens";
import {
  Administration,
  AdminIdentities,
  AdminOverview,
  AdminSecurityLog,
} from "./pages/Administration";
import { Preferences } from "./pages/Preferences";

/**
 * Routes.
 *
 * A project is addressed the way people say it — /p/:namespace/:slug — not by
 * UUID. The UUID is what the system means and is shown on the overview, but a
 * URL someone pastes into a chat should be readable by whoever receives it.
 */
export function App() {
  const { me, loading } = useAuth();

  // Nothing renders until the stored token has been checked. Showing the
  // shell first and yanking it away is worse than a moment of nothing — and
  // it goes in the same sidebar-less layout sign-in uses, since at this point
  // there is still no principal and so nothing to navigate.
  if (loading) {
    return (
      <div className="solo">
        <main className="page">
          <Loading />
        </main>
      </div>
    );
  }
  if (!me) return <SignIn />;

  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Dashboard />} />
        <Route path="projects" element={<Projects />} />
        <Route path="p/:namespace/:slug" element={<ProjectLayout />}>
          <Route index element={<ProjectOverview />} />
          <Route path="artifacts" element={<ProjectArtifacts />} />
          <Route path="skills" element={<ProjectSkills />} />
          <Route path="policies" element={<ProjectPolicies />} />
          <Route path="secrets" element={<ProjectSecrets />} />
          <Route path="members" element={<ProjectMembers />} />
          <Route path="activity" element={<ProjectActivity />} />
        </Route>
        <Route path="organizations" element={<Organizations />} />
        <Route path="registry" element={<Registry />} />
        <Route path="registry/:skillID" element={<SkillDetail />} />
        <Route path="administration" element={<Administration />}>
          <Route index element={<AdminOverview />} />
          <Route path="identities" element={<AdminIdentities />} />
          <Route path="security" element={<AdminSecurityLog />} />
        </Route>
        <Route path="tokens" element={<Tokens />} />
        <Route path="preferences" element={<Preferences />} />
        {/* An unknown path is not an error worth a screen of its own here —
            the dashboard is always a valid place to be. */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
