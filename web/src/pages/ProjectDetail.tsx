import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, ApiError, type MyProject } from "../lib/api";
import { canReadAudit } from "../lib/roles";
import ProjectContextHeader from "../components/ProjectContextHeader";
import Members from "./project/Members";
import Artifacts from "./project/Artifacts";
import Secrets from "./project/Secrets";
import Skills from "./project/Skills";
import Gates from "./project/Gates";
import Audit from "./project/Audit";

const TABS = ["Members", "Artifacts", "Secrets", "Skills", "Gates", "Audit"] as const;
type Tab = (typeof TABS)[number];

export default function ProjectDetail() {
  const { namespace = "", slug = "" } = useParams();
  const [project, setProject] = useState<MyProject | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("Artifacts");

  useEffect(() => {
    // /v1/me/projects carries the caller's role, which GET /v1/projects/... doesn't —
    // and the role is what gates the Members/Audit tabs, so this is cheaper than
    // adding a role field to getProject just for the UI.
    api
      .myProjects()
      .then((list) => {
        const found = list.find((p) => p.namespace === namespace && p.slug === slug);
        setProject(found ?? null);
        if (!found) setError("you are not a member of this project");
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "failed to load project"));
  }, [namespace, slug]);

  if (error) return <p className="text-sm text-cc-danger">{error}</p>;
  if (!project) return <p className="text-sm text-cc-text-muted">Loading…</p>;

  const visibleTabs = TABS.filter((t) => t !== "Audit" || canReadAudit(project.role));
  const visibleTab = visibleTabs.includes(tab) ? tab : visibleTabs[0];

  return (
    <div>
      <ProjectContextHeader namespace={namespace} slug={slug} name={project.name} visibility={project.visibility} role={project.role} />
      <div className="mb-4 flex gap-1 border-b border-cc-border">
        {visibleTabs.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-3 py-2 text-sm ${
              visibleTab === t ? "border-b-2 border-cc-accent-primary text-cc-text" : "text-cc-text-muted hover:text-cc-text-secondary"
            }`}
          >
            {t}
          </button>
        ))}
      </div>
      {visibleTab === "Members" && <Members namespace={namespace} slug={slug} role={project.role} />}
      {visibleTab === "Artifacts" && <Artifacts namespace={namespace} slug={slug} />}
      {visibleTab === "Secrets" && <Secrets namespace={namespace} slug={slug} />}
      {visibleTab === "Skills" && <Skills namespace={namespace} slug={slug} role={project.role} />}
      {visibleTab === "Gates" && <Gates namespace={namespace} slug={slug} role={project.role} />}
      {visibleTab === "Audit" && <Audit namespace={namespace} slug={slug} />}
    </div>
  );
}
