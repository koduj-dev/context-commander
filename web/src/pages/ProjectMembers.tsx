import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useList } from "../lib/resource";
import {
  Badge,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Pager,
  Panel,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * Who may act in this project.
 *
 * There is no "add member" or "change role" control, because the HTTP adapter
 * exposes no write for either — membership is granted through the CLI today.
 * A button that opened a dialog and then failed would be worse than its
 * absence.
 */
export function ProjectMembers() {
  const t = useT();
  const f = useFormat();
  const { me } = useAuth();
  const { namespace, slug } = useProject();

  const list = useList(() => api.members(namespace, slug), [namespace, slug]);

  return (
    <Panel title={t("members.title")} sub={t("members.count", { count: list.items.length })} tight
      footer={
        <Pager
          shown={list.items.length}
          hasMore={list.hasMore}
          onLoadMore={list.loadMore}
          busy={list.loadingMore}
        />
      }
    >
      <p className="sec lead-out">{t("members.lead")}</p>

      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : list.items.length === 0 ? (
        <EmptyState title={t("members.title")} body={t("members.lead")} />
      ) : (
        <DataTable
          columns={[
            { key: "principal", label: t("members.column.principal") },
            { key: "role", label: t("members.column.role") },
            { key: "added", label: t("members.column.added"), align: "r" },
          ]}
        >
          {list.items.map((m) => (
            <tr key={m.principal_id}>
              <td>
                <Cell
                  primary={
                    <>
                      {m.display_name ?? m.email ?? <Mono>{m.principal_id}</Mono>}
                      {m.principal_id === me?.principal_id && (
                        <span className="inline-badge">{t("common.you")}</span>
                      )}
                    </>
                  }
                  secondary={m.email && m.display_name ? <span className="dim">{m.email}</span> : undefined}
                />
              </td>
              <td>
                <Badge tone={m.role === "OWNER" ? "active" : "neutral"}>{roleLabel(t, m.role)}</Badge>
              </td>
              <td className="r">
                <span className="dim">{f.date(m.created_at)}</span>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}
