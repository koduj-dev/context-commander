import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useT } from "../i18n";
import { api, type MyProject } from "../lib/api";
import { useDebounced, useResource } from "../lib/resource";

/**
 * Getting somewhere without navigating there.
 *
 * Three sources, and they are not the same kind of thing:
 *
 *  - Screens are a fixed list this file knows.
 *  - Projects are filtered in the browser, because `/me/projects` already
 *    returned all of them and they are bounded by membership.
 *  - Skills are searched on the server, because the catalogue is not.
 *
 * There is no cross-domain search endpoint and this does not pretend to be
 * one: it says which kind each result is, so a reader is never left wondering
 * whether the thing they were looking for exists but was not searched.
 */
type Result = { id: string; label: string; hint?: string; kind: string; to: string };

export function QuickFind({ onClose }: { onClose: () => void }) {
  const t = useT();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounced = useDebounced(query, 200);

  const projects = useResource(() => api.myProjects(), []);
  const skills = useResource(
    () => (debounced ? api.searchSkills(debounced, undefined, 5) : Promise.resolve({ items: [] })),
    [debounced],
  );

  useEffect(() => inputRef.current?.focus(), []);

  const screens: Result[] = useMemo(
    () => [
      { id: "s:dash", label: t("dashboard.title"), kind: t("find.screen"), to: "/" },
      { id: "s:proj", label: t("projects.title"), kind: t("find.screen"), to: "/projects" },
      { id: "s:orgs", label: t("orgs.title"), kind: t("find.screen"), to: "/organizations" },
      { id: "s:reg", label: t("registry.title"), kind: t("find.screen"), to: "/registry" },
      { id: "s:tok", label: t("tokens.title"), kind: t("find.screen"), to: "/tokens" },
      { id: "s:pref", label: t("prefs.title"), kind: t("find.screen"), to: "/preferences" },
      { id: "s:adm", label: t("admin.title"), kind: t("find.screen"), to: "/administration" },
    ],
    [t],
  );

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    const path = (p: MyProject) => `${p.namespace}/${p.slug}`;

    const projectHits: Result[] = (projects.data ?? [])
      .filter((p) => !q || p.name.toLowerCase().includes(q) || path(p).toLowerCase().includes(q))
      .slice(0, 6)
      .map((p) => ({
        id: `p:${path(p)}`,
        label: p.name,
        hint: path(p),
        kind: t("find.project"),
        to: `/p/${p.namespace}/${p.slug}`,
      }));

    const skillHits: Result[] = (skills.data?.items ?? []).map((s) => ({
      id: `k:${s.skill_id}`,
      label: s.name,
      hint: s.author,
      kind: t("find.skill"),
      to: `/registry/${s.skill_id}`,
    }));

    const screenHits = screens.filter((s) => !q || s.label.toLowerCase().includes(q));

    return [...projectHits, ...skillHits, ...screenHits];
  }, [query, projects.data, skills.data, screens, t]);

  // A result list that shrank under the cursor must not leave it past the end.
  useEffect(() => setCursor(0), [query]);

  function go(r: Result | undefined) {
    if (!r) return;
    navigate(r.to);
    onClose();
  }

  return (
    <div
      className="palette-back"
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="palette" role="dialog" aria-modal="true" aria-label={t("find.open")}>
        <input
          ref={inputRef}
          value={query}
          placeholder={t("find.placeholder")}
          aria-label={t("find.placeholder")}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setCursor((c) => Math.min(c + 1, results.length - 1));
            }
            if (e.key === "ArrowUp") {
              e.preventDefault();
              setCursor((c) => Math.max(c - 1, 0));
            }
            if (e.key === "Enter") {
              e.preventDefault();
              go(results[cursor]);
            }
          }}
        />

        <div className="palette-list" role="listbox" aria-label={t("find.open")}>
          {results.length === 0 ? (
            <div className="palette-empty">{t("find.nothing")}</div>
          ) : (
            results.map((r, i) => (
              <button
                key={r.id}
                type="button"
                role="option"
                aria-selected={i === cursor}
                onMouseEnter={() => setCursor(i)}
                onClick={() => go(r)}
              >
                <span>{r.label}</span>
                {r.hint && <span className="dim">{r.hint}</span>}
                <span className="kind">{r.kind}</span>
              </button>
            ))
          )}
        </div>

        <div className="palette-empty">{t("find.hint")}</div>
      </div>
    </div>
  );
}
