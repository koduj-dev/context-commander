import { createContext, useContext } from "react";
import { NavLink, Outlet, useParams } from "react-router-dom";
import { useT } from "../i18n";
import { api, type Project } from "../lib/api";
import { useResource } from "../lib/resource";
import { Crumbs, ErrorState, Loading, Mono, PageHead, Tabs } from "../ui";

/**
 * The frame around every project screen.
 *
 * It loads the project once and hands it to the tabs through context, so
 * switching tabs does not re-fetch the same row four times — and so a 403 on
 * the project itself is answered once, here, instead of four times in four
 * different shapes.
 */

type Ctx = { project: Project; namespace: string; slug: string };
const ProjectCtx = createContext<Ctx | null>(null);

export function useProject(): Ctx {
  const ctx = useContext(ProjectCtx);
  if (!ctx) throw new Error("useProject must be used inside a project route");
  return ctx;
}

export function ProjectLayout() {
  const t = useT();
  const { namespace = "", slug = "" } = useParams();
  const { data, error, loading, reload } = useResource(
    () => api.project(namespace, slug),
    [namespace, slug],
  );

  if (loading) return <Loading />;
  if (error || !data) return <ErrorState error={error} onRetry={reload} />;

  const base = `/p/${namespace}/${slug}`;
  const tab = (to: string, label: string, end = false) => (
    <NavLink to={to} end={end} className={({ isActive }) => (isActive ? "active" : "")}>
      {label}
    </NavLink>
  );

  return (
    <ProjectCtx.Provider value={{ project: data, namespace, slug }}>
      <Crumbs trail={[t("nav.projects"), <Mono key="ns">{namespace}</Mono>]} current={data.name} />

      <PageHead title={data.name} lead={<Mono dim>{`${namespace}/${slug}`}</Mono>} />

      <Tabs label={data.name}>
        {tab(base, t("project.tab.overview"), true)}
        {tab(`${base}/artifacts`, t("artifacts.title"))}
        {tab(`${base}/skills`, t("project.tab.skills"))}
        {tab(`${base}/policies`, t("gates.title"))}
        {tab(`${base}/secrets`, t("secrets.title"))}
        {tab(`${base}/members`, t("project.tab.members"))}
        {tab(`${base}/activity`, t("project.tab.activity"))}
      </Tabs>

      <Outlet />
    </ProjectCtx.Provider>
  );
}
