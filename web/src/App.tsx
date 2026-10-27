import { Route, Routes } from "react-router-dom";
import { Shell } from "./app/Shell";
import { useAuth } from "./app/auth";
import { Solo } from "./app/Solo";
import { useT } from "./i18n";
import { Btn, Loading, Panel } from "./ui";
import { FirstRun } from "./pages/FirstRun";
import { SignIn } from "./pages/SignIn";
import { Dashboard } from "./pages/Dashboard";
import { Projects } from "./pages/Projects";
import { ProjectLayout } from "./pages/ProjectLayout";
import { ProjectOverview } from "./pages/ProjectOverview";
import { ProjectMembers } from "./pages/ProjectMembers";
import { ProjectArtifacts } from "./pages/ProjectArtifacts";
import { ProjectContext } from "./pages/ProjectContext";
import { ProjectSecrets } from "./pages/ProjectSecrets";
import { ProjectPolicies } from "./pages/ProjectPolicies";
import { ProjectSkills } from "./pages/ProjectSkills";
import { ProjectActivity } from "./pages/ProjectActivity";
import { ProjectHistory } from "./pages/ProjectHistory";
import { ProjectSettings } from "./pages/ProjectSettings";
import { Registry } from "./pages/Registry";
import { SkillDetail } from "./pages/SkillDetail";
import { Organizations } from "./pages/Organizations";
import { Tokens } from "./pages/Tokens";
import { AdminUsage } from "./pages/AdminUsage";
import { AdminStorage } from "./pages/AdminStorage";
import {
  Administration,
  AdminPeople,
  AdminAgents,
  AdminOverview,
  AdminSecurityLog,
} from "./pages/Administration";
import { Preferences } from "./pages/Preferences";
import { NotFound } from "./pages/NotFound";

/**
 * Routes.
 *
 * A project is addressed the way people say it — /p/:namespace/:slug — not by
 * UUID. The UUID is what the system means and is shown on the overview, but a
 * URL someone pastes into a chat should be readable by whoever receives it.
 */
export function App() {
  const t = useT();
  const { me, loading, needsSetup, unreachable, retry } = useAuth();

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
  // The server could not say who this is. Not a refusal, and so not a login
  // form: asking for a password that was never the problem would fail again
  // for a reason the form has no way to explain.
  if (unreachable) {
    return (
      <Solo>
        <Panel title={t("error.unavailable.title")}>
          <p className="sec lead-out">{t("error.unavailable.body")}</p>
          <Btn kind="primary" onClick={retry}>
            {t("common.retry")}
          </Btn>
        </Panel>
      </Solo>
    );
  }

  // A fresh installation has no users, so there is nobody to sign in as yet;
  // sending someone to a login form would be asking for a credential that
  // cannot exist (docs/adr/0019).
  if (!me) return needsSetup ? <FirstRun /> : <SignIn />;

  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Dashboard />} />
        <Route path="projects" element={<Projects />} />
        <Route path="p/:namespace/:slug" element={<ProjectLayout />}>
          <Route index element={<ProjectOverview />} />
          <Route path="context" element={<ProjectContext />} />
          <Route path="artifacts" element={<ProjectArtifacts />} />
          <Route path="skills" element={<ProjectSkills />} />
          <Route path="policies" element={<ProjectPolicies />} />
          <Route path="secrets" element={<ProjectSecrets />} />
          <Route path="members" element={<ProjectMembers />} />
          <Route path="history" element={<ProjectHistory />} />
          <Route path="activity" element={<ProjectActivity />} />
          <Route path="settings" element={<ProjectSettings />} />
        </Route>
        <Route path="organizations" element={<Organizations />} />
        <Route path="registry" element={<Registry />} />
        <Route path="registry/:skillID" element={<SkillDetail />} />
        <Route path="administration" element={<Administration />}>
          <Route index element={<AdminOverview />} />
          <Route path="people" element={<AdminPeople />} />
          <Route path="agents" element={<AdminAgents />} />
          <Route path="usage" element={<AdminUsage />} />
          <Route path="storage" element={<AdminStorage />} />
          <Route path="security" element={<AdminSecurityLog />} />
        </Route>
        <Route path="tokens" element={<Tokens />} />
        <Route path="preferences" element={<Preferences />} />
        {/* Said out loud rather than redirected: a silent bounce to the
            dashboard makes a stale link look like it worked. */}
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}
