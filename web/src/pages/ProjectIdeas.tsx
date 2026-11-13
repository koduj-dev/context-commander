import { createContext, useContext, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api, type Idea, type IdeaPriority, type IdeaStatus, type Person } from "../lib/api";
import { useLive, type Live } from "../lib/live";
import { useList, usePolled } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  Code,
  DataTable,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Loading,
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
import { BOARD_REFRESH_MS, LIVE_REFRESH_MS } from "./ProjectPlans";

type T = ReturnType<typeof useT>;

/** The board stream, shared so an open idea re-reads on the same pulse. */
const IdeasLive = createContext<Live>({ live: false, pulse: 0 });

/**
 * The idea stack (docs/adr/0026).
 *
 * People and agents drop ideas here; a planning agent outside the server
 * takes them one at a time and answers with a proposal and a draft plan,
 * and a person decides here. Read top-down it answers, in order: what is
 * waiting for my decision, what the agent will take next, what it is on now,
 * and what was decided.
 *
 * Accept and Reject exist only on this screen: the server takes a decision
 * only from a browser session, so an agent holding its user's token cannot
 * approve its own proposal.
 *
 * Idea changes are announced on the plan board's stream, which this screen
 * shares rather than opening a second one per project.
 */
export function ProjectIdeas() {
  const t = useT();
  const { namespace, slug } = useProject();
  const live = useLive(api.planBoardEvents(namespace, slug));
  const interval = live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS;
  // The three live stages are small by nature — a queue a team works
  // through — so each is read as one page and kept current. The decided
  // ones only grow, and page with "load more".
  const active = usePolled(
    () => api.ideas(namespace, slug, ["proposed", "claimed"]),
    [namespace, slug],
    interval,
    live.pulse,
  );
  const queue = usePolled(
    () => api.ideas(namespace, slug, ["open"]),
    [namespace, slug],
    interval,
    live.pulse,
  );
  const decided = useList(
    (cursor?: string) => api.ideas(namespace, slug, ["accepted", "rejected", "withdrawn"], cursor),
    [namespace, slug, live.pulse],
  );
  const [open, setOpen] = useState<string>();

  if (active.loading || queue.loading) return <Loading />;
  if (active.error || !active.data)
    return <ErrorState error={active.error} onRetry={active.reload} />;
  if (queue.error || !queue.data) return <ErrorState error={queue.error} onRetry={queue.reload} />;

  const people = { ...active.data.people, ...queue.data.people };
  const proposed = active.data.items.filter((i) => i.status === "proposed");
  const claimed = active.data.items.filter((i) => i.status === "claimed");
  const changed = () => {
    active.reload();
    queue.reload();
    decided.reload();
  };
  const rows = {
    open,
    toggle: (id: string) => setOpen(open === id ? undefined : id),
    people,
    onChanged: changed,
  };

  return (
    <IdeasLive.Provider value={live}>
      <PageHead title={t("ideas.title")} lead={t("ideas.lead")} />
      <Stack>
        {(active.stale || queue.stale) && <Note tone="warn">{t("plans.stale")}</Note>}
        <AddIdea onAdded={changed} />

        <Panel title={t("ideas.review")} tight>
          {proposed.length === 0 ? (
            <EmptyState title={t("ideas.reviewEmpty")} body={t("ideas.reviewEmptyBody")} />
          ) : (
            <IdeaTable ideas={proposed} {...rows} />
          )}
        </Panel>

        <Panel
          title={t("ideas.queue")}
          sub={live.live ? t("plans.live") : t("ideas.queueSub")}
          tight
        >
          {queue.data.items.length === 0 ? (
            <EmptyState title={t("ideas.queueEmpty")} body={t("ideas.queueEmptyBody")} />
          ) : (
            <IdeaTable ideas={queue.data.items} ordered {...rows} />
          )}
        </Panel>

        <Panel title={t("ideas.claimed")} tight>
          {claimed.length === 0 ? (
            <EmptyState title={t("ideas.claimedEmpty")} body={t("ideas.claimedEmptyBody")} />
          ) : (
            <IdeaTable ideas={claimed} {...rows} />
          )}
        </Panel>

        <Panel title={t("ideas.decided")} tight>
          {decided.loading ? (
            <Loading />
          ) : decided.error && decided.items.length === 0 ? (
            <ErrorState error={decided.error} onRetry={decided.reload} />
          ) : decided.items.length === 0 ? (
            <EmptyState title={t("ideas.decidedEmpty")} />
          ) : (
            <>
              <IdeaTable ideas={decided.items} {...rows} />
              <Pager
                shown={decided.items.length}
                hasMore={decided.hasMore}
                onLoadMore={decided.loadMore}
                busy={decided.loadingMore}
              />
            </>
          )}
        </Panel>
      </Stack>
    </IdeasLive.Provider>
  );
}

export function ideaStatusLabel(t: T, s: IdeaStatus) {
  return {
    open: t("ideas.status.open"),
    claimed: t("ideas.status.claimed"),
    proposed: t("ideas.status.proposed"),
    accepted: t("ideas.status.accepted"),
    rejected: t("ideas.status.rejected"),
    withdrawn: t("ideas.status.withdrawn"),
  }[s];
}

const STATUS_TONE: Record<IdeaStatus, Tone> = {
  open: "neutral",
  claimed: "active",
  proposed: "info",
  accepted: "ok",
  rejected: "danger",
  withdrawn: "neutral",
};

function priorityOptions(t: T): { value: IdeaPriority; label: string }[] {
  return [
    { value: "high", label: t("ideas.priority.high") },
    { value: "normal", label: t("ideas.priority.normal") },
    { value: "low", label: t("ideas.priority.low") },
  ];
}

function name(people: Record<string, Person>, id: string | undefined, t: T) {
  return (id && people[id]?.name) || t("plans.nobody");
}

// ---------------------------------------------------------------- add

function AddIdea({ onAdded }: { onAdded: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [priority, setPriority] = useState<IdeaPriority>("normal");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function add() {
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createIdea(namespace, slug, { title: title.trim(), body, priority });
      setTitle("");
      setBody("");
      setPriority("normal");
      onAdded();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("ideas.add")}>
      <Stack gap="sm">
        <TextField
          label={t("ideas.newTitle")}
          placeholder={t("ideas.newTitlePlaceholder")}
          value={title}
          onChange={setTitle}
          labelled
        />
        <TextArea label={t("ideas.newBody")} value={body} onChange={setBody} rows={3} labelled />
        <Row gap="sm">
          <SelectField
            label={t("ideas.priority")}
            value={priority}
            options={priorityOptions(t)}
            onChange={setPriority}
          />
          <Btn kind="primary" disabled={busy || !title.trim()} onClick={() => void add()}>
            {t("ideas.addButton")}
          </Btn>
        </Row>
        {failure !== undefined && <ErrorState error={failure} />}
      </Stack>
    </Panel>
  );
}

// ---------------------------------------------------------------- lists

function IdeaTable({
  ideas,
  ordered,
  open,
  toggle,
  people,
  onChanged,
}: {
  ideas: Idea[];
  /** The queue: move and reprioritise controls. */
  ordered?: boolean;
  open?: string;
  toggle: (id: string) => void;
  people: Record<string, Person>;
  onChanged: () => void;
}) {
  const t = useT();
  const columns = [
    ...(ordered ? [{ key: "order", label: t("plans.column.order") }] : []),
    { key: "idea", label: t("ideas.column.idea") },
    { key: "state", label: t("ideas.column.state") },
    { key: "actions", align: "r" as const },
  ];
  return (
    <DataTable columns={columns}>
      {ideas.map((idea) => (
        <IdeaRows
          key={idea.id}
          idea={idea}
          line={ordered ? ideas.filter((i) => i.priority === idea.priority) : undefined}
          open={open === idea.id}
          onToggle={() => toggle(idea.id)}
          people={people}
          onChanged={onChanged}
          span={columns.length}
        />
      ))}
    </DataTable>
  );
}

function IdeaRows({
  idea,
  line,
  open,
  onToggle,
  people,
  onChanged,
  span,
}: {
  idea: Idea;
  /** The open ideas of this one's priority, in order; set in the queue. */
  line?: Idea[];
  open: boolean;
  onToggle: () => void;
  people: Record<string, Person>;
  onChanged: () => void;
  span: number;
}) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

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

  // Moves are within the idea's own priority: priority is the first key of
  // the queue, so "up" past a higher-priority idea would be undone by the
  // server's ordering the moment it re-read.
  const index = line ? line.findIndex((i) => i.id === idea.id) : -1;

  let state: ReactNode;
  if (idea.status === "claimed") {
    state = idea.claim_lapsed ? (
      <Badge tone="warn">{t("ideas.lapsed")}</Badge>
    ) : (
      <Cell
        primary={<Badge tone="active">{ideaStatusLabel(t, idea.status)}</Badge>}
        secondary={
          <span className="dim">
            {t("ideas.leaseEnds", {
              name: name(people, idea.claimed_by, t),
              when: f.relative(idea.claim_expires_at ?? idea.updated_at),
            })}
          </span>
        }
      />
    );
  } else if (idea.status === "proposed") {
    state = (
      <Cell
        primary={<Badge tone="info">{ideaStatusLabel(t, idea.status)}</Badge>}
        secondary={
          <span className="dim">
            {t("ideas.proposedBy", {
              name: name(people, idea.claimed_by, t),
              when: f.relative(idea.proposed_at ?? idea.updated_at),
            })}
          </span>
        }
      />
    );
  } else {
    state = <Badge tone={STATUS_TONE[idea.status]}>{ideaStatusLabel(t, idea.status)}</Badge>;
  }

  return (
    <>
      <tr>
        {line && (
          <td className="nowrap">
            <Btn
              small
              label={t("plans.moveUp", { name: idea.title })}
              title={t("plans.moveUp", { name: idea.title })}
              disabled={busy || index <= 0}
              onClick={() =>
                void run(() => api.updateIdea(namespace, slug, idea.id, { position: index }))
              }
            >
              ↑
            </Btn>{" "}
            <Btn
              small
              label={t("plans.moveDown", { name: idea.title })}
              title={t("plans.moveDown", { name: idea.title })}
              disabled={busy || index < 0 || index === line.length - 1}
              onClick={() =>
                void run(() => api.updateIdea(namespace, slug, idea.id, { position: index + 2 }))
              }
            >
              ↓
            </Btn>
          </td>
        )}
        <td>
          <Cell
            primary={idea.title}
            secondary={
              <span className="dim">
                {t("ideas.addedBy", {
                  name: name(people, idea.created_by, t),
                  when: f.relative(idea.created_at),
                })}
              </span>
            }
          />
        </td>
        <td>
          {line ? (
            <SelectField
              label={t("ideas.priorityFor", { name: idea.title })}
              value={idea.priority}
              options={priorityOptions(t)}
              onChange={(priority) =>
                void run(() => api.updateIdea(namespace, slug, idea.id, { priority }))
              }
            />
          ) : (
            state
          )}
        </td>
        <td className="r">
          <Btn small onClick={onToggle}>
            {open ? t("common.close") : t("ideas.show")}
          </Btn>
        </td>
      </tr>
      {failure !== undefined && (
        <tr>
          <td colSpan={span}>
            <ErrorState error={failure} />
          </td>
        </tr>
      )}
      {open && (
        <tr>
          <td colSpan={span}>
            <IdeaDetail id={idea.id} onChanged={onChanged} />
          </td>
        </tr>
      )}
    </>
  );
}

// ---------------------------------------------------------------- detail

/**
 * One idea in full. The proposal is the agent's Markdown shown as text: the
 * app has no Markdown renderer, and turning model output into HTML on a
 * colleague's screen is not a thing to add for a heading or two.
 */
function IdeaDetail({ id, onChanged }: { id: string; onChanged: () => void }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const live = useContext(IdeasLive);
  const idea = usePolled(
    () => api.idea(namespace, slug, id),
    [namespace, slug, id],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  const [reason, setReason] = useState("");
  const [mode, setMode] = useState<"reopen" | "final">("reopen");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      idea.reload();
      onChanged();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (idea.loading) return <Loading />;
  if (idea.error || !idea.data) return <ErrorState error={idea.error} onRetry={idea.reload} />;
  const i = idea.data;
  const live_ = i.status === "open" || i.status === "claimed" || i.status === "proposed";
  const planLink = i.resulting_plan_id
    ? `/p/${namespace}/${slug}/plans?plan=${encodeURIComponent(i.resulting_plan_id)}`
    : undefined;

  return (
    <Stack gap="sm">
      <Defs>
        <Def term={t("ideas.column.state")}>
          <Badge tone={STATUS_TONE[i.status]}>{ideaStatusLabel(t, i.status)}</Badge>
        </Def>
        <Def term={t("ideas.priority")}>
          {priorityOptions(t).find((o) => o.value === i.priority)?.label}
        </Def>
        {i.ticket_key || i.ticket_url ? (
          <Def term={t("ideas.ticket")}>
            {i.ticket_url ? (
              // http(s) only: the server refuses any other scheme.
              <a href={i.ticket_url} target="_blank" rel="noreferrer noopener">
                {i.ticket_key || i.ticket_url}
              </a>
            ) : (
              <Mono>{i.ticket_key}</Mono>
            )}
          </Def>
        ) : null}
        {i.branch && (
          <Def term={t("ideas.branch")}>
            <Mono>{i.branch}</Mono>
          </Def>
        )}
        {i.decided_at && (
          <Def term={t("ideas.decidedBy")}>
            {t("ideas.decidedByWhen", {
              name: name(i.people, i.decided_by, t),
              when: f.stamp(i.decided_at),
            })}
          </Def>
        )}
      </Defs>

      <div>
        <b>{t("ideas.details")}</b>
        <div className="mt-sm">
          {i.body ? <Code>{i.body}</Code> : <span className="dim">{t("ideas.noBody")}</span>}
        </div>
      </div>

      {i.rejection && (
        <Note tone="warn">
          <b>{t("ideas.rejection")}</b>
          <div className="mt-sm">{i.rejection}</div>
        </Note>
      )}

      {i.proposal && (
        <div>
          <b>{t("ideas.proposal")}</b>
          <div className="mt-sm">
            <Code>{i.proposal}</Code>
          </div>
          {planLink && (
            <div className="mt-sm">
              <Link to={planLink}>
                {i.status === "proposed" ? t("ideas.draftPlan") : t("ideas.resultingPlan")}
              </Link>
            </div>
          )}
        </div>
      )}

      {i.status === "proposed" && (
        <Panel title={t("ideas.decide")}>
          <Stack gap="sm">
            <Row gap="sm">
              <Btn
                kind="primary"
                disabled={busy}
                title={t("ideas.acceptHelp")}
                onClick={() => void run(() => api.acceptIdea(namespace, slug, i.id))}
              >
                {t("ideas.accept")}
              </Btn>
            </Row>
            <TextArea
              label={t("ideas.rejectReason")}
              value={reason}
              onChange={setReason}
              rows={3}
              labelled
            />
            <Row gap="sm">
              <Seg
                label={t("ideas.rejectMode")}
                value={mode}
                options={[
                  { value: "reopen", label: t("ideas.reopen") },
                  { value: "final", label: t("ideas.rejectFinal") },
                ]}
                onChange={setMode}
              />
              <Btn
                kind="danger"
                disabled={busy || !reason.trim()}
                onClick={() =>
                  void run(async () => {
                    await api.rejectIdea(namespace, slug, i.id, reason.trim(), mode === "reopen");
                    setReason("");
                  })
                }
              >
                {t("ideas.reject")}
              </Btn>
            </Row>
          </Stack>
        </Panel>
      )}

      {live_ && (
        <Row gap="sm">
          {i.status === "claimed" && (
            <Btn
              small
              disabled={busy}
              title={t("ideas.releaseHelp")}
              onClick={() => void run(() => api.releaseIdea(namespace, slug, i.id))}
            >
              {t("ideas.release")}
            </Btn>
          )}
          <Btn
            small
            kind="danger"
            disabled={busy}
            onClick={() => {
              if (window.confirm(t("ideas.confirmWithdraw", { name: i.title }))) {
                void run(() => api.withdrawIdea(namespace, slug, i.id));
              }
            }}
          >
            {t("ideas.withdraw")}
          </Btn>
        </Row>
      )}
      {failure !== undefined && <ErrorState error={failure} />}
    </Stack>
  );
}
