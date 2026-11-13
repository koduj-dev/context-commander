import { useState } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api, type AgentMessage, type Person, type Presence } from "../lib/api";
import { useList } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  ErrorState,
  Mono,
  Note,
  Pager,
  Panel,
  Row,
  Stack,
  TextArea,
} from "../ui";

/**
 * Messages to agents mid-work (docs/adr/0032).
 *
 * A person sends from wherever presence is shown; the agent receives it at
 * its next call to the server, or sooner through the Claude Code hook. Only
 * a browser session can send, which is every session this screen runs in.
 */

/** Who a message being written is for: one session, or everyone. */
export type MessageTarget =
  | { kind: "session"; project: string; presence: Presence; person?: Person }
  | { kind: "project"; project: string };

/** Whether a presence row can be messaged: an agent session, not a browser. */
export function canMessage(p: Presence): boolean {
  return p.client_type !== "session" && !p.you;
}

/** The project path "namespace/slug", split. */
function splitProject(path: string): [string, string] {
  const i = path.indexOf("/");
  return [path.slice(0, i), path.slice(i + 1)];
}

export function MessageComposer({
  target,
  onSent,
  onCancel,
}: {
  target: MessageTarget;
  onSent: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function send() {
    setBusy(true);
    setFailure(undefined);
    try {
      const [ns, slug] = splitProject(target.project);
      await api.sendAgentMessage(ns, slug, {
        body,
        to_client: target.kind === "session" ? target.presence.client_id : undefined,
      });
      setBody("");
      onSent();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  const heading =
    target.kind === "session"
      ? t("messages.toSession", {
          name: target.person?.name || t("plans.nobody"),
          client: target.presence.client_type,
        })
      : t("messages.toEveryone");

  return (
    <Stack gap="sm">
      <Cell primary={heading} />
      <TextArea label={t("messages.body")} value={body} onChange={setBody} rows={3} labelled />
      <Note>{t("messages.hint")}</Note>
      <Row gap="sm">
        <Btn kind="primary" disabled={busy || !body.trim()} onClick={() => void send()}>
          {t("messages.send")}
        </Btn>
        <Btn onClick={onCancel} disabled={busy}>
          {t("messages.cancel")}
        </Btn>
      </Row>
      {failure !== undefined && <ErrorState error={failure} />}
    </Stack>
  );
}

function recipient(m: AgentMessage, people: Record<string, Person>, t: ReturnType<typeof useT>) {
  const name = (m.to_principal && people[m.to_principal]?.name) || t("plans.nobody");
  switch (m.audience) {
    case "session":
      return t("messages.to.session", { name });
    case "principal":
      return t("messages.to.principal", { name });
    default:
      return t("messages.to.project");
  }
}

function MessageState({ m }: { m: AgentMessage }) {
  const t = useT();
  const acked = m.deliveries.filter((d) => d.acked_at).length;
  const delivered = m.deliveries.length;
  return (
    <Row gap="sm">
      {acked > 0 && <Badge tone="ok">{t("messages.state.acked", { count: acked })}</Badge>}
      {delivered > acked && (
        <Badge tone="info">{t("messages.state.delivered", { count: delivered - acked })}</Badge>
      )}
      {delivered === 0 && !m.expired && <Badge tone="warn">{t("messages.state.waiting")}</Badge>}
      {m.expired && <Badge>{t("messages.state.expired")}</Badge>}
    </Row>
  );
}

/**
 * What the reader sent in this project, newest first, with who received and
 * acknowledged each. `pulse` is the board stream's: a delivery or an
 * acknowledgement is announced there, and the list re-reads.
 */
export function SentMessages({
  namespace,
  slug,
  pulse,
}: {
  namespace: string;
  slug: string;
  pulse: number;
}) {
  const t = useT();
  const f = useFormat();
  const [people, setPeople] = useState<Record<string, Person>>({});
  const list = useList(
    async (cursor?: string) => {
      const page = await api.sentAgentMessages(namespace, slug, cursor);
      setPeople((prev) => ({ ...prev, ...page.people }));
      return page;
    },
    [namespace, slug, pulse],
  );

  if (list.loading || (list.items.length === 0 && !list.error)) return null;
  return (
    <Panel title={t("messages.sentTitle")} tight>
      {list.error !== undefined && <ErrorState error={list.error} onRetry={list.reload} />}
      <DataTable
        columns={[
          { key: "message", label: t("messages.column.message") },
          { key: "to", label: t("messages.column.to") },
          { key: "state", label: t("messages.column.state"), align: "r" },
        ]}
      >
        {list.items.map((m) => (
          <tr key={m.id}>
            <td>
              <Cell
                primary={m.body}
                secondary={
                  <span className="dim">
                    {t("messages.sentAt", { when: f.relative(m.created_at) })}
                  </span>
                }
              />
            </td>
            <td>
              <Cell
                primary={recipient(m, people, t)}
                secondary={
                  m.deliveries.length > 0 ? (
                    <Mono dim>
                      {[...new Set(m.deliveries.map((d) => d.client_type))].join(", ")}
                    </Mono>
                  ) : undefined
                }
              />
            </td>
            <td className="r">
              <MessageState m={m} />
            </td>
          </tr>
        ))}
      </DataTable>
      <Pager
        shown={list.items.length}
        hasMore={list.hasMore}
        onLoadMore={list.loadMore}
        busy={list.loadingMore}
      />
    </Panel>
  );
}
