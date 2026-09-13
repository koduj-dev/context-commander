import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
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
import { useSearchParams } from "react-router-dom";

/**
 * Organizations, and what can be seen inside one.
 *
 * There is no `GET /organizations/{id}`, so the name shown on the right comes
 * from the list on the left rather than from a second read — the list is the
 * only place the API states it.
 */
export function Organizations() {
  const t = useT();
  useCrumbs("organizations", () => ({ path: [], current: t("orgs.title") }));
  const [params, setParams] = useSearchParams();
  const selected = params.get("org") ?? "";

  const { data, error, loading, reload } = useResource(() => api.myOrganizations(), []);
  const orgs = data ?? [];
  const current = orgs.find((o) => o.id === selected);

  return (
    <>
      <PageHead title={t("orgs.title")} lead={t("orgs.lead")} />

      <Grid cols="2-1">
        <Stack>
          <Panel title={t("orgs.title")} tight>
            {loading ? (
              <Loading />
            ) : error ? (
              <ErrorState error={error} onRetry={reload} />
            ) : orgs.length === 0 ? (
              <EmptyState title={t("orgs.empty")} body={t("orgs.emptyBody")} />
            ) : (
              <DataTable
                columns={[
                  { key: "name", label: t("orgs.column.name") },
                  { key: "role", label: t("orgs.column.role"), align: "r" },
                ]}
              >
                {orgs.map((o) => (
                  <tr key={o.id} className={o.id === selected ? "active" : ""}>
                    <td>
                      <a
                        href={`?org=${o.id}`}
                        onClick={(e) => {
                          e.preventDefault();
                          setParams({ org: o.id }, { replace: true });
                        }}
                      >
                        {o.name}
                      </a>
                      <div className="sub-line">
                        <Mono dim truncate>
                          {o.id}
                        </Mono>
                      </div>
                    </td>
                    <td className="r">
                      <Badge tone={o.role === "OWNER" ? "active" : "neutral"}>
                        {roleLabel(t, o.role)}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>

          {current && <OrganizationAudit id={current.id} />}
        </Stack>

        <Stack top>
          {current ? (
            <OrganizationMembers id={current.id} name={current.name} />
          ) : (
            <Panel title={t("orgs.select")}>
              <EmptyState title={t("orgs.select")} body={t("orgs.selectBody")} />
            </Panel>
          )}
        </Stack>
      </Grid>
    </>
  );
}

function OrganizationMembers({ id, name }: { id: string; name: string }) {
  const t = useT();
  const f = useFormat();
  const { data, error, loading, reload } = useResource(() => api.organizationMembers(id), [id]);
  const members = data ?? [];

  return (
    <Panel
      title={t("orgs.members")}
      sub={name}
      tight
      footer={<span className="dim">{t("orgs.membersLead")}</span>}
    >
      {loading ? (
        <Loading />
      ) : error ? (
        <ErrorState error={error} onRetry={reload} />
      ) : members.length === 0 ? (
        <EmptyState title={t("orgs.members")} />
      ) : (
        <DataTable
          columns={[
            { key: "user", label: t("orgs.column.user") },
            { key: "role", label: t("members.column.role") },
            { key: "added", label: t("members.column.added"), align: "r" },
          ]}
        >
          {members.map((m) => (
            <tr key={m.user_id}>
              <td>
                <Mono truncate>{m.user_id}</Mono>
              </td>
              <td>
                <Badge tone={m.role === "OWNER" ? "active" : "neutral"}>
                  {roleLabel(t, m.role)}
                </Badge>
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

/**
 * Organization-scoped audit.
 *
 * A separate policy scope in the schema (migration 000023): these are the
 * records about the organization itself — who was added, what was mandated —
 * not a roll-up of its projects' activity.
 */
function OrganizationAudit({ id }: { id: string }) {
  const t = useT();
  const f = useFormat();
  const list = useList((cursor) => api.organizationAudit(id, cursor), [id]);

  return (
    <Panel
      title={t("orgs.audit")}
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
        <EmptyState title={t("activity.empty")} body={t("activity.emptyBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "time", label: t("activity.column.time") },
            { key: "action", label: t("activity.column.action") },
            { key: "principal", label: t("activity.column.principal") },
            { key: "result", label: t("activity.column.result"), align: "r" },
          ]}
        >
          {list.items.map((r) => (
            <tr key={r.id}>
              <td className="nowrap">
                <span className="dim">{f.stamp(r.timestamp)}</span>
              </td>
              <td>
                <Mono>{r.action}</Mono>
              </td>
              <td>
                <span className="dim">
                  {r.actor_email ?? r.principal_id ?? t("common.unknown")}
                </span>
              </td>
              <td className="r">
                <Badge tone={r.result === "deny" ? "danger" : r.result === "error" ? "warn" : "ok"}>
                  {r.result === "deny"
                    ? t("activity.result.deny")
                    : r.result === "error"
                      ? t("activity.result.error")
                      : t("activity.result.allow")}
                </Badge>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}
