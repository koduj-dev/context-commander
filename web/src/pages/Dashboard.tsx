import { useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import {
  api,
  principalName,
  type MyProject,
  type Person,
  type Presence,
  type ProjectIdeaCounts,
  type ProjectOverview,
  type UsageSummary,
  type WaitingHandoff,
} from "../lib/api";
import { useLive } from "../lib/live";
import { usePolled, useResource } from "../lib/resource";
import { BOARD_REFRESH_MS, LIVE_REFRESH_MS, PresenceTable } from "./ProjectPlans";
import { MessageComposer, type MessageTarget } from "./AgentMessages";
import { lastDays, UsageFooter, UsageTable } from "./Usage";
import { DoneBars, ProgressCell } from "./WorkOverview";
import {
  Avatar,
  Badge,
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Grid,
  Kpi,
  Loading,
  Mono,
  Note,
  PageHead,
  Panel,
  ProgressLegend,
  Stack,
} from "../ui";

/**
 * The landing screen: is anything waiting on you.
 *
 * One request, `GET /me/handoffs`. This screen used to ask every project for
 * its current handoff, which was N requests and the shape that stops working
 * long before it stops being tempting — a person in thirty projects paid
 * thirty round trips to be told about one. The server answers it in a single
 * query now (`handoffs.Service.ListWaiting`), scoped by RLS and filtered by
 * the capability the role holds.
 *
 * The row carries a project PATH and no project name, because a cross-project
 * read has no business joining a table it does not need. The name comes from
 * /me/projects, which this screen loads anyway for the panel beside it.
 */
type Waiting = { row: WaitingHandoff; project?: MyProject };

/**
 * Attaches each row's project, for its display name and its link.
 *
 * A row whose project is not in the list is kept rather than dropped: the two
 * reads are not one transaction, so a project joined a moment ago can appear
 * here first. Showing the path is worse than showing a name and far better
 * than silently hiding work that is waiting on someone.
 */
function withProjects(rows: WaitingHandoff[], projects: MyProject[]): Waiting[] {
  const byPath = new Map(projects.map((p) => [`${p.namespace}/${p.slug}`, p]));
  return rows.map((row) => ({ row, project: byPath.get(row.project) }));
}

export function Dashboard() {
  const t = useT();
  useCrumbs("dashboard", () => ({ path: [], current: t("dashboard.title") }));
  const f = useFormat();
  const { me } = useAuth();

  const projects = useResource(() => api.myProjects(), []);
  const mine = projects.data ?? [];

  // Independent of the project list: it no longer needs it to make the
  // request, only to label the result. Loading them in parallel rather than
  // in sequence is the whole point of the change.
  const handoffs = useResource(() => api.myHandoffs(), []);
  const waiting = withProjects(handoffs.data ?? [], mine);
  const ideas = useResource(() => api.myIdeaCounts(), []);
  // One live presence read for the table of projects and the panel of who is
  // where, rather than two connections asking the same question.
  const live = useLive(api.myPresenceEvents());
  const presence = usePolled(
    () => api.myPresence(),
    [],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  const week = lastDays(7);
  const fortnight = lastDays(14);
  const weekUsage = useResource(() => api.myUsage({ ...week, group: ["principal"] }), [week.from]);
  const daily = useResource(() => api.myUsage({ ...fortnight, group: ["day"] }), [fortnight.from]);

  return (
    <>
      <PageHead
        title={t("dashboard.title")}
        lead={t("dashboard.lead")}
        actions={<span className="dim">{t("auth.signedInAs", { name: principalName(me) })}</span>}
      />

      <WeekKpis
        usage={weekUsage.data}
        proposals={(ideas.data?.items ?? []).reduce((n, c) => n + c.proposed, 0)}
        queued={(ideas.data?.items ?? []).reduce((n, c) => n + c.open, 0)}
      />

      <Stack>
        <PendingDeletions onRestored={projects.reload} />

        <Portfolio
          projects={mine}
          projectsLoading={projects.loading}
          projectsError={projects.error}
          onRetryProjects={projects.reload}
          presence={presence.data}
          waiting={handoffs.data ?? []}
          ideas={ideas.data?.items ?? []}
        />

        <Grid cols={2}>
          <Panel
            title={t("dashboard.waiting")}
            tight
            footer={<span className="dim">{t("dashboard.waitingLead")}</span>}
          >
            {handoffs.loading ? (
              <Loading />
            ) : handoffs.error ? (
              <ErrorState error={handoffs.error} onRetry={handoffs.reload} />
            ) : waiting.length === 0 ? (
              <EmptyState
                title={t("dashboard.noneWaiting")}
                body={t("dashboard.noneWaitingBody")}
              />
            ) : (
              <DataTable
                columns={[
                  { key: "project", label: t("projects.column.project") },
                  { key: "summary", label: t("handoff.summary") },
                  { key: "status", label: t("handoff.column.status"), align: "r" },
                ]}
              >
                {waiting.map(({ row, project }) => (
                  <tr key={row.id}>
                    <td>
                      <Cell
                        primary={
                          <Link to={`/p/${row.project}`}>{project?.name ?? row.project}</Link>
                        }
                        secondary={<span className="dim">{f.relative(row.created_at)}</span>}
                      />
                    </td>
                    <td>
                      <span className="sec">{row.summary}</span>
                    </td>
                    <td className="r">
                      <Badge tone={row.status === "open" ? "active" : "warn"}>
                        {row.status === "open"
                          ? t("handoff.status.open")
                          : t("handoff.status.claimed")}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>
          <IdeasWaiting
            loading={ideas.loading}
            error={ideas.error}
            reload={ideas.reload}
            items={ideas.data?.items ?? []}
          />
        </Grid>

        <WorkingNow live={live.live} presence={presence} />

        <Grid cols={2}>
          <Panel
            title={t("dashboard.usageDaily", { count: 14 })}
            tight
            footer={daily.data && <UsageFooter summary={daily.data} />}
          >
            {daily.loading ? (
              <Loading />
            ) : daily.error || !daily.data ? (
              <ErrorState error={daily.error} onRetry={daily.reload} />
            ) : (
              <UsageTable
                summary={{ ...daily.data, rows: [...daily.data.rows].reverse() }}
                group="day"
              />
            )}
          </Panel>
          <Panel
            title={t("dashboard.usageByPerson", { count: 7 })}
            tight
            footer={weekUsage.data && <UsageFooter summary={weekUsage.data} />}
          >
            {weekUsage.loading ? (
              <Loading />
            ) : weekUsage.error || !weekUsage.data ? (
              <ErrorState error={weekUsage.error} onRetry={weekUsage.reload} />
            ) : (
              <UsageTable summary={weekUsage.data} group="principal" />
            )}
          </Panel>
        </Grid>
      </Stack>
    </>
  );
}

/**
 * Who is working where, across every project the reader is in — the panel
 * for checking on an agent left running at home. Re-reads when the server
 * says someone moved, and polls while visible when it cannot (docs/adr/0022).
 */
function WorkingNow({ live, presence }: { live: boolean; presence: PresenceRead }) {
  const t = useT();
  const [messageTo, setMessageTo] = useState<MessageTarget>();
  const [sent, setSent] = useState(false);
  return (
    <Panel
      title={t("dashboard.workingNow")}
      sub={live ? t("plans.live") : t("plans.refreshes")}
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
      {presence.loading ? (
        <Loading />
      ) : presence.error || !presence.data ? (
        <ErrorState error={presence.error} onRetry={presence.reload} />
      ) : (
        <PresenceTable
          items={presence.data.items}
          people={presence.data.people}
          showProject
          onMessage={(p) => {
            setSent(false);
            setMessageTo({
              kind: "session",
              project: p.project,
              presence: p,
              person: presence.data?.people[p.principal_id],
            });
          }}
        />
      )}
    </Panel>
  );
}

/**
 * The week in four numbers: time agents and people were present, what they
 * produced in output tokens, what it would have cost at the operator's
 * prices (or how many turns it took, without a price table), and how many
 * idea proposals wait for a person to decide.
 */
function WeekKpis({
  usage,
  proposals,
  queued,
}: {
  usage?: UsageSummary;
  proposals: number;
  queued: number;
}) {
  const t = useT();
  const f = useFormat();
  const total = usage?.total;
  const dash = "—";
  return (
    <Grid cols={4}>
      <Kpi value={total ? f.duration(total.seconds) : dash} label={t("dashboard.kpi.time")} />
      <Kpi value={total ? f.number(total.tokens_out) : dash} label={t("dashboard.kpi.output")} />
      {usage?.currency && total?.cost !== undefined ? (
        <Kpi value={f.money(total.cost, usage.currency)} label={t("dashboard.kpi.cost")} />
      ) : (
        <Kpi value={total ? f.number(total.turns) : dash} label={t("dashboard.kpi.turns")} />
      )}
      <Kpi
        value={f.number(proposals)}
        label={t("dashboard.kpi.proposals", { queued: f.number(queued) })}
        tone={proposals > 0 ? "warn" : undefined}
      />
    </Grid>
  );
}

/**
 * Ideas that wait for something, per project: a proposal for a person to
 * accept or reject comes first, then the queue a planning agent takes from
 * (docs/adr/0026).
 */
function IdeasWaiting({
  loading,
  error,
  reload,
  items,
}: {
  loading: boolean;
  error: unknown;
  reload: () => void;
  items: ProjectIdeaCounts[];
}) {
  const t = useT();
  return (
    <Panel
      title={t("dashboard.ideas")}
      tight
      footer={<span className="dim">{t("dashboard.ideasLead")}</span>}
    >
      {loading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} onRetry={reload} />
      ) : items.length === 0 ? (
        <EmptyState title={t("dashboard.noIdeas")} body={t("dashboard.noIdeasBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "project", label: t("projects.column.project") },
            { key: "proposed", label: t("dashboard.ideasProposed"), align: "r" },
            { key: "open", label: t("dashboard.ideasOpen"), align: "r" },
            { key: "claimed", label: t("dashboard.ideasClaimed"), align: "r" },
          ]}
        >
          {items.map((c) => (
            <tr key={c.project}>
              <td>
                <Cell
                  primary={<Link to={`/p/${c.project}/ideas`}>{c.name}</Link>}
                  secondary={<Mono dim>{c.project}</Mono>}
                />
              </td>
              <td className="r">
                {c.proposed > 0 ? (
                  <Badge tone="warn">{t("dashboard.ideasDecide", { count: c.proposed })}</Badge>
                ) : (
                  <span className="dim">0</span>
                )}
              </td>
              <td className="r">
                <span className="num">{c.open}</span>
              </td>
              <td className="r">
                <span className="num">{c.claimed}</span>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}

type PresenceData = { items: Presence[]; people: Record<string, Person> };
type PresenceRead = {
  data?: PresenceData;
  loading: boolean;
  error?: unknown;
  reload: () => void;
};

/** How many of a project's people to draw before "+n". */
const AVATARS_SHOWN = 4;

/**
 * Every project in one row each: how far its plans are, what is in progress
 * and blocked, who is on it now, what it finished each day, what waits on the
 * reader, and when it last moved. Most recently active first.
 *
 * The numbers come from one read (`GET /me/projects/overview`); who is on it
 * and what waits are the reads this screen makes anyway, joined by path. A
 * project whose role cannot read plans still gets its row, with the reason.
 *
 * Either read failing shows its error above whatever the other one returned:
 * the list of projects is the way into every project, so a failed aggregate
 * must not take the links with it, and a failed list must not read as "no
 * projects".
 */
function Portfolio({
  projects,
  projectsLoading,
  projectsError,
  onRetryProjects,
  presence,
  waiting,
  ideas,
}: {
  projects: MyProject[];
  projectsLoading: boolean;
  projectsError?: unknown;
  onRetryProjects: () => void;
  presence?: PresenceData;
  waiting: WaitingHandoff[];
  ideas: ProjectIdeaCounts[];
}) {
  const t = useT();
  const f = useFormat();
  const overview = useResource(() => api.myProjectsOverview(), []);

  const rows: { path: string; name: string; o?: ProjectOverview }[] = (
    overview.data?.items ?? []
  ).map((o) => ({ path: o.project, name: o.name, o }));
  const listed = new Set(rows.map((r) => r.path));
  for (const p of projects) {
    const path = `${p.namespace}/${p.slug}`;
    if (!listed.has(path)) rows.push({ path, name: p.name });
  }

  const people = new Map<string, string[]>();
  for (const p of presence?.items ?? []) {
    if (p.idle) continue;
    const names = people.get(p.project) ?? [];
    const name = presence?.people[p.principal_id]?.name ?? p.client_type;
    if (!names.includes(name)) names.push(name);
    people.set(p.project, names);
  }
  const waitingOn = new Map<string, number>();
  for (const h of waiting) waitingOn.set(h.project, (waitingOn.get(h.project) ?? 0) + 1);
  for (const c of ideas) waitingOn.set(c.project, (waitingOn.get(c.project) ?? 0) + c.proposed);

  const days = overview.data?.days ?? 14;
  return (
    <Panel
      title={t("overview.portfolio")}
      sub={t("projects.count", { count: rows.length })}
      tight
      footer={
        <Stack gap="sm">
          <ProgressLegend
            labels={{
              done: t("overview.legend.done"),
              doing: t("overview.legend.doing"),
              blocked: t("overview.legend.blocked"),
              open: t("overview.legend.open"),
            }}
          />
          <span className="dim">{t("overview.portfolioLead")}</span>
        </Stack>
      }
    >
      {overview.loading || projectsLoading ? (
        <Loading />
      ) : (
        <>
          {projectsError ? <ErrorState error={projectsError} onRetry={onRetryProjects} /> : null}
          {overview.error ? <ErrorState error={overview.error} onRetry={overview.reload} /> : null}
          {rows.length === 0 ? (
            projectsError || overview.error ? null : (
              <EmptyState title={t("dashboard.noProjects")} body={t("projects.emptyBody")} />
            )
          ) : (
            <DataTable
              columns={[
                { key: "project", label: t("projects.column.project") },
                { key: "progress", label: t("overview.col.progress") },
                { key: "doing", label: t("overview.col.doing"), align: "r" },
                { key: "blocked", label: t("overview.col.blocked"), align: "r" },
                { key: "people", label: t("overview.col.people") },
                { key: "activity", label: t("overview.col.activity", { count: days }) },
                { key: "waiting", label: t("overview.col.waiting"), align: "r" },
                { key: "last", label: t("overview.col.last"), align: "r" },
              ]}
            >
              {rows.map(({ path, name, o }) => {
                const here = people.get(path) ?? [];
                const wait = waitingOn.get(path) ?? 0;
                return (
                  <tr key={path}>
                    <td>
                      <Cell
                        primary={<Link to={`/p/${path}`}>{name}</Link>}
                        secondary={<Mono dim>{path}</Mono>}
                      />
                    </td>
                    <td>
                      {o ? (
                        <ProgressCell counts={o.counts} />
                      ) : overview.error ? (
                        <span className="dim">—</span>
                      ) : (
                        <span className="dim">{t("overview.noPlanAccess")}</span>
                      )}
                    </td>
                    <td className="r">
                      <span className="num">{o ? f.number(o.counts.in_progress) : "—"}</span>
                    </td>
                    <td className="r">
                      {o && o.counts.blocked > 0 ? (
                        <Badge tone="danger">{f.number(o.counts.blocked)}</Badge>
                      ) : (
                        <span className="dim">{o ? "0" : "—"}</span>
                      )}
                    </td>
                    <td>
                      {here.length === 0 ? (
                        <span className="dim">{t("overview.nobody")}</span>
                      ) : (
                        <div className="row row-sm">
                          {here.slice(0, AVATARS_SHOWN).map((n) => (
                            <Avatar key={n} name={n} />
                          ))}
                          {here.length > AVATARS_SHOWN && (
                            <span className="dim">+{f.number(here.length - AVATARS_SHOWN)}</span>
                          )}
                        </div>
                      )}
                    </td>
                    <td>
                      {o && overview.data ? (
                        <DoneBars from={overview.data.from} values={o.done_per_day} spark />
                      ) : (
                        <span className="dim">—</span>
                      )}
                    </td>
                    <td className="r">
                      {wait > 0 ? (
                        <Badge tone="warn">{t("overview.waitingCount", { count: wait })}</Badge>
                      ) : (
                        <span className="dim">—</span>
                      )}
                    </td>
                    <td className="r">
                      <span className="dim">
                        {o?.last_activity ? f.relative(o.last_activity) : t("overview.never")}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </DataTable>
          )}
        </>
      )}
    </Panel>
  );
}

/**
 * The reader's projects scheduled for deletion (docs/adr/0036). Nothing
 * else lists them any more, so this is the only way back before the purge.
 * Not drawn when there are none, or when the list cannot be read: an empty
 * panel here would only be noise on every visit.
 */
function PendingDeletions({ onRestored }: { onRestored: () => void }) {
  const t = useT();
  const f = useFormat();
  const list = useResource(() => api.pendingDeletions(), []);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const items = list.data?.items ?? [];
  if (items.length === 0) return null;

  async function restore(id: string) {
    setBusy(true);
    setFailure(undefined);
    try {
      await api.cancelDeletion(id);
      list.reload();
      onRestored();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title={t("dashboard.pendingDeletion")}
      tight
      footer={<span className="dim">{t("dashboard.pendingDeletionLead")}</span>}
    >
      {failure !== undefined && <ErrorState error={failure} />}
      <DataTable
        columns={[
          { key: "project", label: t("projects.column.project") },
          { key: "purge", label: t("dashboard.purgeColumn"), align: "r" },
          { key: "actions", label: "", align: "r" },
        ]}
      >
        {items.map((p) => (
          <tr key={p.id}>
            <td>
              <Cell primary={p.name} secondary={<Mono dim>{p.project}</Mono>} />
            </td>
            <td className="r">
              <span
                className="dim"
                title={t("dashboard.purgeAfter", { date: f.date(p.purge_after) })}
              >
                {f.date(p.purge_after)}
              </span>
            </td>
            <td className="r">
              <Btn small onClick={() => void restore(p.id)} disabled={busy}>
                {t("dashboard.cancelDeletion")}
              </Btn>
            </td>
          </tr>
        ))}
      </DataTable>
    </Panel>
  );
}
