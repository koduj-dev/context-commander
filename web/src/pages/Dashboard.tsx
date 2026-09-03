import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, type MyOrganization, type MyProject } from "../lib/api";
import EmptyState from "../components/EmptyState";

export default function Dashboard() {
  const [projects, setProjects] = useState<MyProject[] | null>(null);
  const [orgs, setOrgs] = useState<MyOrganization[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [newProjectPath, setNewProjectPath] = useState("");
  const [newProjectName, setNewProjectName] = useState("");
  const [newOrgName, setNewOrgName] = useState("");

  async function refresh() {
    try {
      const [p, o] = await Promise.all([api.myProjects(), api.myOrganizations()]);
      setProjects(p);
      setOrgs(o);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load dashboard");
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function onCreateProject(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createProject(newProjectPath.trim(), newProjectName.trim());
      setNewProjectPath("");
      setNewProjectName("");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create project");
    }
  }

  async function onCreateOrganization(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.createOrganization(newOrgName.trim());
      setNewOrgName("");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to create organization");
    }
  }

  return (
    <div className="mx-auto max-w-3xl space-y-10">
      {error && <p className="rounded border border-cc-danger/40 bg-cc-surface-1 px-3 py-2 text-sm text-cc-danger">{error}</p>}

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Projects</h2>
        <ul className="mb-4 divide-y divide-cc-border rounded border border-cc-border">
          {projects === null && (
            <li>
              <EmptyState message="Loading…" />
            </li>
          )}
          {projects?.length === 0 && (
            <li>
              <EmptyState message="No projects yet." />
            </li>
          )}
          {projects?.map((p) => (
            <li key={`${p.namespace}/${p.slug}`}>
              <Link
                to={`/projects/${p.namespace}/${p.slug}`}
                className="flex items-center justify-between px-3 py-2 text-sm hover:bg-cc-surface-2"
              >
                <span>
                  {p.namespace}/{p.slug} <span className="text-cc-text-muted">— {p.name}</span>
                </span>
                <span className="text-xs text-cc-text-muted">{p.role}</span>
              </Link>
            </li>
          ))}
        </ul>
        <form onSubmit={onCreateProject} className="flex gap-2">
          <input
            value={newProjectPath}
            onChange={(e) => setNewProjectPath(e.target.value)}
            placeholder="namespace/slug"
            className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <input
            value={newProjectName}
            onChange={(e) => setNewProjectName(e.target.value)}
            placeholder="Name"
            className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <button
            type="submit"
            disabled={!newProjectPath.trim() || !newProjectName.trim()}
            className="rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Create
          </button>
        </form>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-semibold text-cc-text">Organizations</h2>
        <ul className="mb-4 divide-y divide-cc-border rounded border border-cc-border">
          {orgs === null && (
            <li>
              <EmptyState message="Loading…" />
            </li>
          )}
          {orgs?.length === 0 && (
            <li>
              <EmptyState message="No organizations yet." />
            </li>
          )}
          {orgs?.map((o) => (
            <li key={o.id}>
              <Link to={`/organizations/${o.id}`} className="flex items-center justify-between px-3 py-2 text-sm hover:bg-cc-surface-2">
                <span>{o.name}</span>
                <span className="text-xs text-cc-text-muted">{o.role}</span>
              </Link>
            </li>
          ))}
        </ul>
        <form onSubmit={onCreateOrganization} className="flex gap-2">
          <input
            value={newOrgName}
            onChange={(e) => setNewOrgName(e.target.value)}
            placeholder="Organization name"
            className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <button
            type="submit"
            disabled={!newOrgName.trim()}
            className="rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Create
          </button>
        </form>
      </section>
    </div>
  );
}
