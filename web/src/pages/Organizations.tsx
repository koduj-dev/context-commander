import { useState } from "react";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Copyable,
  DataTable,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  PageHead,
  Note,
  Pager,
  Panel,
  Row,
  SelectField,
  Stack,
  TextField,
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
  const [creating, setCreating] = useState(false);
  const selected = params.get("org") ?? "";

  const { data, error, loading, reload } = useResource(() => api.myOrganizations(), []);
  const orgs = data ?? [];
  const current = orgs.find((o) => o.id === selected);

  return (
    <>
      <PageHead
        title={t("orgs.title")}
        lead={t("orgs.lead")}
        actions={
          <Btn kind="primary" onClick={() => setCreating((v) => !v)}>
            {t("orgs.create")}
          </Btn>
        }
      />

      {creating && (
        <div className="mb">
          <CreateOrganization
            onCreated={(id) => {
              reload();
              setParams({ org: id }, { replace: true });
              setCreating(false);
            }}
            onCancel={() => setCreating(false)}
          />
        </div>
      )}

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
                        <Copyable value={o.id} />
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
            <OrganizationMembers id={current.id} name={current.name} role={current.role} />
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

function OrganizationMembers({ id, name, role }: { id: string; name: string; role: string }) {
  const t = useT();
  const f = useFormat();
  const { data, error, loading, reload } = useResource(() => api.organizationMembers(id), [id]);
  const members = data ?? [];
  const canAdd = role === "OWNER" || role === "ADMIN";

  return (
    <>
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
                  <Copyable value={m.user_id} />
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

      {canAdd && <AddOrganizationMember id={id} onAdded={reload} />}
    </>
  );
}

function CreateOrganization({
  onCreated,
  onCancel,
}: {
  onCreated: (id: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!name.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const org = await api.createOrganization(name.trim());
      onCreated(org.id);
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("orgs.create")}>
      <p className="sec lead-out">{t("orgs.createLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("orgs.name")}
          value={name}
          onChange={setName}
          placeholder={t("orgs.name")}
          grow
        />
        <Btn onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !name.trim()}>
          {busy ? t("common.loading") : t("orgs.create")}
        </Btn>
      </Row>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
    </Panel>
  );
}

/**
 * Adding someone to an organization.
 *
 * By user id rather than principal id: an organization role is about a
 * person (docs/adr/0006), which is also why a service principal never has
 * one. Nothing in the API lists users, so there is nothing to pick from and
 * the field says so.
 */
function AddOrganizationMember({ id, onAdded }: { id: string; onAdded: () => void }) {
  const t = useT();
  const [userID, setUserID] = useState("");
  const [role, setRole] = useState("READER");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!userID.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.addOrganizationMember(id, userID.trim(), role);
      setUserID("");
      onAdded();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("orgs.addMember")}>
      <p className="sec lead-out">{t("orgs.addMemberLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("orgs.userId")}
          value={userID}
          onChange={setUserID}
          placeholder={t("orgs.userId")}
          grow
        />
        <SelectField
          label={t("members.column.role")}
          value={role}
          onChange={setRole}
          options={["OWNER", "ADMIN", "CONTRIBUTOR", "READER"].map((r) => ({
            value: r,
            label: roleLabel(t, r),
          }))}
        />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !userID.trim()}>
          {busy ? t("common.loading") : t("orgs.addMember")}
        </Btn>
      </Row>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      <div className="mt">
        <Note tone="info">{t("members.noRemoval")}</Note>
      </div>
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
