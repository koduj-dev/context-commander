import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type GateBinding } from "../../lib/api";
import { canManageGates } from "../../lib/roles";

const SCOPES = ["project", "organization"];
const HOOKS = [
  "artifact.before_write",
  "artifact.before_read",
  "artifact.after_write",
  "artifact.after_read",
  "artifact.before_delete",
  "handoff.before_create",
  "handoff.after_create",
];

export default function Gates({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [bindings, setBindings] = useState<GateBinding[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [projectId, setProjectId] = useState<string | null>(null);
  const [scope, setScope] = useState("project");
  const [scopeRef, setScopeRef] = useState("");
  const [gateName, setGateName] = useState("secret-scanner");
  const [hook, setHook] = useState(HOOKS[0]);
  const [config, setConfig] = useState("{}");

  async function refresh() {
    try {
      setBindings(await api.listGateBindings(namespace, slug));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load gates");
    }
  }

  useEffect(() => {
    void refresh();
    api
      .getProject(namespace, slug)
      .then((p) => setProjectId(p.id))
      .catch(() => {
        // best-effort — only used to pre-fill scope_ref for "project" scope
      });
  }, [namespace, slug]);

  useEffect(() => {
    if (scope === "project" && projectId) setScopeRef(projectId);
  }, [scope, projectId]);

  async function onBind(e: FormEvent) {
    e.preventDefault();
    setError(null);
    let parsedConfig: unknown;
    try {
      parsedConfig = config.trim() ? JSON.parse(config) : {};
    } catch {
      setError("config must be valid JSON");
      return;
    }
    try {
      await api.bindGate(namespace, slug, {
        scope,
        scope_ref: scopeRef.trim() || undefined,
        gate_name: gateName.trim(),
        hook,
        config: parsedConfig,
      });
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to bind gate");
    }
  }

  async function onUnbind(id: string) {
    setError(null);
    try {
      await api.unbindGate(namespace, slug, id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to unbind gate");
    }
  }

  const canManage = canManageGates(role);

  return (
    <div className="max-w-2xl space-y-4">
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {bindings === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {bindings?.length === 0 && <li className="px-3 py-2 text-neutral-500">No gates bound.</li>}
        {bindings?.map((g) => (
          <li key={g.id} className="flex items-center justify-between px-3 py-2">
            <span>
              {g.gate_name} <span className="text-xs text-neutral-500">({g.scope})</span>
            </span>
            <span className="flex items-center gap-3 text-xs text-neutral-500">
              {g.hook}
              {canManage && (
                <button onClick={() => void onUnbind(g.id)} className="text-red-400 hover:text-red-300">
                  Unbind
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>
      {canManage && (
        <form onSubmit={onBind} className="flex flex-col gap-2 rounded border border-neutral-800 bg-neutral-950 p-3">
          <div className="flex gap-2">
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value)}
              className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            >
              {SCOPES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
            <input
              value={scopeRef}
              onChange={(e) => setScopeRef(e.target.value)}
              placeholder="scope_ref (project or organization id)"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
          </div>
          <div className="flex gap-2">
            <input
              value={gateName}
              onChange={(e) => setGateName(e.target.value)}
              placeholder="gate_name"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
            <select
              value={hook}
              onChange={(e) => setHook(e.target.value)}
              className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            >
              {HOOKS.map((h) => (
                <option key={h} value={h}>
                  {h}
                </option>
              ))}
            </select>
          </div>
          <textarea
            value={config}
            onChange={(e) => setConfig(e.target.value)}
            placeholder="config (JSON)"
            rows={2}
            className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 font-mono text-sm outline-none focus:border-neutral-500"
          />
          <button
            type="submit"
            disabled={!gateName.trim() || !scopeRef.trim()}
            className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Bind gate
          </button>
        </form>
      )}
    </div>
  );
}
