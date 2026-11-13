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
  type ProjectIdeaCounts,
  type UsageSummary,
  type WaitingHandoff,
} from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useLive } from "../lib/live";
import { usePolled, useResource } from "../lib/resource";
import { BOARD_REFRESH_MS, LIVE_REFRESH_MS, PresenceTable } from "./ProjectPlans";
import { MessageComposer, type MessageTarget } from "./AgentMessages";
import { lastDays, UsageFooter, UsageTable } from "./Usage";
import {
  Badge,
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

      <Grid cols="2-1">
        <Stack>
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
          <WorkingNow />
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
        </Stack>

        <Stack top>
          <Panel
            title={t("dashboard.yourProjects")}
            sub={t("projects.count", { count: mine.length })}
            tight
          >
            {projects.loading ? (
              <Loading />
            ) : projects.error ? (
              <ErrorState error={projects.error} onRetry={projects.reload} />
            ) : mine.length === 0 ? (
              <EmptyState title={t("dashboard.noProjects")} body={t("projects.emptyBody")} />
            ) : (
              <DataTable
                columns={[
                  { key: "p", label: t("projects.column.project") },
                  { key: "r", label: t("projects.column.role"), align: "r" },
                ]}
              >
                {mine.map((p) => (
                  <tr key={`${p.namespace}/${p.slug}`}>
                    <td>
                      <Cell
                        primary={<Link to={`/p/${p.namespace}/${p.slug}`}>{p.name}</Link>}
                        secondary={<Mono dim>{`${p.namespace}/${p.slug}`}</Mono>}
                      />
                    </td>
                    <td className="r">
                      <Badge tone={p.role === "OWNER" ? "active" : "neutral"}>
                        {roleLabel(t, p.role)}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>
        </Stack>
      </Grid>
    </>
  );
}

/**
 * Who is working where, across every project the reader is in — the panel
 * for checking on an agent left running at home. Re-reads when the server
 * says someone moved, and polls while visible when it cannot (docs/adr/0022).
 */
function WorkingNow() {
  const t = useT();
  const live = useLive(api.myPresenceEvents());
  const presence = usePolled(
    () => api.myPresence(),
    [],
    live.live ? LIVE_REFRESH_MS : BOARD_REFRESH_MS,
    live.pulse,
  );
  const [messageTo, setMessageTo] = useState<MessageTarget>();
  const [sent, setSent] = useState(false);
  return (
    <Panel
      title={t("dashboard.workingNow")}
      sub={live.live ? t("plans.live") : t("plans.refreshes")}
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
