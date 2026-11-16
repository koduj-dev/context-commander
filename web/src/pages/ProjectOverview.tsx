import { useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import {
  ApiError,
  api,
  type Board,
  type Handoff,
  type HandoffDetail,
  type MemoryKind,
  type Person,
} from "../lib/api";
import { useLive } from "../lib/live";
import { useList, usePolled, useResource } from "../lib/resource";
import { BOARD_REFRESH_MS, LIVE_REFRESH_MS, PresenceTable } from "./ProjectPlans";
import { MessageComposer, canMessage, type MessageTarget } from "./AgentMessages";
import { lastDays } from "./Usage";
import { DoneBars, ProgressCell, windowDays, workTotal } from "./WorkOverview";
import {
  Badge,
  Bars,
  Btn,
  Cell,
  Kpi,
  Meter,
  ProgressLegend,
  TextField,
  DataTable,
  Pager,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  Note,
  Panel,
  Stack,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * What is going on in this project right now: how far the work is, who is on
 * it, what waits on a person, what was finished lately, and what was decided.
 *
 * The board is the spine (`…/plans/board`, live like the Plans tab); the
 * per-day numbers come from `…/plans/overview`. Time per day and branches come
 * from the usage ledger, which only clients that report usage fill — so those
 * panels say so when it is empty rather than drawing a flat line of zeros.
 */
export function ProjectOverview() {
  const t = useT();
  const { project, namespace, slug } = useProject();

  const live = useLive(api.planBoardEvents(namespace, slug));
  const board = usePolled(
    () => api.planBoard(namespace, slug),
    [namespace, slug],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  const handoff = useResource(() => api.currentHandoff(namespace, slug), [namespace, slug]);

  // "There is no open handoff" is a 404 from this endpoint, and it is an
  // answer rather than a failure — rendering it as an error would put a red
  // panel on a project where nothing is wrong.
  const noneOpen = handoff.error instanceof ApiError && handoff.error.kind === "not_found";

  return (
    <Stack>
      {board.data && <OverviewKpis board={board.data} />}
      <Grid cols="2-1">
        <Stack>
          {board.loading ? (
            <Loading />
          ) : board.error || !board.data ? (
            <ErrorState error={board.error} onRetry={board.reload} />
          ) : (
            <>
              <WorkingOn board={board.data} live={live.live} />
              <Phases board={board.data} />
            </>
          )}
          <LastDays />
          <Branches />
        </Stack>

        <Stack top>
          <Panel
            title={t("handoff.current")}
            right={handoff.data && <HandoffStatus handoff={handoff.data} />}
          >
            {handoff.loading ? (
              <Loading />
            ) : noneOpen ? (
              <>
                <EmptyState title={t("handoff.none")} body={t("handoff.noneBody")} />
                <CreateHandoff onCreated={handoff.reload} />
              </>
            ) : handoff.error ? (
              <ErrorState error={handoff.error} onRetry={handoff.reload} />
            ) : handoff.data ? (
              <HandoffBody handoff={handoff.data} onChanged={handoff.reload} />
            ) : null}
          </Panel>

          <WaitingOnPerson
            blocked={board.data?.blocked}
            boardLoading={board.loading}
            people={board.data?.people ?? {}}
          />
          <RecentMemories />
          <HandoffHistory />

          <Panel title={t("project.identity")}>
            <Defs>
              <Def term={t("project.path")}>
                <Mono>{`${namespace}/${slug}`}</Mono>
              </Def>
              <Def term={t("project.uuid")}>
                <Mono truncate>{project.id}</Mono>
              </Def>
              <Def term={t("scope.project")}>{project.visibility}</Def>
            </Defs>
          </Panel>
        </Stack>
      </Grid>
    </Stack>
  );
}

/** The project in four numbers, from the board. */
function OverviewKpis({ board }: { board: Board }) {
  const t = useT();
  const f = useFormat();
  const c = { open: 0, in_progress: 0, done: 0, blocked: 0, skipped: 0 };
  for (const p of board.plans) {
    c.open += p.counts.open;
    c.in_progress += p.counts.in_progress;
    c.done += p.counts.done;
    c.blocked += p.counts.blocked;
    c.skipped += p.counts.skipped;
  }
  const total = workTotal(c);
  const now = new Set(board.presence.filter((p) => !p.idle).map((p) => p.principal_id)).size;
  return (
    <Grid cols={4}>
      <Kpi
        value={total ? f.percent(c.done / total) : "—"}
        label={t("overview.kpi.done", { done: f.number(c.done), total: f.number(total) })}
      />
      <Kpi value={f.number(c.in_progress)} label={t("overview.kpi.doing")} />
      <Kpi
        value={f.number(c.blocked)}
        label={t("overview.kpi.blocked")}
        tone={c.blocked > 0 ? "warn" : undefined}
      />
      <Kpi value={f.number(now)} label={t("overview.kpi.people")} />
    </Grid>
  );
}

/** Who is on what, with a message to any agent session (docs/adr/0032). */
function WorkingOn({ board, live }: { board: Board; live: boolean }) {
  const t = useT();
  const { namespace, slug } = useProject();
  const path = `${namespace}/${slug}`;
  const [messageTo, setMessageTo] = useState<MessageTarget>();
  const [sent, setSent] = useState(false);
  return (
    <Panel
      title={t("plans.workingNow")}
      sub={live ? t("plans.live") : t("plans.refreshes")}
      right={
        board.presence.some(canMessage) ? (
          <Btn
            small
            onClick={() => {
              setSent(false);
              setMessageTo({ kind: "project", project: path });
            }}
          >
            {t("messages.everyone")}
          </Btn>
        ) : undefined
      }
      footer={
        messageTo ? (
          <MessageComposer
            target={messageTo}
            onSent={() => {
              setMessageTo(undefined);
              setSent(true);
            }}
            onCancel={() => setMessageTo(undefined)}
          />
        ) : sent ? (
          <Note tone="info">{t("messages.sentNote")}</Note>
        ) : undefined
      }
      tight
    >
      <PresenceTable
        items={board.presence}
        people={board.people}
        onMessage={(p) => {
          setSent(false);
          setMessageTo({
            kind: "session",
            project: path,
            presence: p,
            person: board.people[p.principal_id],
          });
        }}
      />
    </Panel>
  );
}

const PHASE_TONE = { planned: "neutral", active: "active", done: "ok" } as const;
const PHASE_LABEL = {
  planned: "plans.phase.planned",
  active: "plans.phase.active",
  done: "plans.phase.done",
} as const;

/** Each phase as one bar, with the time tracked on it. */
function Phases({ board }: { board: Board }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  return (
    <Panel
      title={t("plans.phases")}
      tight
      footer={
        board.phases.length > 0 && (
          <ProgressLegend
            labels={{
              done: t("overview.legend.done"),
              doing: t("overview.legend.doing"),
              blocked: t("overview.legend.blocked"),
              open: t("overview.legend.open"),
            }}
          />
        )
      }
    >
      {board.phases.length === 0 ? (
        <EmptyState title={t("overview.noPhases")} body={t("overview.noPhasesBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "phase", label: t("plans.phases") },
            { key: "progress", label: t("overview.col.progress") },
            { key: "time", label: t("overview.col.time"), align: "r" },
          ]}
        >
          {board.phases.map((ph) => (
            <tr key={ph.id}>
              <td>
                <Cell
                  primary={<Link to={`/p/${namespace}/${slug}/plans`}>{ph.title}</Link>}
                  secondary={
                    <Badge tone={PHASE_TONE[ph.status]}>{t(PHASE_LABEL[ph.status])}</Badge>
                  }
                />
              </td>
              <td>
                <ProgressCell counts={ph.counts} />
              </td>
              <td className="r">
                <span className="num">
                  {ph.effort.seconds > 0
                    ? t("overview.phaseTime", { time: f.duration(ph.effort.seconds) })
                    : "—"}
                </span>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}

const WINDOW = 14;
const BRANCH_WINDOW = 7;

/** Items finished per day beside time reported per day. */
function LastDays() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const span = lastDays(WINDOW);
  const overview = useResource(
    () => api.projectOverview(namespace, slug, WINDOW),
    [namespace, slug],
  );
  const usage = useResource(
    () => api.projectUsage(namespace, slug, { ...span, group: ["day"] }),
    [namespace, slug, span.from],
  );
  const days = windowDays(span.from, WINDOW);
  const secondsByDay = new Map((usage.data?.rows ?? []).map((r) => [r.day ?? "", r.seconds]));
  const seconds = days.map((d) => secondsByDay.get(d) ?? 0);
  const anyTime = seconds.some((s) => s > 0);
  const axis = (
    <div className="bars-axis" aria-hidden="true">
      <span>{f.calendarDate(days[0] ?? span.from)}</span>
      <span>{f.calendarDate(days[days.length - 1] ?? span.to)}</span>
    </div>
  );
  const done = overview.data?.items[0];

  return (
    <Panel title={t("overview.days", { count: WINDOW })}>
      <Grid cols={2}>
        <div>
          <div className="label-caps">{t("overview.finished")}</div>
          {overview.loading ? (
            <Loading />
          ) : overview.error || !overview.data || !done ? (
            <ErrorState error={overview.error} onRetry={overview.reload} />
          ) : (
            <>
              <DoneBars from={overview.data.from} values={done.done_per_day} />
              {axis}
            </>
          )}
        </div>
        <div>
          <div className="label-caps">{t("overview.timePerDay")}</div>
          {usage.loading ? (
            <Loading />
          ) : usage.error ? (
            <ErrorState error={usage.error} onRetry={usage.reload} />
          ) : !anyTime ? (
            <p className="dim">{t("overview.noTime")}</p>
          ) : (
            <>
              <Bars
                values={seconds}
                titles={seconds.map((s, k) =>
                  t("overview.timeDay", {
                    day: f.calendarDate(days[k] ?? ""),
                    time: f.duration(s),
                  }),
                )}
                label={t("overview.timeAria", {
                  values: seconds.map((s) => f.duration(s)).join(", "),
                })}
              />
              {axis}
            </>
          )}
        </div>
      </Grid>
    </Panel>
  );
}

/**
 * Time per branch and ticket over the last week, from the usage ledger. Not
 * drawn at all when nothing was reported: usage is optional, and an empty
 * panel would read as "no work happened".
 */
function Branches() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const span = lastDays(BRANCH_WINDOW);
  const usage = useResource(
    () => api.projectUsage(namespace, slug, { ...span, group: ["branch", "ticket"] }),
    [namespace, slug, span.from],
  );
  const rows = (usage.data?.rows ?? [])
    .filter((r) => r.seconds > 0)
    .sort((a, b) => b.seconds - a.seconds);
  if (rows.length === 0) return null;
  const max = rows[0]?.seconds ?? 1;
  return (
    <Panel title={t("overview.branches", { count: BRANCH_WINDOW })} tight>
      <DataTable
        columns={[
          { key: "branch", label: t("overview.col.branch") },
          { key: "ticket", label: t("overview.col.ticket") },
          { key: "time", label: t("overview.col.time"), align: "r" },
        ]}
      >
        {rows.map((r) => (
          <tr key={`${r.branch ?? ""}\u0000${r.ticket_key ?? ""}`}>
            <td>
              {r.branch ? (
                <Mono>{r.branch}</Mono>
              ) : (
                <span className="dim">{t("overview.noBranch")}</span>
              )}
            </td>
            <td>{r.ticket_key ? <Mono>{r.ticket_key}</Mono> : <span className="dim">—</span>}</td>
            <td className="r">
              <div className="stack-sm">
                <span className="num">{f.duration(r.seconds)}</span>
                <Meter percent={(r.seconds / max) * 100} />
              </div>
            </td>
          </tr>
        ))}
      </DataTable>
    </Panel>
  );
}

/**
 * What a person has to act on: proposals to accept or reject, and blocked
 * steps. The current handoff has its own panel above this one.
 *
 * `blocked` is undefined while the board loads or when it failed (the board's
 * own panel shows that error): then nothing is claimed about blocked steps.
 */
function WaitingOnPerson({
  blocked,
  boardLoading,
  people,
}: {
  blocked?: Board["blocked"];
  boardLoading: boolean;
  people: Record<string, Person>;
}) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const proposals = useResource(() => api.ideas(namespace, slug, ["proposed"]), [namespace, slug]);
  const ideas = proposals.data?.items ?? [];
  const stuck = blocked ?? [];
  return (
    <Panel
      title={t("overview.waiting")}
      tight
      footer={<span className="dim">{t("overview.waitingLead")}</span>}
    >
      {proposals.loading || boardLoading ? (
        <Loading />
      ) : proposals.error ? (
        <ErrorState error={proposals.error} onRetry={proposals.reload} />
      ) : ideas.length === 0 && stuck.length === 0 ? (
        blocked ? (
          <EmptyState title={t("overview.waitingNone")} body={t("overview.waitingNoneBody")} />
        ) : (
          <EmptyState title={t("overview.noProposals")} />
        )
      ) : (
        <ul className="feed">
          {ideas.map((i) => (
            <li key={i.id}>
              <Badge tone="shared">{t("overview.kind.proposal")}</Badge>
              <div className="what">
                <Link to={`/p/${namespace}/${slug}/ideas`}>{i.title}</Link>
                <div className="sub-line">
                  {[
                    proposals.data?.people[i.claimed_by ?? ""]?.name,
                    i.proposed_at && f.relative(i.proposed_at),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
              </div>
            </li>
          ))}
          {stuck.map((b) => (
            <li key={b.item.id}>
              <Badge tone="danger">{t("overview.kind.blocked")}</Badge>
              <div className="what">
                <Link to={`/p/${namespace}/${slug}/plans?plan=${b.item.plan_id}&item=${b.item.id}`}>
                  {b.item.text}
                </Link>
                <div className="sub-line">
                  {[
                    b.plan_title,
                    people[b.item.updated_by ?? ""]?.name,
                    f.relative(b.item.updated_at),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

const RECENT_KINDS: MemoryKind[] = ["decision", "gotcha", "finding"];
const RECENT_SHOWN = 5;
const KIND_TONE = { decision: "ok", gotcha: "warn", finding: "info" } as const;

/** The latest decisions, gotchas and findings: what the next session should know. */
function RecentMemories() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  // One read per kind, filtered by the server: filtering the first page of
  // every kind here would lose a decision behind a page of notes. Each list
  // is newest first, so the newest few of the union are among their heads.
  const list = useResource(async () => {
    const pages = await Promise.all(
      RECENT_KINDS.map((kind) => api.memories(namespace, slug, { kind })),
    );
    const items = pages
      .flatMap((p) => p.items)
      .sort(
        (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id),
      )
      .slice(0, RECENT_SHOWN);
    return {
      items,
      people: Object.assign({}, ...pages.map((p) => p.people)) as Record<string, Person>,
    };
  }, [namespace, slug]);
  const items = list.data?.items ?? [];
  return (
    <Panel
      title={t("overview.memories")}
      tight
      right={<Link to={`/p/${namespace}/${slug}/memories`}>{t("overview.memoriesAll")}</Link>}
    >
      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : items.length === 0 ? (
        <EmptyState title={t("overview.memoriesNone")} />
      ) : (
        <ul className="feed">
          {items.map((m) => (
            <li key={m.id}>
              <Badge tone={KIND_TONE[m.kind as keyof typeof KIND_TONE] ?? "neutral"}>
                {t(`memories.kind.${m.kind}`)}
              </Badge>
              <div className="what">
                <span>{m.title}</span>
                <div className="sub-line">
                  {[
                    list.data?.people[m.created_by]?.name,
                    m.branch,
                    m.ticket_key,
                    f.relative(m.created_at),
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

/**
 * What came before.
 *
 * Paginated by cursor like everything that grows. Bodies are not fetched: the
 * list endpoint returns status and timestamps only, and pulling a body per
 * row to show a preview would be one request per row for text nobody asked
 * to read.
 */
function HandoffHistory() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  const list = useList((cursor) => api.handoffs(namespace, slug, cursor), [namespace, slug]);

  if (list.loading) return null;
  if (list.error) return <ErrorState error={list.error} onRetry={list.reload} />;
  if (list.items.length === 0) return null;

  return (
    <Panel
      title={t("handoff.history")}
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
      <DataTable
        columns={[
          { key: "created", label: t("handoff.column.created") },
          { key: "status", label: t("handoff.column.status"), align: "r" },
        ]}
      >
        {list.items.map((h) => (
          <tr key={h.id}>
            <td>
              <span className="dim">{f.relative(h.created_at)}</span>
            </td>
            <td className="r">
              <HandoffStatus handoff={h} />
            </td>
          </tr>
        ))}
      </DataTable>
    </Panel>
  );
}

function HandoffStatus({ handoff }: { handoff: Handoff }) {
  const t = useT();
  const tone =
    handoff.status === "open" ? "active" : handoff.status === "claimed" ? "warn" : "neutral";
  const label =
    handoff.status === "open"
      ? t("handoff.status.open")
      : handoff.status === "claimed"
        ? t("handoff.status.claimed")
        : t("handoff.status.closed");
  return <Badge tone={tone}>{label}</Badge>;
}

function HandoffBody({ handoff, onChanged }: { handoff: HandoffDetail; onChanged: () => void }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const { me } = useAuth();
  const [lost, setLost] = useState(false);

  async function claim() {
    setLost(false);
    try {
      await api.claimHandoff(namespace, slug, handoff.id);
    } catch (err) {
      // A 409 here is the coordination answer — another client won the race —
      // and saying so is more useful than a silently reloaded screen.
      if (err instanceof ApiError && err.kind === "conflict") setLost(true);
    } finally {
      onChanged();
    }
  }

  async function close() {
    // Confirmed because it is the one action here that cannot be undone: a
    // closed handoff is no longer the current one and nobody can claim it.
    if (!window.confirm(t("handoff.confirmClose"))) return;
    try {
      await api.closeHandoff(namespace, slug, handoff.id);
    } catch {
      // Same reasoning as claim: reloading shows the state that won.
    } finally {
      onChanged();
    }
  }

  return (
    <>
      <p className="sec lead-out">{handoff.summary}</p>

      {handoff.completed_work && (
        <div className="mt">
          <div className="label-caps">{t("handoff.completed")}</div>
          <p className="sec">{handoff.completed_work}</p>
        </div>
      )}

      {handoff.next_actions && (
        <div className="mt">
          <div className="label-caps">{t("handoff.next")}</div>
          <p className="sec">{handoff.next_actions}</p>
        </div>
      )}

      {handoff.referenced_artifacts && handoff.referenced_artifacts.length > 0 && (
        <div className="mt">
          <div className="label-caps">{t("handoff.references")}</div>
          {handoff.referenced_artifacts.map((id) => (
            <div key={id}>
              <Mono dim truncate>
                {id}
              </Mono>
            </div>
          ))}
        </div>
      )}

      <div className="mt row row-sm">
        <span className="dim">{f.relative(handoff.created_at)}</span>
        <span className="spacer" />
        {handoff.status === "open" && (
          <Btn kind="primary" small onClick={() => void claim()}>
            {t("handoff.claim")}
          </Btn>
        )}
        {handoff.status !== "closed" && (
          <Btn small onClick={() => void close()}>
            {t("handoff.close")}
          </Btn>
        )}
      </div>

      {lost && (
        <div className="mt">
          <Note tone="warn">
            <b>{t("handoff.alreadyClaimed")}</b>
            <div className="mt-sm">{t("handoff.alreadyClaimedBody")}</div>
          </Note>
        </div>
      )}

      {handoff.status === "claimed" && (
        <div className="mt">
          <Note tone="warn">
            {handoff.claimed_by_principal === me?.principal_id
              ? t("handoff.claimedByYou")
              : handoff.claimed_by_principal
                ? t("handoff.claimedBy", { name: handoff.claimed_by_principal })
                : t("handoff.status.claimed")}
          </Note>
        </div>
      )}
    </>
  );
}

/**
 * Leaving a handoff from the interface.
 *
 * The same three fields an agent writes over MCP, because it is the same
 * record: a person picking up after an agent, or handing back to one, should
 * not be writing a different shape of note.
 */
function CreateHandoff({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();

  const [summary, setSummary] = useState("");
  const [completed, setCompleted] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!summary.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createHandoff(namespace, slug, {
        summary: summary.trim(),
        completed_work: completed.trim(),
        next_actions: next.trim(),
      });
      setSummary("");
      setCompleted("");
      setNext("");
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt stack-sm">
      <TextField
        label={t("handoff.summary")}
        value={summary}
        onChange={setSummary}
        placeholder={t("handoff.summaryHelp")}
        grow
      />
      <TextField
        label={t("handoff.completed")}
        value={completed}
        onChange={setCompleted}
        placeholder={t("handoff.completedHelp")}
        grow
      />
      <TextField
        label={t("handoff.next")}
        value={next}
        onChange={setNext}
        placeholder={t("handoff.nextHelp")}
        grow
      />
      <div className="row row-sm">
        <span className="dim">{t("handoff.createLead")}</span>
        <span className="spacer" />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !summary.trim()}>
          {busy ? t("common.loading") : t("handoff.create")}
        </Btn>
      </div>
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}
