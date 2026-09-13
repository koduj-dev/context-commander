import { useSearchParams } from "react-router-dom";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api, decodeHistory } from "../lib/api";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Cell,
  Code,
  DataTable,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  PageHead,
  Pager,
  Panel,
  Stack,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * What clients recorded of their own sessions.
 *
 * Read-only, and that is not a gap: history is ingested by the client that
 * lived it. A person writing a "conversation" from this screen would be
 * recording something that never happened.
 */
export function ProjectHistory() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const [params, setParams] = useSearchParams();
  const selected = params.get("c") ?? "";

  const list = useList((cursor) => api.conversations(namespace, slug, cursor), [namespace, slug]);

  return (
    <>
      <PageHead title={t("history.title")} lead={t("history.lead")} />

      <Grid cols="2-1">
        <Stack>
          {selected ? (
            <Conversation id={selected} />
          ) : (
            <Panel title={t("history.select")}>
              <EmptyState title={t("history.select")} body={t("history.selectBody")} />
            </Panel>
          )}
        </Stack>

        <Stack top>
          <Panel
            title={t("history.title")}
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
            {list.loading ? (
              <Loading />
            ) : list.error ? (
              <ErrorState error={list.error} onRetry={list.reload} />
            ) : list.items.length === 0 ? (
              <EmptyState title={t("history.empty")} body={t("history.emptyBody")} />
            ) : (
              <DataTable
                columns={[
                  { key: "c", label: t("history.column.conversation") },
                  { key: "by", label: t("history.column.by") },
                  { key: "started", label: t("history.column.started"), align: "r" },
                ]}
              >
                {list.items.map((c) => (
                  <tr
                    key={c.id}
                    className={c.id === selected ? "pick active" : "pick"}
                    onClick={() => setParams({ c: c.id }, { replace: true })}
                  >
                    <td>
                      <a href={`?c=${c.id}`} onClick={(e) => e.preventDefault()}>
                        <Mono truncate>{c.id}</Mono>
                      </a>
                    </td>
                    <td>
                      <Mono dim truncate>
                        {c.created_by_principal}
                      </Mono>
                    </td>
                    <td className="r">
                      <span className="dim">{f.relative(c.created_at)}</span>
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

function Conversation({ id }: { id: string }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  const objects = useList(
    (cursor) => api.historyObjects(namespace, slug, id, cursor),
    [namespace, slug, id],
  );
  const lineage = useResource(() => api.lineage(namespace, slug, id), [namespace, slug, id]);
  const edges = lineage.data ?? [];

  return (
    <>
      <Panel
        title={t("history.objects")}
        sub={<Mono truncate>{id}</Mono>}
        tight
        footer={
          <Pager
            shown={objects.items.length}
            hasMore={objects.hasMore}
            onLoadMore={objects.loadMore}
            busy={objects.loadingMore}
          />
        }
      >
        {objects.loading ? (
          <Loading />
        ) : objects.error ? (
          <ErrorState error={objects.error} onRetry={objects.reload} />
        ) : objects.items.length === 0 ? (
          <EmptyState title={t("history.noObjects")} />
        ) : (
          <DataTable
            columns={[
              { key: "type", label: t("history.column.type") },
              { key: "content", label: t("artifacts.content") },
              { key: "at", label: t("history.column.recorded"), align: "r" },
            ]}
          >
            {objects.items.map((o) => {
              const text = decodeHistory(o.content);
              return (
                <tr key={o.id}>
                  <td>
                    <Cell
                      primary={<Badge>{o.type}</Badge>}
                      secondary={
                        <Mono dim truncate>
                          {o.content_hash}
                        </Mono>
                      }
                    />
                  </td>
                  <td>
                    {text === null ? (
                      <Cell
                        primary={<span className="dim">{t("history.notText")}</span>}
                        secondary={<span className="dim">{t("history.notTextBody")}</span>}
                      />
                    ) : (
                      <Code>{text}</Code>
                    )}
                  </td>
                  <td className="r nowrap">
                    <span className="dim">{f.relative(o.created_at)}</span>
                  </td>
                </tr>
              );
            })}
          </DataTable>
        )}
      </Panel>

      <Panel
        title={t("history.lineage")}
        tight
        footer={<span className="dim">{t("history.lineageLead")}</span>}
      >
        {lineage.loading ? (
          <Loading />
        ) : lineage.error ? (
          <ErrorState error={lineage.error} onRetry={lineage.reload} />
        ) : edges.length === 0 ? (
          <EmptyState title={t("history.noLineage")} body={t("history.noLineageBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "from", label: t("history.column.from") },
              { key: "kind", label: t("history.column.kind") },
              { key: "to", label: t("history.column.to") },
            ]}
          >
            {edges.map((e) => (
              <tr key={`${e.from_id}->${e.to_id}`}>
                <td>
                  <Mono truncate>{e.from_id}</Mono>
                </td>
                <td>
                  <Badge tone="info">{e.kind}</Badge>
                </td>
                <td>
                  <Mono truncate>{e.to_id}</Mono>
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>
    </>
  );
}
