import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import { api, principalName, type MyProject, type WaitingHandoff } from "../lib/api";
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
