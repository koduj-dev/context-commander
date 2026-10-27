import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api, type InstallationTrends } from "../lib/api";
import { useResource } from "../lib/resource";
import {
  Badge,
  DataTable,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Grid,
  Kpi,
  Loading,
  Meter,
  Mono,
  PageHead,
  Panel,
  Stack,
} from "../ui";

/**
 * How much this installation is used.
 *
 * Everything here is a count. That is not a limitation of the screen but the
 * reason it can exist: an installation admin is deliberately not a member of
 * every project and cannot read project content, so a figure naming a path,
 * a summary or a person would widen that boundary rather than report on it
 * (migrations/000024 argues the decision).
 *
 * The numbers are grouped by the question they answer — how big is this, what
 * does it hold, what is it coordinating, is it alive — because two dozen
 * numbers in one list is the same data and a worse answer.
 */
export function AdminUsage() {
  const t = useT();
  const f = useFormat();
  useCrumbs("admin:usage", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.usage"),
  }));

  const { data, error, loading, reload } = useResource(() => api.installationStats(), []);

  if (loading) return <Loading />;
  if (error || !data) return <ErrorState error={error} onRetry={reload} />;

  const { scale, content, coordination, activity, credentials } = data;
  const handoffsTotal =
    coordination.handoffs_open + coordination.handoffs_claimed + coordination.handoffs_closed;

  // What share of handoffs are still in play. A high share is not a fault —
  // it is work waiting — so the meter warns rather than alarms.
  const inPlay = handoffsTotal
    ? Math.round(
        ((coordination.handoffs_open + coordination.handoffs_claimed) / handoffsTotal) * 100,
      )
    : 0;

  const issued = credentials.active + credentials.revoked + credentials.expired;
  const inUse = issued ? Math.round((credentials.used_last_7_days / issued) * 100) : 0;

  return (
    <>
      <PageHead title={t("admin.usage")} lead={t("admin.usageLead")} />

      {/* The headline four. Each caption says what the number is OF, so the
          tile reads without its panel — a number whose label names a
          different metric is worse than no label. */}
      <Grid cols={4}>
        <Panel>
          <Kpi
            value={f.number(scale.projects)}
            label={t("admin.usage.inOrganizations", { count: scale.organizations })}
          />
        </Panel>
        <Panel>
          <Kpi
            value={f.number(scale.users)}
            label={t("admin.usage.andAgents", { count: scale.service_principals })}
          />
        </Panel>
        <Panel>
          <Kpi
            value={f.bytes(content.stored_bytes)}
            label={t("admin.usage.acrossArtifacts", { count: content.artifacts })}
          />
        </Panel>
        <Panel>
          <Kpi
            value={f.number(credentials.used_last_7_days)}
            label={t("admin.usage.ofIssued", { count: issued })}
            tone={issued > 0 && credentials.used_last_7_days === 0 ? "warn" : undefined}
          />
          <div className="mt-sm">
            <Meter percent={inUse} tone={inUse === 0 && issued > 0 ? "warn" : undefined} />
          </div>
        </Panel>
      </Grid>

      <div className="mt">
        <Grid cols="2-1">
          <Stack>
            <Panel
              title={t("admin.usage.content")}
              tight
              footer={<span className="dim">{t("admin.usage.storedHelp")}</span>}
            >
              <DataTable columns={[{ key: "what" }, { key: "n", align: "r" }]}>
                <Count label={t("admin.usage.artifacts")} value={f.number(content.artifacts)} />
                <Count
                  label={t("admin.usage.versions")}
                  value={f.number(content.artifact_versions)}
                />
                <Count label={t("admin.usage.stored")} value={f.bytes(content.stored_bytes)} />
                <Count label={t("admin.usage.secrets")} value={f.number(content.secrets)} />
                <Count
                  label={t("admin.usage.conversations")}
                  value={f.number(content.conversations)}
                />
                <Count
                  label={t("admin.usage.historyObjects")}
                  value={f.number(content.history_objects)}
                />
              </DataTable>
            </Panel>

            <Panel title={t("admin.usage.coordination")} tight>
              <DataTable columns={[{ key: "what" }, { key: "n", align: "r" }]}>
                <Count label={t("admin.usage.skills")} value={f.number(coordination.skills)} />
                <Count
                  label={t("admin.usage.skillVersions")}
                  value={f.number(coordination.skill_versions)}
                />
                <Count
                  label={t("admin.usage.skillBindings")}
                  value={f.number(coordination.skill_bindings)}
                />
                <Count
                  label={t("admin.usage.gateBindings")}
                  value={f.number(coordination.gate_bindings)}
                />
              </DataTable>
            </Panel>
          </Stack>

          <Stack top>
            <Panel title={t("admin.usage.handoffs")}>
              {handoffsTotal === 0 ? (
                <EmptyState title={t("handoff.none")} />
              ) : (
                <>
                  <Defs>
                    <Def term={t("admin.usage.handoffsOpen")}>
                      <Badge tone="active">{f.number(coordination.handoffs_open)}</Badge>
                    </Def>
                    <Def term={t("admin.usage.handoffsClaimed")}>
                      <Badge tone="warn">{f.number(coordination.handoffs_claimed)}</Badge>
                    </Def>
                    <Def term={t("admin.usage.handoffsClosed")}>
                      <Badge>{f.number(coordination.handoffs_closed)}</Badge>
                    </Def>
                  </Defs>
                  <div className="mt-sm">
                    <Meter percent={inPlay} tone={inPlay > 80 ? "warn" : undefined} />
                  </div>
                </>
              )}
            </Panel>

            <Panel
              title={t("admin.usage.credentials")}
              footer={<span className="dim">{t("admin.usage.usedRecentlyHelp")}</span>}
            >
              <Defs>
                <Def term={t("admin.usage.active")}>
                  <Badge tone="ok">{f.number(credentials.active)}</Badge>
                </Def>
                <Def term={t("admin.usage.expired")}>
                  <Badge tone="warn">{f.number(credentials.expired)}</Badge>
                </Def>
                <Def term={t("admin.usage.revoked")}>
                  <Badge tone="danger">{f.number(credentials.revoked)}</Badge>
                </Def>
              </Defs>
            </Panel>

            <Panel title={t("admin.usage.activity")}>
              {activity.events === 0 ? (
                <EmptyState title={t("admin.usage.quiet")} body={t("admin.usage.quietBody")} />
              ) : (
                <Defs>
                  <Def term={t("admin.usage.events")}>
                    <Mono>{f.number(activity.events)}</Mono>
                  </Def>
                  <Def term={t("admin.usage.auditRecords")}>
                    <Mono>{f.number(activity.audit_records)}</Mono>
                  </Def>
                  <Def term={t("admin.usage.securityEvents")}>
                    <Mono>{f.number(activity.security_events)}</Mono>
                  </Def>
                  {activity.newest_activity && (
                    <Def term={t("admin.usage.newestActivity")}>
                      <span className="dim">{f.relative(activity.newest_activity)}</span>
                    </Def>
                  )}
                  {activity.oldest_project && (
                    <Def term={t("admin.usage.oldestProject")}>
                      <span className="dim">{f.date(activity.oldest_project)}</span>
                    </Def>
                  )}
                </Defs>
              )}
            </Panel>
          </Stack>
        </Grid>
      </div>

      <div className="mt">
        <Trends />
      </div>
    </>
  );
}

/** Twelve weeks: a quarter, which is long enough to see a trend and short
 *  enough that every row fits without a pager. */
const TREND_WEEKS = 12;

/**
 * Usage over time and the handoff loop — loaded separately from the counts
 * above, so a failure here leaves the rest of the screen standing.
 */
function Trends() {
  const { data, error, loading, reload } = useResource(
    () => api.installationTrends(TREND_WEEKS),
    [],
  );
  if (loading) return <Loading />;
  if (error || !data) return <ErrorState error={error} onRetry={reload} />;
  return (
    <Grid cols="2-1">
      <Weekly weeks={data.weeks} />
      <HandoffLoop flow={data.handoff_flow} />
    </Grid>
  );
}

function Weekly({ weeks }: { weeks: InstallationTrends["weeks"] }) {
  const t = useT();
  const f = useFormat();
  const none = <span className="dim">—</span>;
  // Newest first: the question a reader brings is "what about this week",
  // and the series arrives oldest first because that is the order to draw.
  const rows = [...weeks].reverse();

  return (
    <Panel
      title={t("admin.usage.weekly", { count: weeks.length })}
      tight
      footer={<span className="dim">{t("admin.usage.weeklyHelp")}</span>}
    >
      <DataTable
        columns={[
          { key: "week", label: t("admin.usage.week") },
          { key: "people", label: t("admin.usage.people"), align: "r" },
          { key: "agents", label: t("admin.usage.agents"), align: "r" },
          { key: "created", label: t("admin.usage.handedOff"), align: "r" },
          { key: "claimed", label: t("admin.usage.claimed"), align: "r" },
          { key: "other", label: t("admin.usage.byOtherClient"), align: "r" },
          { key: "toClaim", label: t("admin.usage.medianToClaim"), align: "r" },
          { key: "events", label: t("admin.usage.events"), align: "r" },
        ]}
      >
        {rows.map((w) => (
          <tr key={w.week_start}>
            <td>
              <span className="sec">{f.calendarDate(w.week_start)}</span>
            </td>
            <td className="r">
              <span className="num">{f.number(w.active_users)}</span>
            </td>
            <td className="r">
              <span className="num">{f.number(w.active_agents)}</span>
            </td>
            <td className="r">
              <span className="num">{f.number(w.handoffs_created)}</span>
            </td>
            <td className="r">
              <span className="num">{f.number(w.handoffs_claimed)}</span>
            </td>
            <td className="r">
              <span className="num">{f.number(w.handoffs_claimed_by_other_client)}</span>
            </td>
            <td className="r">
              {w.median_seconds_to_claim === undefined ? (
                none
              ) : (
                <span className="num">{f.duration(w.median_seconds_to_claim)}</span>
              )}
            </td>
            <td className="r">
              <span className="num">{f.number(w.events)}</span>
            </td>
          </tr>
        ))}
      </DataTable>
    </Panel>
  );
}

/**
 * Whether handing work from one agent to another actually happens, and how
 * fast — NEXT.md's "time to first handoff", read from what the server
 * records. It says nothing about whether the next agent understood what it
 * was handed; the footer says so, because a screen of confident numbers
 * invites the reader to assume it does.
 */
function HandoffLoop({ flow }: { flow: InstallationTrends["handoff_flow"] }) {
  const t = useT();
  const f = useFormat();
  const time = (seconds?: number) =>
    seconds === undefined ? (
      <span className="dim">{t("admin.usage.notYet")}</span>
    ) : (
      <Mono>{f.duration(seconds)}</Mono>
    );

  return (
    <Panel
      title={t("admin.usage.loop")}
      footer={<span className="dim">{t("admin.usage.loopHelp")}</span>}
    >
      {flow.handoffs === 0 ? (
        <EmptyState title={t("handoff.none")} />
      ) : (
        <Defs>
          <Def term={t("admin.usage.byOtherClient")}>
            <Mono>{f.number(flow.claimed_by_other_client)}</Mono>{" "}
            <span className="dim">{t("admin.usage.ofClaimed", { count: flow.claimed })}</span>
          </Def>
          <Def term={t("admin.usage.projectsWithTakeover")}>
            <Mono>{f.number(flow.projects_with_takeover)}</Mono>{" "}
            <span className="dim">{t("admin.usage.ofProjects", { count: flow.projects })}</span>
          </Def>
          <Def term={t("admin.usage.firstTakeover")}>
            {time(flow.median_seconds_to_first_takeover)}
          </Def>
          <Def term={t("admin.usage.medianToClaim")}>{time(flow.median_seconds_to_claim)}</Def>
          <Def term={t("admin.usage.p90ToClaim")}>{time(flow.p90_seconds_to_claim)}</Def>
          <Def term={t("admin.usage.claimToClose")}>{time(flow.median_seconds_claim_to_close)}</Def>
          <Def term={t("admin.usage.staleOpen")}>
            <Badge tone={flow.stale_open > 0 ? "warn" : undefined}>
              {f.number(flow.stale_open)}
            </Badge>
          </Def>
        </Defs>
      )}
    </Panel>
  );
}

function Count({ label, value }: { label: string; value: string }) {
  return (
    <tr>
      <td>
        <span className="sec">{label}</span>
      </td>
      <td className="r">
        <span className="num">{value}</span>
      </td>
    </tr>
  );
}
