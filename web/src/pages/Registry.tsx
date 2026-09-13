import { useState } from "react";
import { useT } from "../i18n";
import { api } from "../lib/api";
import { scopeLabel, trustLabel } from "../lib/labels";
import { useDebounced, useList } from "../lib/resource";
import {
  Badge,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Pager,
  PageHead,
  Panel,
  Scope,
  TextField,
} from "../ui";

/**
 * The catalogue an agent searches, rendered for a person.
 *
 * Scoped, not public: what comes back is what this principal can see —
 * skills in their projects, in their organizations, and on the installation
 * shelf. The scope column is not decoration; it is the difference between
 * "everyone here has this" and "this lives in one project".
 */
export function Registry() {
  const t = useT();
  const [query, setQuery] = useState("");
  const debounced = useDebounced(query);

  const list = useList(
    (cursor) => api.searchSkills(debounced || undefined, cursor),
    [debounced],
  );

  return (
    <>
      <PageHead title={t("registry.title")} lead={t("registry.lead")} />

      <Panel
        title={t("registry.title")}
        sub={debounced ? t("registry.results", { count: list.items.length, query: debounced }) : undefined}
        right={
          <TextField
            label={t("registry.search")}
            type="search"
            value={query}
            onChange={setQuery}
            placeholder={t("registry.search")}
          />
        }
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
          debounced ? (
            <EmptyState title={t("empty.noResults")} body={t("empty.noResultsBody")} />
          ) : (
            <EmptyState title={t("registry.empty")} body={t("registry.emptyBody")} />
          )
        ) : (
          <DataTable
            columns={[
              { key: "skill", label: t("skills.column.skill") },
              { key: "scope", label: t("registry.livesAt") },
              { key: "latest", label: t("registry.column.latest"), align: "r" },
              { key: "trust", label: t("skills.column.trust"), align: "r" },
            ]}
          >
            {list.items.map((s) => (
              <tr key={s.skill_id}>
                <td>
                  <Cell
                    primary={s.name}
                    secondary={
                      <Mono dim truncate>
                        {s.latest_checksum}
                      </Mono>
                    }
                  />
                </td>
                <td>
                  <Scope kind={s.scope}>{scopeLabel(t, s.scope)}</Scope>
                </td>
                <td className="r">
                  <span className="num">v{s.latest_version}</span>
                </td>
                <td className="r">
                  <Badge tone={s.trust_status === "signed" ? "ok" : "warn"}>
                    {trustLabel(t, s.trust_status)}
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
