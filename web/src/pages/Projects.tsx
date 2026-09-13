import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { api, type MyProject } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  PageHead,
  Panel,
  TextField,
} from "../ui";

/**
 * Every project this principal is a member of.
 *
 * `/me/projects` returns the whole set rather than a page — membership is
 * bounded by how many projects a person was added to, not by how much data
 * the system holds — so the filter below is local. It is a filter, not a
 * search: it narrows what is already on screen and makes no request.
 */
export function Projects() {
  const t = useT();
  useCrumbs("projects", () => ({ path: [], current: t("projects.title") }));
  const [filter, setFilter] = useState("");
  const { data, error, loading, reload } = useResource(() => api.myProjects(), []);

  const rows = useMemo(() => {
    const all = data ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return all;
    return all.filter((p) => path(p).toLowerCase().includes(q) || p.name.toLowerCase().includes(q));
  }, [data, filter]);

  return (
    <>
      <PageHead title={t("projects.title")} lead={t("projects.lead")} />

      <Panel
        sub={t("projects.count", { count: rows.length })}
        title={t("projects.title")}
        right={
          <TextField
            label={t("projects.filter")}
            type="search"
            value={filter}
            onChange={setFilter}
            placeholder={t("projects.filter")}
          />
        }
        tight
      >
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : rows.length === 0 ? (
          data?.length === 0 ? (
            <EmptyState title={t("projects.empty")} body={t("projects.emptyBody")} />
          ) : (
            <EmptyState title={t("empty.noResults")} body={t("empty.noResultsBody")} />
          )
        ) : (
          <DataTable
            columns={[
              { key: "project", label: t("projects.column.project") },
              { key: "role", label: t("projects.column.role") },
            ]}
          >
            {rows.map((p) => (
              <tr key={path(p)}>
                <td>
                  <Cell
                    primary={<Link to={`/p/${p.namespace}/${p.slug}`}>{p.name}</Link>}
                    secondary={<Mono dim>{path(p)}</Mono>}
                  />
                </td>
                <td>
                  <Badge tone={p.role === "OWNER" ? "active" : "neutral"}>
                    {roleLabel(t, p.role)}
                  </Badge>
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>
    </>
  );
}

function path(p: MyProject) {
  return `${p.namespace}/${p.slug}`;
}
