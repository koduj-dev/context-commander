import { useState } from "react";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { api, type SignInOrganization, type SignInRule } from "../lib/api";
import { useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Note,
  PageHead,
  Panel,
  Picks,
  Row,
  Stack,
  TextField,
} from "../ui";

/**
 * Who may sign in through single sign-on, and where a new account lands
 * (docs/adr/0024).
 *
 * A rule is a domain or one address. Whatever the server's
 * `--oidc-allowed-domains` lists is admitted too; it lives in a flag, so it is
 * not shown as editable here. The first rule closes the door to everything it
 * does not name, which the lead says before anyone adds one.
 */
export function AdminSignIn() {
  const t = useT();
  useCrumbs("admin:signin", () => ({
    path: [t("admin.title")],
    current: t("admin.tab.signin"),
  }));
  const rules = useResource(() => api.signInRules(), []);
  const orgs = useResource(() => api.signInOrganizations(), []);
  const organizations = orgs.data?.items ?? [];

  return (
    <>
      <PageHead title={t("admin.signin.title")} lead={t("admin.signin.lead")} />
      <Stack>
        <Panel title={t("admin.signin.add")}>
          <AddRule organizations={organizations} onAdded={rules.reload} />
        </Panel>
        <Panel
          title={t("admin.signin.rules")}
          tight
          footer={<span className="dim">{t("admin.signin.help")}</span>}
        >
          {rules.loading ? (
            <Loading />
          ) : rules.error ? (
            <ErrorState error={rules.error} onRetry={rules.reload} />
          ) : (rules.data?.items ?? []).length === 0 ? (
            <EmptyState title={t("admin.signin.none")} body={t("admin.signin.noneBody")} />
          ) : (
            <DataTable
              columns={[
                { key: "who", label: t("admin.signin.who") },
                { key: "orgs", label: t("admin.signin.organizations") },
                { key: "actions", label: t("admin.signin.actions"), align: "r" },
              ]}
            >
              {(rules.data?.items ?? []).map((r) => (
                <RuleRow
                  key={r.id}
                  rule={r}
                  organizations={organizations}
                  onChanged={rules.reload}
                />
              ))}
            </DataTable>
          )}
        </Panel>
      </Stack>
    </>
  );
}

function OrganizationPicks({
  organizations,
  value,
  onChange,
}: {
  organizations: SignInOrganization[];
  value: string[];
  onChange: (v: string[]) => void;
}) {
  const t = useT();
  if (organizations.length === 0) {
    return <span className="dim">{t("admin.signin.noOrganizations")}</span>;
  }
  return (
    <Picks
      label={t("admin.signin.organizations")}
      value={value}
      onChange={onChange}
      options={organizations.map((o) => ({ value: o.id, label: o.name }))}
    />
  );
}

function AddRule({
  organizations,
  onAdded,
}: {
  organizations: SignInOrganization[];
  onAdded: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function add() {
    if (!value.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createSignInRule(value.trim(), picked);
      setValue("");
      setPicked([]);
      onAdded();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="stack">
      <TextField
        label={t("admin.signin.value")}
        placeholder={t("admin.signin.valuePlaceholder")}
        value={value}
        onChange={setValue}
        labelled
      />
      <div className="stack">
        <span className="dim">{t("admin.signin.grant")}</span>
        <OrganizationPicks organizations={organizations} value={picked} onChange={setPicked} />
      </div>
      <Row gap="sm">
        <Btn kind="primary" small onClick={() => void add()} disabled={busy || !value.trim()}>
          {busy ? t("common.loading") : t("admin.signin.addButton")}
        </Btn>
      </Row>
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}

function RuleRow({
  rule,
  organizations,
  onChanged,
}: {
  rule: SignInRule;
  organizations: SignInOrganization[];
  onChanged: () => void;
}) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [picked, setPicked] = useState<string[]>(rule.organizations.map((o) => o.id));
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setFailure(undefined);
    try {
      await action();
      setEditing(false);
      onChanged();
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
            primary={<Mono>{rule.value}</Mono>}
            secondary={
              <Badge>
                {rule.kind === "domain"
                  ? t("admin.signin.kindDomain")
                  : t("admin.signin.kindAddress")}
              </Badge>
            }
          />
        </td>
        <td>
          {rule.organizations.length === 0 ? (
            <span className="dim">{t("admin.signin.grantsNothing")}</span>
          ) : (
            rule.organizations.map((o) => o.name).join(", ")
          )}
        </td>
        <td className="r nowrap">
          <Row gap="sm">
            <Btn small onClick={() => setEditing((v) => !v)} disabled={busy}>
              {t("admin.signin.change")}
            </Btn>
            <Btn
              small
              kind="danger"
              disabled={busy}
              onClick={() => {
                if (window.confirm(t("admin.signin.confirmDelete", { value: rule.value }))) {
                  void run(() => api.deleteSignInRule(rule.id));
                }
              }}
            >
              {t("admin.signin.delete")}
            </Btn>
          </Row>
        </td>
      </tr>
      {(editing || failure !== undefined) && (
        <tr>
          <td colSpan={3}>
            <div className="stack">
              {editing && (
                <>
                  <OrganizationPicks
                    organizations={organizations}
                    value={picked}
                    onChange={setPicked}
                  />
                  <Note>{t("admin.signin.changeNote")}</Note>
                  <Row gap="sm">
                    <Btn
                      kind="primary"
                      small
                      disabled={busy}
                      onClick={() =>
                        void run(() => api.setSignInRuleOrganizations(rule.id, picked))
                      }
                    >
                      {busy ? t("common.loading") : t("admin.signin.save")}
                    </Btn>
                    <Btn small onClick={() => setEditing(false)}>
                      {t("common.cancel")}
                    </Btn>
                  </Row>
                </>
              )}
              {failure !== undefined && <ErrorState error={failure} />}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
