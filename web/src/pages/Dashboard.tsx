import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  Note,
  PageHead,
  Panel,
  Stack,
} from "../ui";

/**
 * The landing screen.
 *
 * Deliberately thin. Everything a real dashboard would show — open handoffs
 * across projects, recent refusals, storage — needs an endpoint that spans
 * projects, and none exists: `/me/projects` is the only cross-project read
 * the server has. So this shows what can actually be answered and says
 * nothing about the rest, rather than rendering panels full of plausible
 * numbers (see .claude/skills/ui-design: never fake a backend capability).
 */
export function Dashboard() {
  const t = useT();
  const f = useFormat();
  const { me } = useAuth();
  const { data, error, loading, reload } = useResource(() => api.myProjects(), []);

  const projects = data ?? [];

  return (
    <>
      <PageHead
        title={t("dashboard.title")}
        lead={t("dashboard.lead")}
        actions={<span className="dim">{t("auth.signedInAs", { name: me?.display_name ?? "" })}</span>}
      />

      <Grid cols="2-1">
        <Stack>
          <Panel title={t("dashboard.yourProjects")} sub={t("projects.count", { count: projects.length })} tight>
            {loading ? (
              <Loading />
            ) : error ? (
              <ErrorState error={error} onRetry={reload} />
            ) : projects.length === 0 ? (
              <EmptyState title={t("dashboard.noProjects")} body={t("projects.emptyBody")} />
            ) : (
              <DataTable
                columns={[
                  { key: "p", label: t("projects.column.project") },
                  { key: "r", label: t("projects.column.role") },
                ]}
              >
                {projects.map((p) => (
                  <tr key={p.project.id}>
                    <td>
                      <Cell
                        primary={
                          <Link to={`/p/${p.project.namespace}/${p.project.slug}`}>{p.project.name}</Link>
                        }
                        secondary={
                          <Mono dim>{`${p.project.namespace}/${p.project.slug}`}</Mono>
                        }
                      />
                    </td>
                    <td>
                      <Badge tone={p.role === "OWNER" ? "active" : "neutral"}>{roleLabel(t, p.role)}</Badge>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>
        </Stack>

        <Stack top>
          <Panel title={t("dashboard.recent")}>
            {/* An honest placeholder. There is no cross-project activity
                endpoint; per-project activity is on the project's own tab. */}
            <EmptyState title={t("dashboard.activityHere")} />
            <Note tone="info">{t("project.notBuilt.artifacts")}</Note>
          </Panel>

          <Panel title={t("prefs.title")} sub={f.locale}>
            <p className="sec lead-out">{t("prefs.lead")}</p>
            <Link className="btn btn-sm" to="/preferences">
              {t("nav.preferences")}
            </Link>
          </Panel>
        </Stack>
      </Grid>
    </>
  );
}
