import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type SkillBinding } from "../../lib/api";
import { canManageSkills, canOverrideSkills } from "../../lib/roles";
import EmptyState from "../../components/EmptyState";
import ScopeBadge from "../../components/ScopeBadge";
import StatusBadge from "../../components/StatusBadge";

export default function Skills({ namespace, slug, role }: { namespace: string; slug: string; role: string }) {
  const [bindings, setBindings] = useState<SkillBinding[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [projectId, setProjectId] = useState<string | null>(null);

  // Publish form
  const [name, setName] = useState("");
  const [author, setAuthor] = useState("");
  const [origin, setOrigin] = useState("");
  const [content, setContent] = useState("");

  // Install form — no "browse skills" endpoint exists yet, so skill_id /
  // skill_version_id are entered directly, same as Members' "add by
  // principal id"; a successful publish below pre-fills them.
  const [installScope, setInstallScope] = useState("project");
  const [installScopeRef, setInstallScopeRef] = useState("");
  const [installSkillId, setInstallSkillId] = useState("");
  const [installSkillVersionId, setInstallSkillVersionId] = useState("");
  const [installOverridable, setInstallOverridable] = useState(true);

  async function refresh() {
    try {
      setBindings(await api.listEffectiveSkills(namespace, slug));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load skills");
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
    if (installScope === "project" && projectId) setInstallScopeRef(projectId);
  }, [installScope, projectId]);

  async function onPublish(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const v = await api.publishSkill(namespace, slug, {
        name: name.trim(),
        author: author.trim(),
        origin: origin.trim(),
        content,
        declared_capabilities: [],
      });
      setName("");
      setAuthor("");
      setOrigin("");
      setContent("");
      // pre-fill the install form with what was just published
      setInstallSkillId(v.skill_id);
      setInstallSkillVersionId(v.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to publish skill");
    }
  }

  async function onInstall(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await api.installSkill(namespace, slug, {
        scope: installScope,
        scope_ref: installScopeRef.trim() || undefined,
        skill_id: installSkillId.trim(),
        skill_version_id: installSkillVersionId.trim(),
        managed: false,
        overridable: installOverridable,
      });
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to install skill");
    }
  }

  async function onOverride(b: SkillBinding) {
    setError(null);
    try {
      await api.overrideSkill(namespace, slug, {
        skill_id: b.skill_id,
        skill_version_id: b.skill_version_id,
        managed: b.managed,
        overridable: b.overridable,
      });
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to override skill");
    }
  }

  async function onTogglePin(b: SkillBinding) {
    setError(null);
    try {
      if (b.pinned) {
        await api.unpinSkillBinding(namespace, slug, b.id);
      } else {
        await api.pinSkillBinding(namespace, slug, b.id);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to update pin");
    }
  }

  const canManage = canManageSkills(role);
  const canOverride = canOverrideSkills(role);

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
            <EmptyState message="No skills installed." />
          </li>
        )}
        {bindings?.map((b) => (
          <li key={b.id} className="flex items-center justify-between px-3 py-2">
            <span className="font-mono text-xs text-cc-text">{b.skill_id.slice(0, 8)}…</span>
            <span className="flex items-center gap-3 text-xs text-cc-text-muted">
              <ScopeBadge scope={b.scope} />
              {b.pinned && <StatusBadge tone="neutral">pinned</StatusBadge>}
              {canManage && (
                <button onClick={() => void onTogglePin(b)} className="text-cc-text-secondary hover:text-cc-text">
                  {b.pinned ? "Unpin" : "Pin"}
                </button>
              )}
              {canOverride && b.scope !== "project" && (
                <button onClick={() => void onOverride(b)} className="text-cc-text-secondary hover:text-cc-text">
                  Override
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>

      {canManage && (
        <form onSubmit={onPublish} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
          <p className="text-xs text-cc-text-muted">Publish a new skill version</p>
          <div className="flex gap-2">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="name"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <input
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              placeholder="author"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <input
              value={origin}
              onChange={(e) => setOrigin(e.target.value)}
              placeholder="origin"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
          </div>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder="skill content"
            rows={3}
            className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 font-mono text-sm text-cc-text outline-none focus:border-cc-border-strong"
          />
          <button
            type="submit"
            disabled={!name.trim() || !author.trim() || !content.trim()}
            className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Publish
          </button>
        </form>
      )}

      {canManage && (
        <form onSubmit={onInstall} className="flex flex-col gap-2 rounded border border-cc-border bg-cc-surface-1 p-3">
          <p className="text-xs text-cc-text-muted">Install a skill version</p>
          <div className="flex gap-2">
            <select
              value={installScope}
              onChange={(e) => setInstallScope(e.target.value)}
              className="rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            >
              <option value="project">project</option>
              <option value="organization">organization</option>
            </select>
            <input
              value={installScopeRef}
              onChange={(e) => setInstallScopeRef(e.target.value)}
              placeholder="scope_ref (project or organization id)"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
          </div>
          <div className="flex gap-2">
            <input
              value={installSkillId}
              onChange={(e) => setInstallSkillId(e.target.value)}
              placeholder="skill_id"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
            <input
              value={installSkillVersionId}
              onChange={(e) => setInstallSkillVersionId(e.target.value)}
              placeholder="skill_version_id"
              className="flex-1 rounded border border-cc-border bg-cc-surface-2 px-2 py-1.5 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
          </div>
          <label className="flex items-center gap-2 text-xs text-cc-text-muted">
            <input type="checkbox" checked={installOverridable} onChange={(e) => setInstallOverridable(e.target.checked)} />
            overridable by a narrower scope
          </label>
          <button
            type="submit"
            disabled={!installSkillId.trim() || !installSkillVersionId.trim() || !installScopeRef.trim()}
            className="self-start rounded bg-cc-accent-primary px-3 py-1.5 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            Install
          </button>
        </form>
      )}
    </div>
  );
}
