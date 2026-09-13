import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { useList } from "../lib/resource";
import {
  Badge,
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
 * The audit trail for this project.
 *
 * Result is the column people actually scan: a denial here is the record of
 * someone being refused, and it is the reason this screen exists at all.
 * Denials are coloured and carry a word, never colour alone.
 */
export function ProjectActivity() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  const list = useList(
    (cursor) => api.audit(namespace, slug, cursor),
    [namespace, slug],
  );

  return (
    <Panel
      title={t("activity.title")}
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
      <p className="sec lead-out">{t("activity.lead")}</p>

      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : list.items.length === 0 ? (
        <EmptyState title={t("activity.empty")} body={t("activity.emptyBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "time", label: t("activity.column.time") },
            { key: "action", label: t("activity.column.action") },
            { key: "resource", label: t("activity.column.resource") },
            { key: "principal", label: t("activity.column.principal") },
            { key: "result", label: t("activity.column.result"), align: "r" },
          ]}
        >
          {list.items.map((r) => (
            <tr key={r.id}>
              <td className="nowrap">
                <span className="dim" title={f.showUTC ? f.utc(r.timestamp) : undefined}>
                  {f.stamp(r.timestamp)}
                </span>
                {f.showUTC && (
                  <>
                    <br />
                    <span className="dim">{f.utc(r.timestamp)}</span>
                  </>
                )}
              </td>
              <td>
                <Mono>{r.action}</Mono>
              </td>
              <td>
                <Mono dim truncate>
                  {r.resource_id ? `${r.resource_type}/${r.resource_id}` : r.resource_type}
                </Mono>
              </td>
              <td>
                <span className="dim">{r.actor_email ?? r.principal_id ?? t("common.unknown")}</span>
              </td>
              <td className="r">
                <ResultBadge result={r.result} />
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}

function ResultBadge({ result }: { result: string }) {
  const t = useT();
  if (result === "deny") return <Badge tone="danger">{t("activity.result.deny")}</Badge>;
  if (result === "error") return <Badge tone="warn">{t("activity.result.error")}</Badge>;
  if (result === "allow") return <Badge tone="ok">{t("activity.result.allow")}</Badge>;
  return <Badge>{result}</Badge>;
}
