import { useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type SkillBinding } from "../../lib/api";
import { canManageSkills, canOverrideSkills } from "../../lib/roles";

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
      {error && <p className="text-sm text-red-400">{error}</p>}
      <ul className="divide-y divide-neutral-800 rounded border border-neutral-800 text-sm">
        {bindings === null && <li className="px-3 py-2 text-neutral-500">Loading…</li>}
        {bindings?.length === 0 && <li className="px-3 py-2 text-neutral-500">No skills installed.</li>}
        {bindings?.map((b) => (
          <li key={b.id} className="flex items-center justify-between px-3 py-2">
            <span className="font-mono text-xs">{b.skill_id.slice(0, 8)}…</span>
            <span className="flex items-center gap-3 text-xs text-neutral-500">
              {b.scope}
              {b.pinned ? " · pinned" : ""}
              {canManage && (
                <button onClick={() => void onTogglePin(b)} className="text-neutral-300 hover:text-neutral-100">
                  {b.pinned ? "Unpin" : "Pin"}
                </button>
              )}
              {canOverride && b.scope !== "project" && (
                <button onClick={() => void onOverride(b)} className="text-neutral-300 hover:text-neutral-100">
                  Override
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>

      {canManage && (
        <form onSubmit={onPublish} className="flex flex-col gap-2 rounded border border-neutral-800 bg-neutral-950 p-3">
          <p className="text-xs text-neutral-500">Publish a new skill version</p>
          <div className="flex gap-2">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="name"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
            <input
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              placeholder="author"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
            <input
              value={origin}
              onChange={(e) => setOrigin(e.target.value)}
              placeholder="origin"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
          </div>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder="skill content"
            rows={3}
            className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 font-mono text-sm outline-none focus:border-neutral-500"
          />
          <button
            type="submit"
            disabled={!name.trim() || !author.trim() || !content.trim()}
            className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Publish
          </button>
        </form>
      )}

      {canManage && (
        <form onSubmit={onInstall} className="flex flex-col gap-2 rounded border border-neutral-800 bg-neutral-950 p-3">
          <p className="text-xs text-neutral-500">Install a skill version</p>
          <div className="flex gap-2">
            <select
              value={installScope}
              onChange={(e) => setInstallScope(e.target.value)}
              className="rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            >
              <option value="project">project</option>
              <option value="organization">organization</option>
            </select>
            <input
              value={installScopeRef}
              onChange={(e) => setInstallScopeRef(e.target.value)}
              placeholder="scope_ref (project or organization id)"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
          </div>
          <div className="flex gap-2">
            <input
              value={installSkillId}
              onChange={(e) => setInstallSkillId(e.target.value)}
              placeholder="skill_id"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
            <input
              value={installSkillVersionId}
              onChange={(e) => setInstallSkillVersionId(e.target.value)}
              placeholder="skill_version_id"
              className="flex-1 rounded border border-neutral-700 bg-neutral-950 px-2 py-1.5 text-sm outline-none focus:border-neutral-500"
            />
          </div>
          <label className="flex items-center gap-2 text-xs text-neutral-400">
            <input type="checkbox" checked={installOverridable} onChange={(e) => setInstallOverridable(e.target.checked)} />
            overridable by a narrower scope
          </label>
          <button
            type="submit"
            disabled={!installSkillId.trim() || !installSkillVersionId.trim() || !installScopeRef.trim()}
            className="self-start rounded bg-neutral-100 px-3 py-1.5 text-sm font-medium text-neutral-900 disabled:opacity-50"
          >
            Install
          </button>
        </form>
      )}
    </div>
  );
}
