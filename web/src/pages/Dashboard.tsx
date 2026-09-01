import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, type MyOrganization, type MyProject } from "../lib/api";

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
      {error && <p className="rounded border border-red-900 bg-red-950/50 px-3 py-2 text-sm text-red-300">{error}</p>}

      <section>
        <h2 className="mb-3 text-lg font-semibold">Projects</h2>
        <ul className="mb-4 divide-y divide-neutral-800 rounded border border-neutral-800">
          {projects === null && <li className="px-3 py-2 text-sm text-neutral-500">Loading…</li>}
          {projects?.length === 0 && <li className="px-3 py-2 text-sm text-neutral-500">No projects yet.</li>}
          {projects?.map((p) => (
            <li key={`${p.namespace}/${p.slug}`}>
              <Link
                to={`/projects/${p.namespace}/${p.slug}`}
                className="flex items-center justify-between px-3 py-2 text-sm hover:bg-neutral-900"
              >
                <span>
                  {p.namespace}/{p.slug} <span className="text-neutral-500">— {p.name}</span>
                </span>
                <span className="text-xs text-neutral-500">{p.role}</span>
              </Link>
            </li>
          ))}
        </ul>
        <form onSubmit={onCreateProject} className="flex gap-2">
          <input
            value={newProjectPath}
            onChange={(e) => setNewProjectPath(e.target.value)}
            placeholder="namespace/slug"
            className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
          <input
            value={newProjectName}
            onChange={(e) => setNewProjectName(e.target.value)}
            placeholder="Name"
            className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
          <button
            type="submit"
            disabled={!newProjectPath.trim() || !newProjectName.trim()}
            className="rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Create
          </button>
        </form>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-semibold">Organizations</h2>
        <ul className="mb-4 divide-y divide-neutral-800 rounded border border-neutral-800">
          {orgs === null && <li className="px-3 py-2 text-sm text-neutral-500">Loading…</li>}
          {orgs?.length === 0 && <li className="px-3 py-2 text-sm text-neutral-500">No organizations yet.</li>}
          {orgs?.map((o) => (
            <li key={o.id}>
              <Link to={`/organizations/${o.id}`} className="flex items-center justify-between px-3 py-2 text-sm hover:bg-neutral-900">
                <span>{o.name}</span>
                <span className="text-xs text-neutral-500">{o.role}</span>
              </Link>
            </li>
          ))}
        </ul>
        <form onSubmit={onCreateOrganization} className="flex gap-2">
          <input
            value={newOrgName}
            onChange={(e) => setNewOrgName(e.target.value)}
            placeholder="Organization name"
            className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
          />
          <button
            type="submit"
            disabled={!newOrgName.trim()}
            className="rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Create
          </button>
        </form>
      </section>
    </div>
  );
}
