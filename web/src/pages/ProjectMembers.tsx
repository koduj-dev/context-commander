import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  Copyable,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  PageHead,
  Panel,
  Row,
  SelectField,
  TextField,
} from "../ui";
import { PrincipalPicker } from "../ui/PrincipalPicker";
import { useProject } from "./ProjectLayout";

const ROLES = ["OWNER", "ADMIN", "CONTRIBUTOR", "READER"] as const;

/**
 * Who may act in this project.
 *
 * The list is not paginated, and that is the handler's decision rather than
 * an omission: membership is bounded by how many people were added, not by
 * how much data the system holds (adapters/http/projects.go returns a bare
 * array).
 *
 * The caller's own role is read off this list rather than fetched: it is
 * already here, and asking a second endpoint for something on screen is a
 * request that can fail for no benefit.
 *
 * ADMIN and OWNER change roles and remove members; anything touching an
 * OWNER is the OWNER's alone, and every member may leave. The server says no
 * to the last OWNER stepping down or leaving, and that refusal is shown as
 * it comes back rather than guessed here.
 */
export function ProjectMembers() {
  const t = useT();
  const f = useFormat();
  const { me } = useAuth();
  const { namespace, slug } = useProject();

  const { data, error, loading, reload } = useResource(
    () => api.members(namespace, slug),
    [namespace, slug],
  );

  const navigate = useNavigate();
  const members = useMemo(() => data ?? [], [data]);
  const myRole = members.find((m) => m.principal_id === me?.principal_id)?.role;
  const canAdd = myRole === "OWNER" || myRole === "ADMIN";
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  /** Whether the reader may change this member's role or remove them. */
  const canManage = (role: string) => canAdd && (role !== "OWNER" || myRole === "OWNER");
  const roles = myRole === "OWNER" ? ROLES : ROLES.filter((r) => r !== "OWNER");

  async function act(action: () => Promise<unknown>, after: () => void = reload) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      after();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageHead title={t("members.title")} lead={t("members.lead")} />

      <Panel title={t("members.title")} sub={t("members.count", { count: members.length })} tight>
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : members.length === 0 ? (
          <EmptyState title={t("members.title")} body={t("members.lead")} />
        ) : (
          <>
            {failure !== undefined && <ErrorState error={failure} />}
            <DataTable
              columns={[
                { key: "principal", label: t("members.column.principal") },
                { key: "role", label: t("members.column.role") },
                { key: "added", label: t("members.column.added"), align: "r" },
                { key: "actions", label: t("members.actions"), align: "r" },
              ]}
            >
              {members.map((m) => {
                const name = m.display_name ?? m.email ?? m.principal_id;
                const mine = m.principal_id === me?.principal_id;
                return (
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
                        secondary={<Copyable value={m.principal_id} />}
                      />
                    </td>
                    <td>
                      {canManage(m.role) ? (
                        <SelectField
                          label={t("members.roleOf", { name })}
                          value={m.role as (typeof ROLES)[number]}
                          onChange={(role) =>
                            void act(() => api.setMemberRole(namespace, slug, m.principal_id, role))
                          }
                          options={(m.role === "OWNER" ? ROLES : roles).map((r) => ({
                            value: r,
                            label: roleLabel(t, r),
                          }))}
                        />
                      ) : (
                        <span
                          title={canAdd && m.role === "OWNER" ? t("members.ownerOnly") : undefined}
                        >
                          <Badge tone={m.role === "OWNER" ? "active" : "neutral"}>
                            {roleLabel(t, m.role)}
                          </Badge>
                        </span>
                      )}
                    </td>
                    <td className="r">
                      <span className="dim">{f.date(m.created_at)}</span>
                    </td>
                    <td className="r">
                      {mine ? (
                        <Btn
                          small
                          kind="danger"
                          disabled={busy}
                          onClick={() => {
                            if (!window.confirm(t("members.confirmLeave"))) return;
                            void act(
                              () => api.removeMember(namespace, slug, m.principal_id),
                              () => navigate("/"),
                            );
                          }}
                        >
                          {t("members.leave")}
                        </Btn>
                      ) : canManage(m.role) ? (
                        <Btn
                          small
                          kind="danger"
                          disabled={busy}
                          onClick={() => {
                            if (!window.confirm(t("members.confirmRemove", { name }))) return;
                            void act(() => api.removeMember(namespace, slug, m.principal_id));
                          }}
                        >
                          {t("members.remove")}
                        </Btn>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </DataTable>
          </>
        )}
      </Panel>

      {canAdd && (
        <div className="mt">
          <Invitations canGrantOwner={myRole === "OWNER"} />
        </div>
      )}
      {canAdd && (
        <div className="mt">
          <AddMember onAdded={reload} />
        </div>
      )}
    </>
  );
}

/**
 * Adding a principal.
 *
 * By UUID, because that is what the endpoint takes and there is no way to
 * offer anything better: nothing in the API lists the principals of an
 * installation, so a picker would have nothing to pick from. The field says
 * so rather than pretending a name would work.
 */
function AddMember({ onAdded }: { onAdded: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();

  const [principal, setPrincipal] = useState("");
  const [role, setRole] = useState<(typeof ROLES)[number]>("READER");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!principal.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.addMember(namespace, slug, principal.trim(), role);
      setPrincipal("");
      onAdded();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("members.add")}>
      <p className="sec lead-out">{t("members.addHelp")}</p>
      <Row gap="sm">
        <PrincipalPicker
          kind="principal"
          includeAgents
          label={t("members.column.principal")}
          value={principal}
          onChange={setPrincipal}
        />
        <SelectField
          label={t("members.column.role")}
          value={role}
          onChange={setRole}
          options={ROLES.map((r) => ({ value: r, label: roleLabel(t, r) }))}
        />
        <Btn kind="primary" onClick={submit} disabled={busy || !principal.trim()}>
          {busy ? t("common.loading") : t("members.add")}
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
 * Inviting someone by email (docs/adr/0030). The invitation is accepted the
 * next time a person with that address signs in through the browser — on
 * their first sign-in if they have no account yet — so nobody has to know a
 * principal id, or wait for the person to exist. The screen never says
 * whether the address already has an account: the server does not tell it.
 */
function Invitations({ canGrantOwner }: { canGrantOwner: boolean }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const list = useResource(() => api.invitations(namespace, slug), [namespace, slug]);

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<(typeof ROLES)[number]>("CONTRIBUTOR");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const roles = canGrantOwner ? ROLES : ROLES.filter((r) => r !== "OWNER");

  async function run(action: () => Promise<unknown>, after?: () => void) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      after?.();
      list.reload();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  const items = list.data?.items ?? [];
  return (
    <Panel title={t("members.invite")} tight>
      <div className="stack">
        <p className="sec lead-out">{t("members.inviteHelp")}</p>
        <Row gap="sm">
          <TextField
            label={t("members.inviteEmail")}
            placeholder="someone@example.com"
            value={email}
            onChange={setEmail}
            labelled
          />
          <SelectField
            label={t("members.column.role")}
            value={role}
            onChange={setRole}
            options={roles.map((r) => ({ value: r, label: roleLabel(t, r) }))}
          />
          <Btn
            kind="primary"
            disabled={busy || !email.trim()}
            onClick={() =>
              void run(
                () => api.invite(namespace, slug, email.trim(), role),
                () => setEmail(""),
              )
            }
          >
            {busy ? t("common.loading") : t("members.inviteButton")}
          </Btn>
        </Row>
        {failure !== undefined && <ErrorState error={failure} />}
      </div>
      {list.loading ? (
        <Loading />
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={list.reload} />
      ) : items.length === 0 ? (
        <EmptyState title={t("members.noInvitations")} />
      ) : (
        <DataTable
          columns={[
            { key: "email", label: t("members.inviteEmail") },
            { key: "role", label: t("members.column.role") },
            { key: "expires", label: t("members.inviteExpires") },
            { key: "actions", label: t("members.inviteActions"), align: "r" },
          ]}
        >
          {items.map((i) => (
            <tr key={i.id}>
              <td>
                <Mono>{i.email}</Mono>
              </td>
              <td>
                <Badge>{roleLabel(t, i.role)}</Badge>
              </td>
              <td>
                <span className="dim">{f.date(i.expires_at)}</span>
              </td>
              <td className="r">
                <Btn
                  small
                  kind="danger"
                  disabled={busy}
                  onClick={() => void run(() => api.revokeInvitation(namespace, slug, i.id))}
                >
                  {t("members.inviteRevoke")}
                </Btn>
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}
