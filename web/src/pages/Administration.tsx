import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api, type InstallationUser, type IssuedToken, type ServicePrincipal } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  Code,
  Copyable,
  DataTable,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  Note,
  PageHead,
  Pager,
  Panel,
  Row,
  Stack,
  Tabs,
  TextField,
} from "../ui";

/**
 * The installation, for whoever administers it.
 *
 * Split into tabs rather than one long page: "is anything wrong" and "show me
 * every row" are different questions, and the overview should answer the
 * first one without making someone scroll past the second.
 */
export function Administration() {
  const t = useT();
  const tab = (to: string, label: string, end = false) => (
    <NavLink to={to} end={end} className={({ isActive }) => (isActive ? "active" : "")}>
      {label}
    </NavLink>
  );

  return (
    <>
      <Tabs label={t("admin.title")}>
        {tab("/administration", t("admin.tab.overview"), true)}
        {tab("/administration/people", t("admin.tab.people"))}
        {tab("/administration/agents", t("admin.tab.agents"))}
        {tab("/administration/usage", t("admin.tab.usage"))}
        {tab("/administration/security", t("admin.tab.security"))}
      </Tabs>
      <Outlet />
    </>
  );
}

export function AdminOverview() {
  const t = useT();
  useCrumbs("admin:overview", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.overview"),
  }));
  const f = useFormat();
  const admins = useResource(() => api.installationAdmins(), []);
  const recent = useList((cursor) => api.securityEvents(cursor, 5), []);

  return (
    <>
      <PageHead title={t("admin.title")} lead={t("admin.lead")} />

      <Grid cols="2-1">
        <Stack>
          <Panel
            title={t("admin.admins")}
            tight
            footer={<span className="dim">{t("admin.adminsLead")}</span>}
          >
            {admins.loading ? (
              <Loading />
            ) : admins.error ? (
              <ErrorState error={admins.error} onRetry={admins.reload} />
            ) : (
              <DataTable
                columns={[
                  { key: "user", label: t("admin.column.user") },
                  { key: "role", label: t("admin.column.role") },
                  { key: "granted", label: t("admin.column.granted"), align: "r" },
                ]}
              >
                {(admins.data ?? []).map((a) => (
                  <tr key={a.user_id}>
                    <td>
                      <Cell
                        primary={<Copyable value={a.user_id} />}
                        secondary={
                          a.granted_by && (
                            <Mono dim truncate>
                              {a.granted_by}
                            </Mono>
                          )
                        }
                      />
                    </td>
                    <td>
                      <Badge tone="active">{roleLabel(t, a.role)}</Badge>
                    </td>
                    <td className="r">
                      <span className="dim">{f.date(a.granted_at)}</span>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>
        </Stack>

        <Stack top>
          <Panel title={t("admin.security")} tight>
            {recent.loading ? (
              <Loading />
            ) : recent.error ? (
              <ErrorState error={recent.error} onRetry={recent.reload} />
            ) : recent.items.length === 0 ? (
              <EmptyState title={t("admin.securityEmpty")} body={t("admin.securityEmptyBody")} />
            ) : (
              <DataTable columns={[{ key: "e" }]}>
                {recent.items.map((e) => (
                  <tr key={e.id}>
                    <td>
                      <Cell
                        primary={
                          <>
                            <Badge tone="danger">{e.kind}</Badge>{" "}
                            <span className="dim">{e.reason}</span>
                          </>
                        }
                        secondary={<span className="dim">{f.relative(e.ts)}</span>}
                      />
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

/**
 * Who and what exists on this installation.
 *
 * Two lists and the forms that add to them. The lists are the point: before
 * them, an interface that needed to name a person could only ask for a UUID,
 * and this screen could show what it had just created and nothing else.
 */
/**
 * The people on this installation.
 *
 * Split from the agents it used to share a screen with. Two paginated tables
 * side by side in a two-column grid each got half the width and competed for
 * the same vertical space — tolerable with three rows each, and the
 * maintainer pointed out that an installation can hold hundreds. A directory
 * is a thing you scan, so it gets the whole width.
 */
export function AdminPeople() {
  const t = useT();
  const users = useList((cursor) => api.users(cursor), []);

  return (
    <>
      <PageHead title={t("admin.users")} lead={t("admin.usersLead")} />

      <Stack>
        <Panel
          title={t("admin.users")}
          tight
          footer={
            <Pager
              shown={users.items.length}
              hasMore={users.hasMore}
              onLoadMore={users.loadMore}
              busy={users.loadingMore}
            />
          }
        >
          {users.loading ? (
            <Loading />
          ) : users.error ? (
            <ErrorState error={users.error} onRetry={users.reload} />
          ) : users.items.length === 0 ? (
            <EmptyState title={t("admin.noUsers")} />
          ) : (
            <DataTable
              columns={[
                { key: "who", label: t("admin.column.user") },
                { key: "created", label: t("admin.column.created"), align: "r" },
              ]}
            >
              {users.items.map((u) => (
                <UserRow key={u.id} user={u} />
              ))}
            </DataTable>
          )}
        </Panel>

        <CreateUser onCreated={users.reload} />
      </Stack>
    </>
  );
}

/**
 * The agents on this installation, and who registered each one.
 *
 * `registered by` is provenance and nothing else: an agent is its own
 * principal and reaches a project only through its own membership there
 * (docs/adr/0004). The column is deliberately not called "owner" — a word
 * that would invite a reader to assume the agent inherits that person's
 * access, which it does not.
 */
export function AdminAgents() {
  const t = useT();
  const agents = useList((cursor) => api.servicePrincipals(cursor), []);

  return (
    <>
      <PageHead title={t("admin.servicePrincipals")} lead={t("admin.servicePrincipalsLead")} />

      <Stack>
        <Panel
          title={t("admin.servicePrincipals")}
          tight
          footer={
            <>
              <span className="dim">{t("admin.registeredByHelp")}</span>
              <Pager
                shown={agents.items.length}
                hasMore={agents.hasMore}
                onLoadMore={agents.loadMore}
                busy={agents.loadingMore}
              />
            </>
          }
        >
          {agents.loading ? (
            <Loading />
          ) : agents.error ? (
            <ErrorState error={agents.error} onRetry={agents.reload} />
          ) : agents.items.length === 0 ? (
            <EmptyState
              title={t("admin.noServicePrincipals")}
              body={t("admin.noServicePrincipalsBody")}
            />
          ) : (
            <DataTable
              columns={[
                { key: "name", label: t("admin.column.name") },
                { key: "by", label: t("admin.column.registeredBy") },
                { key: "created", label: t("admin.column.created"), align: "r" },
              ]}
            >
              {agents.items.map((a) => (
                <AgentRow key={a.id} agent={a} />
              ))}
            </DataTable>
          )}
        </Panel>

        <CreateServicePrincipal onCreated={agents.reload} />
      </Stack>
    </>
  );
}

function UserRow({ user }: { user: InstallationUser }) {
  const t = useT();
  const f = useFormat();
  const [open, setOpen] = useState(false);

  return (
    <>
      <tr>
        <td>
          <Cell
            primary={user.display_name || user.email}
            secondary={
              user.display_name ? (
                <span className="dim">{user.email}</span>
              ) : (
                <Copyable value={user.id} />
              )
            }
          />
        </td>
        <td className="r nowrap">
          <span className="dim">{f.date(user.created_at)}</span>{" "}
          {user.principal_id && (
            <Btn small onClick={() => setOpen((v) => !v)}>
              {t("admin.tokens")}
            </Btn>
          )}
        </td>
      </tr>
      {open && user.principal_id && (
        <tr>
          <td colSpan={2}>
            <Credentials principalID={user.principal_id} />
          </td>
        </tr>
      )}
    </>
  );
}

/**
 * The tokens one identity holds.
 *
 * Revoking lives here and not only on a person's own account screen because
 * the credential that matters in an incident is usually not yours — it is
 * the agent's, and whoever is dealing with it is an admin, not the agent.
 */
function Credentials({ principalID }: { principalID: string }) {
  const t = useT();
  const f = useFormat();
  const { data, error, loading, reload } = useResource(
    () => api.credentialsFor(principalID),
    [principalID],
  );
  const [failure, setFailure] = useState<unknown>();

  async function revoke(id: string) {
    if (!window.confirm(t("admin.confirmRevokeFor"))) return;
    setFailure(undefined);
    try {
      await api.revokeCredentialFor(id);
      reload();
    } catch (err) {
      setFailure(err);
    }
  }

  const credentials = data ?? [];

  if (loading) return <Loading />;
  if (error) return <ErrorState error={error} onRetry={reload} />;
  if (credentials.length === 0) return <EmptyState title={t("admin.noTokens")} />;

  return (
    <>
      <DataTable
        columns={[
          { key: "type", label: t("tokens.column.type") },
          { key: "used", label: t("tokens.column.lastUsed"), align: "r" },
          { key: "act", align: "r" },
        ]}
      >
        {credentials.map((c) => (
          <tr key={c.id}>
            <td>
              <Cell
                primary={<Mono>{c.client_type}</Mono>}
                secondary={
                  c.revoked_at ? (
                    <Badge tone="danger">{t("tokens.revoked")}</Badge>
                  ) : (
                    <Copyable value={c.id} />
                  )
                }
              />
            </td>
            <td className="r">
              <span className="dim">
                {c.last_used_at ? f.relative(c.last_used_at) : t("common.never")}
              </span>
            </td>
            <td className="r">
              {!c.revoked_at && (
                <Btn kind="danger" small onClick={() => void revoke(c.id)}>
                  {t("tokens.revoke")}
                </Btn>
              )}
            </td>
          </tr>
        ))}
      </DataTable>
      <div className="mt-sm">
        <span className="dim">{t("admin.tokensLead")}</span>
      </div>
      {failure !== undefined && <ErrorState error={failure} />}
    </>
  );
}

/**
 * An agent, and the one thing anyone wants to do with it.
 *
 * Registering a service principal creates an identity and nothing else —
 * which is why "what is this for" is a fair question. The answer is the
 * token: issue one here and whatever holds it acts as this agent.
 */
function AgentRow({ agent }: { agent: ServicePrincipal }) {
  const t = useT();
  const f = useFormat();
  const [issued, setIssued] = useState<IssuedToken>();
  const [showTokens, setShowTokens] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function issue() {
    // The token is issued for the principal the agent acts as, not for the
    // service_principals row — they are different ids and the endpoint takes
    // the first.
    if (busy || !agent.principal_id) return;
    setBusy(true);
    setFailure(undefined);
    try {
      setIssued(await api.issueTokenFor(agent.principal_id, agent.name, 365 * 86400));
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <tr>
        <td>
          <Cell
            primary={<Mono>{agent.name}</Mono>}
            secondary={
              agent.description ? (
                <span className="dim">{agent.description}</span>
              ) : (
                <Copyable value={agent.id} />
              )
            }
          />
        </td>
        <td>
          {/* The name, with the address underneath: two people called Filip
              is the normal case, and the email is what tells them apart.
              "not recorded" is said in words rather than left blank — an
              empty cell reads as a rendering fault, and the absence here is
              a fact (migrations/000026 backfills nobody, because naming a
              person who did not do it would be worse than naming none). */}
          {agent.created_by_name || agent.created_by_email ? (
            <Cell
              primary={agent.created_by_name || agent.created_by_email}
              secondary={
                agent.created_by_name && agent.created_by_email ? (
                  <span className="dim">{agent.created_by_email}</span>
                ) : undefined
              }
            />
          ) : (
            <span className="dim">{t("admin.unattributed")}</span>
          )}
        </td>
        <td className="r nowrap">
          <span className="dim">{f.date(agent.created_at)}</span>{" "}
          {agent.principal_id && (
            <Btn small onClick={() => setShowTokens((v) => !v)}>
              {t("admin.tokens")}
            </Btn>
          )}{" "}
          <Btn small onClick={() => void issue()} disabled={busy}>
            {busy ? t("common.loading") : t("admin.issueFor")}
          </Btn>
        </td>
      </tr>
      {showTokens && agent.principal_id && (
        <tr>
          <td colSpan={3}>
            <Credentials principalID={agent.principal_id} />
          </td>
        </tr>
      )}
      {(issued || failure !== undefined) && (
        <tr>
          <td colSpan={3}>
            {failure !== undefined ? (
              <ErrorState error={failure} />
            ) : (
              <>
                <Note tone="warn">{t("admin.issueForLead")}</Note>
                <div className="mt">
                  <Copyable value={issued!.token}>
                    <Code>{issued!.token}</Code>
                  </Copyable>
                </div>
                <div className="mt">
                  <Btn small onClick={() => setIssued(undefined)}>
                    {t("common.close")}
                  </Btn>
                </div>
              </>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

function CreateUser({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!email.trim() || !name.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createUser(email.trim(), name.trim());
      setEmail("");
      setName("");
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("admin.createUser")}>
      <p className="sec lead-out">{t("admin.createUserLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("admin.email")}
          value={email}
          onChange={setEmail}
          placeholder={t("admin.email")}
          grow
        />
        <TextField
          label={t("admin.displayName")}
          value={name}
          onChange={setName}
          placeholder={t("admin.displayName")}
          grow
        />
        <Btn
          kind="primary"
          onClick={() => void submit()}
          disabled={busy || !email.trim() || !name.trim()}
        >
          {busy ? t("common.loading") : t("admin.createUser")}
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

function CreateServicePrincipal({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!name.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createServicePrincipal(name.trim(), description.trim());
      setName("");
      setDescription("");
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("admin.createService")}>
      <p className="sec lead-out">{t("admin.createServiceLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("admin.name")}
          value={name}
          onChange={setName}
          placeholder={t("admin.name")}
          grow
        />
        <TextField
          label={t("admin.description")}
          value={description}
          onChange={setDescription}
          placeholder={t("admin.description")}
          grow
        />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !name.trim()}>
          {busy ? t("common.loading") : t("admin.createService")}
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

export function AdminSecurityLog() {
  const t = useT();
  useCrumbs("admin:security", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.security"),
  }));
  const f = useFormat();
  const list = useList((cursor) => api.securityEvents(cursor), []);

  return (
    <>
      <PageHead title={t("admin.security")} lead={t("admin.securityLead")} />

      <Panel
        title={t("admin.security")}
        tight
        footer={
          <>
            <span className="dim">{t("admin.securityNote")}</span>
            <Pager
              shown={list.items.length}
              hasMore={list.hasMore}
              onLoadMore={list.loadMore}
              busy={list.loadingMore}
            />
          </>
        }
      >
        {list.loading ? (
          <Loading />
        ) : list.error ? (
          <ErrorState error={list.error} onRetry={list.reload} />
        ) : list.items.length === 0 ? (
          <EmptyState title={t("admin.securityEmpty")} body={t("admin.securityEmptyBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "time", label: t("activity.column.time") },
              { key: "kind", label: t("admin.column.kind") },
              { key: "principal", label: t("activity.column.principal") },
              { key: "capability", label: t("admin.column.capability") },
              { key: "reason", label: t("admin.column.reason") },
            ]}
          >
            {list.items.map((e) => (
              <tr key={e.id}>
                <td className="nowrap">
                  <span className="dim">{f.stamp(e.ts)}</span>
                  {f.showUTC && (
                    <>
                      <br />
                      <span className="dim">{f.utc(e.ts)}</span>
                    </>
                  )}
                </td>
                <td>
                  <Badge tone="danger">{e.kind}</Badge>
                </td>
                <td>
                  <Cell
                    primary={<Mono truncate>{e.principal_id ?? t("common.unknown")}</Mono>}
                    secondary={
                      e.credential_id && (
                        <Mono dim truncate>
                          {e.credential_id}
                        </Mono>
                      )
                    }
                  />
                </td>
                <td>{e.capability ? <Mono dim>{e.capability}</Mono> : null}</td>
                <td>
                  <span className="dim">{e.reason}</span>
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>
    </>
  );
}
