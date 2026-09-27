import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
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
    </>
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
