import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import { ApiError, api, principalName, type HandoffDetail, type MyProject } from "../lib/api";
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
  PageHead,
  Panel,
  Stack,
} from "../ui";

/**
 * The landing screen: is anything waiting on you.
 *
 * There is no cross-project endpoint for open handoffs, so this asks each
 * project for its current one. That is N requests, which is acceptable here
 * and nowhere else: membership is bounded by how many projects a person was
 * added to, the calls run in parallel, and a project with no open handoff
 * answers 404 — an answer, not a failure.
 *
 * The alternative was to show nothing and call it honest. Showing nothing is
 * only honest when the answer is unobtainable, and this one is obtainable.
 */
// `current` returns the body as well as the status, which is why a summary
// can be shown here and not in a handoff list.
type Waiting = { project: MyProject; handoff: HandoffDetail };

async function waitingOn(projects: MyProject[]): Promise<Waiting[]> {
  const results = await Promise.all(
    projects.map(async (project) => {
      try {
        const handoff = await api.currentHandoff(project.namespace, project.slug);
        return { project, handoff };
      } catch (err) {
        // 404 is "nothing is open here", which is the common case. Anything
        // else is a real failure for that one project and is left out rather
        // than failing the whole screen — one unreachable project should not
        // hide the others.
        if (err instanceof ApiError && err.kind === "not_found") return null;
        return null;
      }
    }),
  );
  return results.filter((r): r is Waiting => r !== null);
}

export function Dashboard() {
  const t = useT();
  useCrumbs("dashboard", () => ({ path: [], current: t("dashboard.title") }));
  const f = useFormat();
  const { me } = useAuth();

  const projects = useResource(() => api.myProjects(), []);
  const mine = projects.data ?? [];

  const waiting = useResource(
    () => (projects.data ? waitingOn(projects.data) : Promise.resolve([])),
    [projects.data],
  );

  return (
    <>
      <PageHead
        title={t("dashboard.title")}
        lead={t("dashboard.lead")}
        actions={<span className="dim">{t("auth.signedInAs", { name: principalName(me) })}</span>}
      />

      <Grid cols="2-1">
        <Stack>
          <Panel
            title={t("dashboard.waiting")}
            tight
            footer={<span className="dim">{t("dashboard.waitingLead")}</span>}
          >
            {projects.loading || waiting.loading ? (
              <Loading />
            ) : projects.error ? (
              <ErrorState error={projects.error} onRetry={projects.reload} />
            ) : (waiting.data ?? []).length === 0 ? (
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
                {(waiting.data ?? []).map(({ project, handoff }) => (
                  <tr key={`${project.namespace}/${project.slug}`}>
                    <td>
                      <Cell
                        primary={
                          <Link to={`/p/${project.namespace}/${project.slug}`}>{project.name}</Link>
                        }
                        secondary={<span className="dim">{f.relative(handoff.created_at)}</span>}
                      />
                    </td>
                    <td>
                      <span className="sec">{handoff.summary}</span>
                    </td>
                    <td className="r">
                      <Badge tone={handoff.status === "open" ? "active" : "warn"}>
                        {handoff.status === "open"
                          ? t("handoff.status.open")
                          : t("handoff.status.claimed")}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </DataTable>
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
