import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
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
        {tab("/administration/identities", t("admin.tab.identities"))}
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
                        primary={<Mono truncate>{a.user_id}</Mono>}
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
 * Creating identities, and looking up what a principal holds.
 *
 * There is no endpoint that lists users or service principals, so this screen
 * creates them and shows what it just created — it cannot show a directory,
 * and does not pretend to have one.
 */
export function AdminIdentities() {
  const t = useT();
  useCrumbs("admin:identities", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.identities"),
  }));

  return (
    <>
      <PageHead title={t("admin.tab.identities")} lead={t("admin.identitiesLead")} />
      <Grid cols={2}>
        <Stack>
          <CreateUser />
          <CreateServicePrincipal />
        </Stack>
        <Stack>
          <CredentialLookup />
        </Stack>
      </Grid>
    </>
  );
}

function CreateUser() {
  const t = useT();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [created, setCreated] = useState<string>();

  async function submit() {
    if (!email.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const user = await api.createUser(email.trim(), name.trim());
      setCreated(user.id);
      setEmail("");
      setName("");
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
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !email.trim()}>
          {busy ? t("common.loading") : t("admin.createUser")}
        </Btn>
      </Row>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      {created && (
        <div className="mt">
          <Note tone="info">
            {t("admin.created", { name: "" })} <Mono>{created}</Mono>
          </Note>
        </div>
      )}
    </Panel>
  );
}

function CreateServicePrincipal() {
  const t = useT();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [created, setCreated] = useState<string>();

  async function submit() {
    if (!name.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const sp = await api.createServicePrincipal(name.trim(), description.trim());
      setCreated(sp.id);
      setName("");
      setDescription("");
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
      {created && (
        <div className="mt">
          <Note tone="info">
            {t("admin.created", { name: "" })} <Mono>{created}</Mono>
          </Note>
        </div>
      )}
    </Panel>
  );
}

function CredentialLookup() {
  const t = useT();
  const f = useFormat();
  const [input, setInput] = useState("");
  const [principal, setPrincipal] = useState("");

  const creds = useResource(
    () => (principal ? api.credentialsFor(principal) : Promise.resolve([])),
    [principal],
  );

  return (
    <>
      <Panel title={t("admin.credentials")}>
        <p className="sec lead-out">{t("admin.credentialsLead")}</p>
        <Row gap="sm">
          <TextField
            label={t("members.column.principal")}
            value={input}
            onChange={setInput}
            placeholder={t("members.principalPlaceholder")}
            grow
          />
          <Btn onClick={() => setPrincipal(input.trim())} disabled={!input.trim()}>
            {t("admin.lookup")}
          </Btn>
        </Row>
      </Panel>

      {principal && (
        <Panel title={t("tokens.title")} tight>
          {creds.loading ? (
            <Loading />
          ) : creds.error ? (
            <ErrorState error={creds.error} onRetry={creds.reload} />
          ) : (creds.data ?? []).length === 0 ? (
            <EmptyState title={t("tokens.empty")} />
          ) : (
            <DataTable
              columns={[
                { key: "type", label: t("tokens.column.type") },
                { key: "used", label: t("tokens.column.lastUsed"), align: "r" },
              ]}
            >
              {(creds.data ?? []).map((c) => (
                <tr key={c.id}>
                  <td>
                    <Cell
                      primary={<Mono>{c.client_type}</Mono>}
                      secondary={
                        <Mono dim truncate>
                          {c.id}
                        </Mono>
                      }
                    />
                  </td>
                  <td className="r">
                    <span className="dim">
                      {c.last_used_at ? f.relative(c.last_used_at) : t("common.never")}
                    </span>
                  </td>
                </tr>
              ))}
            </DataTable>
          )}
        </Panel>
      )}
    </>
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
