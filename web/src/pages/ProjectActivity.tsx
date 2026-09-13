import { useState } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { useList } from "../lib/resource";
import {
  Badge,
  DataTable,
  Seg,
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
  const [view, setView] = useState<"audit" | "events">("audit");

  return (
    <>
      <div className="mb">
        <Seg
          label={t("activity.title")}
          value={view}
          onChange={setView}
          options={[
            { value: "audit", label: t("activity.title") },
            { value: "events", label: t("events.title") },
          ]}
        />
      </div>
      {view === "audit" ? <AuditView /> : <EventsView />}
    </>
  );
}

function AuditView() {
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

/**
 * The same timeline, seen as the outbox sees it.
 *
 * Kept behind a switch on the same screen rather than given a tab of its own:
 * events and audit answer the same question from two sides — what the system
 * published versus what it decided — and someone comparing them should not
 * have to navigate between two pages to do it.
 */
function EventsView() {
  const t = useT();
  const { namespace, slug } = useProject();

  const list = useList((cursor) => api.events(namespace, slug, cursor), [namespace, slug]);

  return (
    <Panel
      title={t("events.title")}
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
      <p className="sec lead-out">{t("events.lead")}</p>

      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : list.items.length === 0 ? (
        <EmptyState title={t("events.empty")} body={t("activity.emptyBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "seq", label: t("events.column.seq"), align: "r" },
            { key: "type", label: t("events.column.type") },
            { key: "resource", label: t("events.column.resource") },
            { key: "payload", label: t("events.column.payload"), align: "r" },
          ]}
        >
          {list.items.map((e) => (
            <tr key={e.seq}>
              <td className="r">
                <span className="num">{e.seq}</span>
              </td>
              <td>
                <Mono>{e.type}</Mono>
              </td>
              <td>
                <Mono dim truncate>
                  {e.resource_id ? `${e.resource_type}/${e.resource_id}` : e.resource_type}
                </Mono>
              </td>
              <td className="r">
                <span className="dim">{e.payload_kind}</span>
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
