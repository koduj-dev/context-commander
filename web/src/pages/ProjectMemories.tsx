import { useState, type ReactNode } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import {
  api,
  type Memory,
  type MemoryFilter,
  type MemoryChange,
  type MemoryHit,
  type MemoryKind,
  type Person,
  type SearchMode,
} from "../lib/api";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Loading,
  Markdown,
  Mono,
  Note,
  PageHead,
  Pager,
  Panel,
  Row,
  Seg,
  SelectField,
  Stack,
  TextArea,
  TextField,
  type Tone,
} from "../ui";
import { useProject } from "./ProjectLayout";

type T = ReturnType<typeof useT>;

const KINDS: MemoryKind[] = ["decision", "finding", "gotcha", "note", "status", "other"];

export function kindLabel(t: T, kind: string): string {
  switch (kind) {
    case "decision":
      return t("memories.kind.decision");
    case "finding":
      return t("memories.kind.finding");
    case "gotcha":
      return t("memories.kind.gotcha");
    case "note":
      return t("memories.kind.note");
    case "status":
      return t("memories.kind.status");
    default:
      return t("memories.kind.other");
  }
}

function kindTone(kind: string): Tone {
  switch (kind) {
    case "decision":
      return "ok";
    case "gotcha":
      return "warn";
    case "finding":
      return "info";
    default:
      return "neutral";
  }
}

function statusLabel(t: T, status: string): string {
  switch (status) {
    case "superseded":
      return t("memories.status.superseded");
    case "archived":
      return t("memories.status.archived");
    default:
      return t("memories.status.active");
  }
}

/**
 * The ticket link, or nothing. The server already refuses anything but an
 * absolute http(s) URL; this checks again because a link is where a stored
 * string turns into something a colleague clicks, and a javascript: URL
 * that slipped past one check must not get a second chance here.
 */
export function safeTicketURL(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  try {
    const u = new URL(raw);
    return u.protocol === "http:" || u.protocol === "https:" ? u.href : undefined;
  } catch {
    return undefined;
  }
}

function personName(people: Record<string, Person>, id: string | undefined, fallback: string) {
  if (!id) return fallback;
  return people[id]?.name || fallback;
}

/**
 * What the team and its agents learned about this project (docs/adr/0025).
 *
 * The filters are the point: an agent records the branch, the ticket and who
 * solved it, so "everything about PROJ-123" or "what was found on this
 * branch" is one click. Filtering is metadata, answered by the server
 * without decrypting anything; the search box matches the encrypted text,
 * which the server decrypts to answer, and is combined with whatever filters
 * are set.
 *
 * Where the server has semantic search (docs/adr/0028) the facets say so,
 * and only then does the mode switch appear and the detail view offer
 * related memories: a control for a mode the server would refuse is a
 * promise this screen cannot keep.
 */
export function ProjectMemories() {
  const t = useT();
  const { namespace, slug } = useProject();
  const [filter, setFilter] = useState<MemoryFilter>({});
  const [query, setQuery] = useState("");
  const [submitted, setSubmitted] = useState("");
  const [creating, setCreating] = useState(false);
  const [nonce, setNonce] = useState(0);
  const [mode, setMode] = useState<SearchMode>();
  const facets = useResource(() => api.memoryFacets(namespace, slug), [namespace, slug, nonce]);
  const semantic = facets.data?.search_modes?.includes("semantic") ?? false;
  const activeMode: SearchMode = mode ?? facets.data?.default_search_mode ?? "text";

  const set = (key: keyof MemoryFilter, value: string) =>
    setFilter((f) => ({ ...f, [key]: value || undefined }));
  const changed = () => setNonce((n) => n + 1);

  const fc = facets.data;
  const chips: { key: keyof MemoryFilter; label: string }[] = [];
  if (filter.kind)
    chips.push({
      key: "kind",
      label: t("memories.chip.kind", { value: kindLabel(t, filter.kind) }),
    });
  if (filter.branch)
    chips.push({ key: "branch", label: t("memories.chip.branch", { value: filter.branch }) });
  if (filter.ticket)
    chips.push({ key: "ticket", label: t("memories.chip.ticket", { value: filter.ticket }) });
  if (filter.tag) chips.push({ key: "tag", label: t("memories.chip.tag", { value: filter.tag }) });
  if (filter.assignee)
    chips.push({
      key: "assignee",
      label: t("memories.chip.assignee", {
        value: personName(fc?.people ?? {}, filter.assignee, t("plans.nobody")),
      }),
    });
  if (filter.status)
    chips.push({
      key: "status",
      label: t("memories.chip.status", {
        value: filter.status === "any" ? t("memories.status.any") : statusLabel(t, filter.status),
      }),
    });

  return (
    <>
      <PageHead
        title={t("memories.title")}
        lead={t("memories.lead")}
        actions={
          <Btn kind="primary" onClick={() => setCreating((c) => !c)}>
            {creating ? t("common.cancel") : t("memories.new")}
          </Btn>
        }
      />
      <Stack>
        {creating && (
          <NewMemoryForm
            onCreated={() => {
              setCreating(false);
              changed();
            }}
          />
        )}
        <Panel title={t("memories.findTitle")}>
          <Stack gap="sm">
            <Row gap="sm">
              <TextField
                label={t("memories.search")}
                type="search"
                placeholder={
                  semantic && activeMode !== "text"
                    ? t("memories.searchPlaceholderMeaning")
                    : t("memories.searchPlaceholder")
                }
                value={query}
                onChange={(v) => {
                  setQuery(v);
                  // Clearing the box goes back to the list, as a person
                  // emptying a search field expects.
                  if (!v.trim()) setSubmitted("");
                }}
                grow
              />
              <Btn disabled={!query.trim()} onClick={() => setSubmitted(query.trim())}>
                {t("memories.searchButton")}
              </Btn>
            </Row>
            {semantic && (
              <Row gap="sm">
                <span className="dim">{t("memories.mode")}</span>
                <Seg<SearchMode>
                  label={t("memories.mode")}
                  value={activeMode}
                  onChange={setMode}
                  options={[
                    { value: "text", label: t("memories.mode.text") },
                    { value: "semantic", label: t("memories.mode.semantic") },
                    { value: "hybrid", label: t("memories.mode.hybrid") },
                  ]}
                />
              </Row>
            )}
            <Row gap="sm">
              <SelectField
                label={t("memories.filter.kind")}
                value={filter.kind ?? ""}
                onChange={(v) => set("kind", v)}
                options={[
                  { value: "", label: t("memories.filter.anyKind") },
                  ...KINDS.map((k) => ({ value: k, label: kindLabel(t, k) })),
                ]}
              />
              <SelectField
                label={t("memories.filter.branch")}
                value={filter.branch ?? ""}
                onChange={(v) => set("branch", v)}
                options={[
                  { value: "", label: t("memories.filter.anyBranch") },
                  ...(fc?.branches ?? []).map((b) => ({ value: b, label: b })),
                ]}
              />
              <SelectField
                label={t("memories.filter.ticket")}
                value={filter.ticket ?? ""}
                onChange={(v) => set("ticket", v)}
                options={[
                  { value: "", label: t("memories.filter.anyTicket") },
                  ...(fc?.tickets ?? []).map((k) => ({ value: k, label: k })),
                ]}
              />
              <SelectField
                label={t("memories.filter.tag")}
                value={filter.tag ?? ""}
                onChange={(v) => set("tag", v)}
                options={[
                  { value: "", label: t("memories.filter.anyTag") },
                  ...(fc?.tags ?? []).map((g) => ({ value: g, label: g })),
                ]}
              />
              <SelectField
                label={t("memories.filter.assignee")}
                value={filter.assignee ?? ""}
                onChange={(v) => set("assignee", v)}
                options={[
                  { value: "", label: t("memories.filter.anyAssignee") },
                  ...(fc?.assignees ?? []).map((id) => ({
                    value: id,
                    label: personName(fc?.people ?? {}, id, id),
                  })),
                ]}
              />
              <SelectField
                label={t("memories.filter.status")}
                value={filter.status ?? ""}
                onChange={(v) => set("status", v)}
                options={[
                  { value: "", label: t("memories.status.active") },
                  { value: "superseded", label: t("memories.status.superseded") },
                  { value: "archived", label: t("memories.status.archived") },
                  { value: "any", label: t("memories.status.any") },
                ]}
              />
            </Row>
            {chips.length > 0 && (
              <Row gap="sm">
                {chips.map((c) => (
                  <Btn
                    key={c.key}
                    small
                    label={t("memories.chip.remove", { filter: c.label })}
                    onClick={() => set(c.key, "")}
                  >
                    {c.label} ×
                  </Btn>
                ))}
                <Btn small onClick={() => setFilter({})}>
                  {t("memories.chip.clear")}
                </Btn>
              </Row>
            )}
          </Stack>
        </Panel>
        {submitted ? (
          <Results
            query={submitted}
            filter={filter}
            mode={semantic ? activeMode : undefined}
            semantic={semantic}
            nonce={nonce}
            onChanged={changed}
          />
        ) : (
          <List filter={filter} semantic={semantic} nonce={nonce} onChanged={changed} />
        )}
      </Stack>
    </>
  );
}

function List({
  filter,
  semantic,
  nonce,
  onChanged,
}: {
  filter: MemoryFilter;
  semantic: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [people, setPeople] = useState<Record<string, Person>>({});
  const key = JSON.stringify(filter);
  const list = useList(
    (cursor) =>
      api.memories(namespace, slug, filter, cursor).then((page) => {
        setPeople((p) => ({ ...p, ...page.people }));
        return page;
      }),
    [namespace, slug, key, nonce],
  );

  return (
    <Panel
      title={t("memories.listTitle")}
      tight
      footer={
        <Pager
          shown={list.items.length}
          hasMore={list.hasMore}
          onLoadMore={list.loadMore}
          busy={list.loadingMore}
        />
      }
    >
      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : list.items.length === 0 ? (
        <EmptyState title={t("memories.empty")} body={t("memories.emptyBody")} />
      ) : (
        <MemoryTable rows={list.items} people={people} semantic={semantic} onChanged={onChanged} />
      )}
    </Panel>
  );
}

function Results({
  query,
  filter,
  mode,
  semantic,
  nonce,
  onChanged,
}: {
  query: string;
  filter: MemoryFilter;
  mode?: SearchMode;
  semantic: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const key = JSON.stringify(filter);
  const hits = useResource(
    () => api.searchMemories(namespace, slug, query, filter, mode),
    [namespace, slug, query, key, mode, nonce],
  );

  return (
    <Panel title={t("memories.resultsTitle", { query })} tight>
      {hits.loading ? (
        <Loading />
      ) : hits.error || !hits.data ? (
        <ErrorState error={hits.error} onRetry={hits.reload} />
      ) : hits.data.items.length === 0 ? (
        <EmptyState title={t("memories.noHits", { query })} />
      ) : (
        <>
          {hits.data.degraded && (
            <div className="body">
              <Note tone="warn">{t("memories.degraded")}</Note>
            </div>
          )}
          {hits.data.truncated && (
            <div className="body">
              <Note tone="info">{t("memories.truncated")}</Note>
            </div>
          )}
          <MemoryTable
            rows={hits.data.items}
            people={hits.data.people}
            semantic={semantic}
            onChanged={onChanged}
          />
        </>
      )}
    </Panel>
  );
}

function MemoryTable({
  rows,
  people,
  semantic,
  onChanged,
}: {
  rows: (Memory | MemoryHit)[];
  people: Record<string, Person>;
  semantic: boolean;
  onChanged: () => void;
}) {
  const t = useT();
  const f = useFormat();
  const [open, setOpen] = useState<string>();

  return (
    <DataTable
      columns={[
        { key: "memory", label: t("memories.column.memory") },
        { key: "assignee", label: t("memories.column.assignee") },
        { key: "when", label: t("memories.column.when"), align: "r" },
        { key: "actions", align: "r" },
      ]}
    >
      {rows.map((m) => {
        const snippet = "snippet" in m ? m.snippet : "";
        const byMeaning = "match" in m && m.match === "semantic";
        return (
          <MemoryRows
            key={m.id}
            memory={m}
            open={open === m.id}
            onToggle={() => setOpen(open === m.id ? undefined : m.id)}
            people={people}
            semantic={semantic}
            onChanged={onChanged}
            summary={
              <>
                <td>
                  <Cell
                    primary={m.title}
                    secondary={
                      <>
                        <Badge tone={kindTone(m.kind)}>{kindLabel(t, m.kind)}</Badge>{" "}
                        {byMeaning && (
                          <>
                            <Badge tone="info">{t("memories.match.semantic")}</Badge>{" "}
                          </>
                        )}
                        {m.status !== "active" && (
                          <>
                            <Badge>{statusLabel(t, m.status)}</Badge>{" "}
                          </>
                        )}
                        {m.branch && (
                          <>
                            <Mono dim>{m.branch}</Mono>{" "}
                          </>
                        )}
                        {m.ticket_key && (
                          <>
                            <Mono dim>{m.ticket_key}</Mono>{" "}
                          </>
                        )}
                        {m.tags.length > 0 && <span className="dim">{m.tags.join(" · ")}</span>}
                        {snippet && <div className="dim">{snippet}</div>}
                      </>
                    }
                  />
                </td>
                <td>{personName(people, m.assignee, "—")}</td>
                <td className="r dim">{f.relative(m.created_at)}</td>
              </>
            }
          />
        );
      })}
    </DataTable>
  );
}

function MemoryRows({
  memory,
  open,
  onToggle,
  summary,
  people,
  semantic,
  onChanged,
}: {
  memory: Memory;
  open: boolean;
  onToggle: () => void;
  summary: ReactNode;
  people: Record<string, Person>;
  semantic: boolean;
  onChanged: () => void;
}) {
  const t = useT();
  return (
    <>
      <tr>
        {summary}
        <td className="r">
          <Btn small onClick={onToggle}>
            {open ? t("common.close") : t("memories.open")}
          </Btn>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={4}>
            <MemoryDetail
              id={memory.id}
              people={people}
              semantic={semantic}
              onChanged={onChanged}
            />
          </td>
        </tr>
      )}
    </>
  );
}

/**
 * One memory, whole. Loaded by id because a search hit carries a snippet,
 * not the body.
 */
function MemoryDetail({
  id,
  people,
  semantic,
  onChanged,
}: {
  id: string;
  people: Record<string, Person>;
  semantic: boolean;
  onChanged: () => void;
}) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const memory = useResource(() => api.memory(namespace, slug, id), [namespace, slug, id]);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [editing, setEditing] = useState(false);

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      onChanged();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (memory.loading) return <Loading />;
  if (memory.error || !memory.data)
    return <ErrorState error={memory.error} onRetry={memory.reload} />;
  const m = memory.data;
  const link = safeTicketURL(m.ticket_url);

  if (editing) {
    return (
      <MemoryForm
        title={t("memories.edit")}
        initial={draftOf(m)}
        submitLabel={t("memories.saveChanges")}
        onCancel={() => setEditing(false)}
        onSubmit={async (d) => {
          const change = memoryChange(m, d);
          if (Object.keys(change).length > 1) {
            await api.updateMemory(namespace, slug, m.id, change);
          }
          setEditing(false);
          memory.reload();
          onChanged();
        }}
      />
    );
  }

  return (
    <Stack gap="sm">
      <Defs>
        <Def term={t("memories.field.kind")}>
          <Badge tone={kindTone(m.kind)}>{kindLabel(t, m.kind)}</Badge>
        </Def>
        <Def term={t("memories.field.status")}>{statusLabel(t, m.status)}</Def>
        {m.branch && (
          <Def term={t("memories.field.branch")}>
            <Mono>{m.branch}</Mono>
          </Def>
        )}
        {(m.ticket_key || link) && (
          <Def term={t("memories.field.ticket")}>
            {link ? (
              <a href={link} target="_blank" rel="noopener noreferrer">
                {m.ticket_key || link}
              </a>
            ) : (
              <Mono>{m.ticket_key}</Mono>
            )}
          </Def>
        )}
        {m.tags.length > 0 && <Def term={t("memories.field.tags")}>{m.tags.join(", ")}</Def>}
        <Def term={t("memories.field.assignee")}>{personName(people, m.assignee, "—")}</Def>
        <Def term={t("memories.field.author")}>
          {personName(people, m.created_by, t("plans.nobody"))}
        </Def>
        <Def term={t("memories.field.created")}>{f.stamp(m.created_at)}</Def>
        {m.updated_at !== m.created_at && (
          <Def term={t("memories.field.updated")}>{f.stamp(m.updated_at)}</Def>
        )}
        {m.superseded_by && (
          <Def term={t("memories.field.supersededBy")}>
            <Mono>{m.superseded_by}</Mono>
          </Def>
        )}
      </Defs>
      {m.body ? <Markdown source={m.body} /> : <span className="dim">{t("memories.noBody")}</span>}
      {semantic && <Related id={m.id} />}
      {failure !== undefined && <ErrorState error={failure} />}
      <Row gap="sm">
        <Btn small disabled={busy} onClick={() => setEditing(true)}>
          {t("memories.editAction")}
        </Btn>
        {m.status === "active" ? (
          <Btn
            small
            disabled={busy}
            onClick={() => run(() => api.setMemoryStatus(namespace, slug, m.id, "archived"))}
          >
            {t("memories.archive")}
          </Btn>
        ) : (
          <Btn
            small
            disabled={busy}
            onClick={() => run(() => api.setMemoryStatus(namespace, slug, m.id, "active"))}
          >
            {t("memories.reactivate")}
          </Btn>
        )}
        {confirmDelete ? (
          <>
            <Btn
              small
              kind="danger"
              disabled={busy}
              onClick={() => run(() => api.deleteMemory(namespace, slug, m.id))}
            >
              {t("memories.deleteConfirm")}
            </Btn>
            <Btn small onClick={() => setConfirmDelete(false)}>
              {t("common.cancel")}
            </Btn>
          </>
        ) : (
          <Btn small kind="danger" disabled={busy} onClick={() => setConfirmDelete(true)}>
            {t("memories.delete")}
          </Btn>
        )}
      </Row>
    </Stack>
  );
}

/**
 * The memories closest in meaning to one memory (docs/adr/0028). Shown only
 * where the server has semantic search; the server compares the stored
 * embeddings, and a memory not embedded yet is embedded on the spot.
 */
function Related({ id }: { id: string }) {
  const t = useT();
  const { namespace, slug } = useProject();
  const related = useResource(
    () => api.relatedMemories(namespace, slug, id),
    [namespace, slug, id],
  );
  return (
    <Panel title={t("memories.related")} tight>
      <div className="body">
        <span className="dim">{t("memories.relatedLead")}</span>
      </div>
      {related.loading ? (
        <Loading />
      ) : related.error || !related.data ? (
        <ErrorState error={related.error} onRetry={related.reload} />
      ) : related.data.items.length === 0 ? (
        <EmptyState title={t("memories.relatedEmpty")} />
      ) : (
        <DataTable columns={[{ key: "memory", label: t("memories.column.memory") }]}>
          {related.data.items.map((h) => (
            <tr key={h.id}>
              <td>
                <Cell
                  primary={h.title}
                  secondary={
                    <>
                      <Badge tone={kindTone(h.kind)}>{kindLabel(t, h.kind)}</Badge>{" "}
                      {h.snippet && <span className="dim">{h.snippet}</span>}
                    </>
                  }
                />
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}

/** What the memory form edits, as the inputs hold it. */
type MemoryDraft = {
  kind: MemoryKind;
  title: string;
  body: string;
  branch: string;
  ticketKey: string;
  ticketURL: string;
  tags: string;
};

const EMPTY_DRAFT: MemoryDraft = {
  kind: "note",
  title: "",
  body: "",
  branch: "",
  ticketKey: "",
  ticketURL: "",
  tags: "",
};

function draftOf(m: Memory): MemoryDraft {
  return {
    kind: m.kind,
    title: m.title,
    body: m.body,
    branch: m.branch ?? "",
    ticketKey: m.ticket_key ?? "",
    ticketURL: m.ticket_url ?? "",
    tags: m.tags.join(", "),
  };
}

function tagList(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

/**
 * Only what the form changed, so an edit does not rewrite fields it never
 * touched. An emptied branch or ticket is sent as "" — the server reads that
 * as "clear it" — and the version the form was opened at guards the whole
 * change: if an agent updated the memory meanwhile the server answers 409
 * and nothing is overwritten.
 */
export function memoryChange(before: Memory, d: MemoryDraft): MemoryChange {
  const was = draftOf(before);
  const change: MemoryChange = { expected_version: before.version };
  if (d.kind !== was.kind) change.kind = d.kind;
  if (d.title.trim() !== was.title) change.title = d.title.trim();
  if (d.body !== was.body) change.body = d.body;
  if (d.branch.trim() !== was.branch) change.branch = d.branch.trim();
  if (d.ticketKey.trim() !== was.ticketKey) change.ticket_key = d.ticketKey.trim();
  if (d.ticketURL.trim() !== was.ticketURL) change.ticket_url = d.ticketURL.trim();
  const tags = tagList(d.tags);
  if (tags.join("\u0000") !== before.tags.join("\u0000")) change.tags = tags;
  return change;
}

function MemoryForm({
  title,
  initial,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  title: string;
  initial: MemoryDraft;
  submitLabel: string;
  onSubmit: (d: MemoryDraft) => Promise<unknown>;
  onCancel?: () => void;
}) {
  const t = useT();
  const [d, setD] = useState<MemoryDraft>(initial);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const set =
    <K extends keyof MemoryDraft>(k: K) =>
    (v: MemoryDraft[K]) =>
      setD((cur) => ({ ...cur, [k]: v }));

  async function save() {
    setBusy(true);
    setFailure(undefined);
    try {
      await onSubmit(d);
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={title}>
      <Stack gap="sm">
        <SelectField
          label={t("memories.field.kind")}
          value={d.kind}
          onChange={set("kind")}
          options={KINDS.map((k) => ({ value: k, label: kindLabel(t, k) }))}
        />
        <TextField
          labelled
          label={t("memories.field.title")}
          value={d.title}
          onChange={set("title")}
        />
        <TextArea
          labelled
          label={t("memories.field.body")}
          value={d.body}
          onChange={set("body")}
          rows={8}
        />
        <Row gap="sm">
          <TextField
            labelled
            label={t("memories.field.branch")}
            value={d.branch}
            onChange={set("branch")}
          />
          <TextField
            labelled
            label={t("memories.field.ticketKey")}
            value={d.ticketKey}
            onChange={set("ticketKey")}
          />
          <TextField
            labelled
            label={t("memories.field.ticketURL")}
            value={d.ticketURL}
            onChange={set("ticketURL")}
          />
        </Row>
        <TextField
          labelled
          label={t("memories.field.tagsInput")}
          value={d.tags}
          onChange={set("tags")}
        />
        {failure !== undefined && <ErrorState error={failure} />}
        <Row gap="sm">
          <Btn kind="primary" disabled={busy || !d.title.trim()} onClick={save}>
            {submitLabel}
          </Btn>
          {onCancel && (
            <Btn disabled={busy} onClick={onCancel}>
              {t("common.cancel")}
            </Btn>
          )}
        </Row>
      </Stack>
    </Panel>
  );
}

function NewMemoryForm({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();
  return (
    <MemoryForm
      title={t("memories.new")}
      initial={EMPTY_DRAFT}
      submitLabel={t("memories.save")}
      onSubmit={async (d) => {
        await api.createMemory(namespace, slug, {
          kind: d.kind,
          title: d.title.trim(),
          body: d.body,
          branch: d.branch.trim() || undefined,
          ticket_key: d.ticketKey.trim() || undefined,
          ticket_url: d.ticketURL.trim() || undefined,
          tags: tagList(d.tags),
        });
        onCreated();
      }}
    />
  );
}
