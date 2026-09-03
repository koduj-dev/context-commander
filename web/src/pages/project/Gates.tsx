import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type GateBinding } from "../../lib/api";
import { canManageGates } from "../../lib/roles";
import EmptyState from "../../components/EmptyState";
import ScopeBadge from "../../components/ScopeBadge";
import ConfirmDialog from "../../components/ConfirmDialog";

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
  const [confirmUnbind, setConfirmUnbind] = useState<string | null>(null);

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
      {error && <p className="text-sm text-cc-danger">{error}</p>}
      <ul className="divide-y divide-cc-border rounded border border-cc-border text-sm">
        {bindings === null && (
          <li>
            <EmptyState message="Loading…" />
          </li>
        )}
        {bindings?.length === 0 && (
          <li>
            <EmptyState message="No gates bound." />
          </li>
        )}
        {bindings?.map((g) => (
          <li key={g.id} className="flex items-center justify-between px-3 py-2">
            <span className="flex items-center gap-2">
              {g.gate_name} <ScopeBadge scope={g.scope} />
            </span>
            <span className="flex items-center gap-3 text-xs text-cc-text-muted">
              {g.hook}
              {canManage && (
                <button onClick={() => setConfirmUnbind(g.id)} className="text-cc-danger hover:opacity-80">
                  Unbind
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>
      {canManage && (
        <form onSubmit={onBind} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
          <div className="flex gap-2">
            <select
              value={scope}
              onChange={(e) => setScope(e.target.value)}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
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
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
          </div>
          <div className="flex gap-2">
            <input
              value={gateName}
              onChange={(e) => setGateName(e.target.value)}
              placeholder="gate_name"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <select
              value={hook}
              onChange={(e) => setHook(e.target.value)}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
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
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 font-mono text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <button
            type="submit"
            disabled={!gateName.trim() || !scopeRef.trim()}
            className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Bind gate
          </button>
        </form>
      )}
      <ConfirmDialog
        open={confirmUnbind !== null}
        title="Unbind gate"
        message="Unbind this gate? Requests that relied on it will no longer be checked."
        confirmLabel="Unbind"
        danger
        onCancel={() => setConfirmUnbind(null)}
        onConfirm={() => {
          const target = confirmUnbind!;
          setConfirmUnbind(null);
          void onUnbind(target);
        }}
      />
    </div>
  );
}
