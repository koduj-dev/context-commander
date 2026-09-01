import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, ApiError, type MyProject } from "../lib/api";
import { canReadAudit } from "../lib/roles";
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

  if (error) return <p className="text-sm text-red-400">{error}</p>;
  if (!project) return <p className="text-sm text-neutral-500">Loading…</p>;

  const visibleTabs = TABS.filter((t) => t !== "Audit" || canReadAudit(project.role));
  const visibleTab = visibleTabs.includes(tab) ? tab : visibleTabs[0];

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-lg font-semibold">
          {namespace}/{slug}
        </h1>
        <p className="text-sm text-neutral-500">
          {project.name} · your role: {project.role}
        </p>
      </div>
      <div className="mb-4 flex gap-1 border-b border-neutral-800">
        {visibleTabs.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-3 py-2 text-sm ${
              visibleTab === t ? "border-b-2 border-neutral-100 text-neutral-100" : "text-neutral-500 hover:text-neutral-300"
            }`}
          >
            {t}
          </button>
        ))}
      </div>
      {visibleTab === "Members" && <Members namespace={namespace} slug={slug} role={project.role} />}
      {visibleTab === "Artifacts" && <Artifacts namespace={namespace} slug={slug} />}
      {visibleTab === "Secrets" && <Secrets namespace={namespace} slug={slug} />}
      {visibleTab === "Skills" && <Skills namespace={namespace} slug={slug} />}
      {visibleTab === "Gates" && <Gates namespace={namespace} slug={slug} />}
      {visibleTab === "Audit" && <Audit namespace={namespace} slug={slug} />}
    </div>
  );
}
