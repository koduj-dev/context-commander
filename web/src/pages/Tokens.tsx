import { useState } from "react";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { useFormat } from "../app/prefs";
import { api, type IssuedToken } from "../lib/api";
import { useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  Copyable,
  Code,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Note,
  PageHead,
  Panel,
  Row,
  SelectField,
  TextField,
} from "../ui";

const EXPIRY = {
  never: undefined,
  "30d": 30 * 86400,
  "90d": 90 * 86400,
  "1y": 365 * 86400,
} as const;

type ExpiryKey = keyof typeof EXPIRY;

/**
 * Your own credentials.
 *
 * Self-only, because the endpoint is: issuing a token for someone else is a
 * deliberately separate, unbuilt feature (adapters/http/tokens.go), and a
 * screen that offered it would be promising an escalation the server refuses.
 */
export function Tokens() {
  const t = useT();
  useCrumbs("tokens", () => ({ path: [], current: t("tokens.title") }));
  const f = useFormat();
  const { data, error, loading, reload } = useResource(() => api.tokens(), []);

  const [clientType, setClientType] = useState("web ui");
  const [expiry, setExpiry] = useState<ExpiryKey>("90d");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [issued, setIssued] = useState<IssuedToken>();

  const tokens = data ?? [];

  async function issue() {
    if (!clientType.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      setIssued(await api.issueToken(clientType.trim(), EXPIRY[expiry]));
      reload();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  async function revoke(id: string) {
    if (!window.confirm(t("tokens.confirmRevoke"))) return;
    setFailure(undefined);
    try {
      await api.revokeToken(id);
      reload();
    } catch (err) {
      setFailure(err);
    }
  }

  return (
    <>
      <PageHead title={t("tokens.title")} lead={t("tokens.lead")} />

      {issued && (
        <div className="mb">
          <Panel title={t("tokens.justIssued")}>
            {/* Shown once and never again — only the hash is stored. */}
            <Copyable value={issued.token}>
              <Code>{issued.token}</Code>
            </Copyable>
            <div className="mt">
              <Note tone="warn">{t("tokens.justIssuedBody")}</Note>
            </div>
            <div className="mt">
              <Btn onClick={() => setIssued(undefined)}>{t("common.close")}</Btn>
            </div>
          </Panel>
        </div>
      )}

      <Panel title={t("tokens.title")} tight>
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : tokens.length === 0 ? (
          <EmptyState title={t("tokens.empty")} body={t("tokens.emptyBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "type", label: t("tokens.column.type") },
              { key: "state" },
              { key: "created", label: t("tokens.column.created"), align: "r" },
              { key: "used", label: t("tokens.column.lastUsed"), align: "r" },
              { key: "expires", label: t("tokens.column.expires"), align: "r" },
              { key: "actions", align: "r" },
            ]}
          >
            {tokens.map((c) => {
              const revoked = Boolean(c.revoked_at);
              const expired = Boolean(c.expires_at && new Date(c.expires_at) < new Date());
              return (
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
                  <td>
                    <Badge tone={revoked ? "danger" : expired ? "warn" : "ok"}>
                      {revoked
                        ? t("tokens.revoked")
                        : expired
                          ? t("tokens.expired")
                          : t("tokens.active")}
                    </Badge>
                  </td>
                  <td className="r">
                    <span className="dim">{f.date(c.created_at)}</span>
                  </td>
                  <td className="r">
                    <span className="dim">
                      {c.last_used_at ? f.relative(c.last_used_at) : t("common.never")}
                    </span>
                  </td>
                  <td className="r">
                    <span className="dim">
                      {c.expires_at ? f.date(c.expires_at) : t("common.never")}
                    </span>
                  </td>
                  <td className="r">
                    {!revoked && (
                      <Btn kind="danger" small onClick={() => void revoke(c.id)}>
                        {t("tokens.revoke")}
                      </Btn>
                    )}
                  </td>
                </tr>
              );
            })}
          </DataTable>
        )}
      </Panel>

      <div className="mt">
        <Panel title={t("tokens.issue")}>
          <Row gap="sm">
            <TextField
              label={t("tokens.clientType")}
              value={clientType}
              onChange={setClientType}
              placeholder={t("tokens.clientType")}
              grow
            />
            <SelectField
              label={t("tokens.expiry")}
              value={expiry}
              onChange={setExpiry}
              options={[
                { value: "30d", label: t("tokens.expiry.30d") },
                { value: "90d", label: t("tokens.expiry.90d") },
                { value: "1y", label: t("tokens.expiry.1y") },
                { value: "never", label: t("tokens.expiry.never") },
              ]}
            />
            <Btn kind="primary" onClick={() => void issue()} disabled={busy || !clientType.trim()}>
              {busy ? t("common.loading") : t("tokens.issue")}
            </Btn>
          </Row>
          {failure !== undefined && (
            <div className="mt">
              <ErrorState error={failure} />
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}
