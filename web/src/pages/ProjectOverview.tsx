import { useState } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { ApiError, api, type Handoff, type HandoffDetail } from "../lib/api";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  TextField,
  DataTable,
  Pager,
  Def,
  Defs,
  EmptyState,
  ErrorState,
  Grid,
  Loading,
  Mono,
  Note,
  Panel,
  Stack,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * What is going on in this project right now.
 *
 * The current handoff is the whole point of the screen: it is the one piece
 * of state an agent hands to the next one, and a person looking at this page
 * is usually asking "has someone left something for me".
 */
export function ProjectOverview() {
  const t = useT();
  const { project, namespace, slug } = useProject();

  const handoff = useResource(() => api.currentHandoff(namespace, slug), [namespace, slug]);

  // "There is no open handoff" is a 404 from this endpoint, and it is an
  // answer rather than a failure — rendering it as an error would put a red
  // panel on a project where nothing is wrong.
  const noneOpen = handoff.error instanceof ApiError && handoff.error.kind === "not_found";

  return (
    <Grid cols="2-1">
      <Stack>
        <Panel
          title={t("handoff.current")}
          right={handoff.data && <HandoffStatus handoff={handoff.data} />}
        >
          {handoff.loading ? (
            <Loading />
          ) : noneOpen ? (
            <>
              <EmptyState title={t("handoff.none")} body={t("handoff.noneBody")} />
              <CreateHandoff onCreated={handoff.reload} />
            </>
          ) : handoff.error ? (
            <ErrorState error={handoff.error} onRetry={handoff.reload} />
          ) : handoff.data ? (
            <HandoffBody handoff={handoff.data} onChanged={handoff.reload} />
          ) : null}
        </Panel>
      </Stack>

      <Stack top>
        <HandoffHistory />

        <Panel title={t("project.identity")}>
          <Defs>
            <Def term={t("project.path")}>
              <Mono>{`${namespace}/${slug}`}</Mono>
            </Def>
            <Def term={t("project.uuid")}>
              <Mono truncate>{project.id}</Mono>
            </Def>
            <Def term={t("scope.project")}>{project.visibility}</Def>
          </Defs>
        </Panel>

        <Panel title={t("project.notBuilt.title")}>
          <Note tone="info">{t("project.notBuilt.artifacts")}</Note>
        </Panel>
      </Stack>
    </Grid>
  );
}

/**
 * What came before.
 *
 * Paginated by cursor like everything that grows. Bodies are not fetched: the
 * list endpoint returns status and timestamps only, and pulling a body per
 * row to show a preview would be one request per row for text nobody asked
 * to read.
 */
function HandoffHistory() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  const list = useList((cursor) => api.handoffs(namespace, slug, cursor), [namespace, slug]);

  if (list.loading) return null;
  if (list.error) return <ErrorState error={list.error} onRetry={list.reload} />;
  if (list.items.length === 0) return null;

  return (
    <Panel
      title={t("handoff.history")}
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
      <DataTable
        columns={[
          { key: "created", label: t("handoff.column.created") },
          { key: "status", label: t("handoff.column.status"), align: "r" },
        ]}
      >
        {list.items.map((h) => (
          <tr key={h.id}>
            <td>
              <span className="dim">{f.relative(h.created_at)}</span>
            </td>
            <td className="r">
              <HandoffStatus handoff={h} />
            </td>
          </tr>
        ))}
      </DataTable>
    </Panel>
  );
}

function HandoffStatus({ handoff }: { handoff: Handoff }) {
  const t = useT();
  const tone =
    handoff.status === "open" ? "active" : handoff.status === "claimed" ? "warn" : "neutral";
  const label =
    handoff.status === "open"
      ? t("handoff.status.open")
      : handoff.status === "claimed"
        ? t("handoff.status.claimed")
        : t("handoff.status.closed");
  return <Badge tone={tone}>{label}</Badge>;
}

function HandoffBody({ handoff, onChanged }: { handoff: HandoffDetail; onChanged: () => void }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  async function claim() {
    try {
      await api.claimHandoff(namespace, slug, handoff.id);
    } catch {
      // A 409 here is the coordination answer — another client won the race.
      // Reloading shows who, which is the useful thing to say.
    } finally {
      onChanged();
    }
  }

  async function close() {
    // Confirmed because it is the one action here that cannot be undone: a
    // closed handoff is no longer the current one and nobody can claim it.
    if (!window.confirm(t("handoff.confirmClose"))) return;
    try {
      await api.closeHandoff(namespace, slug, handoff.id);
    } catch {
      // Same reasoning as claim: reloading shows the state that won.
    } finally {
      onChanged();
    }
  }

  return (
    <>
      <p className="sec lead-out">{handoff.summary}</p>

      {handoff.completed_work && (
        <div className="mt">
          <div className="label-caps">{t("handoff.completed")}</div>
          <p className="sec">{handoff.completed_work}</p>
        </div>
      )}

      {handoff.next_actions && (
        <div className="mt">
          <div className="label-caps">{t("handoff.next")}</div>
          <p className="sec">{handoff.next_actions}</p>
        </div>
      )}

      <div className="mt row row-sm">
        <span className="dim">{f.relative(handoff.created_at)}</span>
        <span className="spacer" />
        {handoff.status === "open" && (
          <Btn kind="primary" small onClick={() => void claim()}>
            {t("handoff.claim")}
          </Btn>
        )}
        {handoff.status !== "closed" && (
          <Btn small onClick={() => void close()}>
            {t("handoff.close")}
          </Btn>
        )}
      </div>

      {handoff.status === "claimed" && (
        <div className="mt">
          <Note tone="warn">
            {handoff.claimed_by_principal
              ? t("handoff.claimedBy", { name: handoff.claimed_by_principal })
              : t("handoff.status.claimed")}
          </Note>
        </div>
      )}
    </>
  );
}

/**
 * Leaving a handoff from the interface.
 *
 * The same three fields an agent writes over MCP, because it is the same
 * record: a person picking up after an agent, or handing back to one, should
 * not be writing a different shape of note.
 */
function CreateHandoff({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const { namespace, slug } = useProject();

  const [summary, setSummary] = useState("");
  const [completed, setCompleted] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (!summary.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createHandoff(namespace, slug, {
        summary: summary.trim(),
        completed_work: completed.trim(),
        next_actions: next.trim(),
      });
      setSummary("");
      setCompleted("");
      setNext("");
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt stack-sm">
      <TextField
        label={t("handoff.summary")}
        value={summary}
        onChange={setSummary}
        placeholder={t("handoff.summaryHelp")}
        grow
      />
      <TextField
        label={t("handoff.completed")}
        value={completed}
        onChange={setCompleted}
        placeholder={t("handoff.completedHelp")}
        grow
      />
      <TextField
        label={t("handoff.next")}
        value={next}
        onChange={setNext}
        placeholder={t("handoff.nextHelp")}
        grow
      />
      <div className="row row-sm">
        <span className="dim">{t("handoff.createLead")}</span>
        <span className="spacer" />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !summary.trim()}>
          {busy ? t("common.loading") : t("handoff.create")}
        </Btn>
      </div>
      {failure !== undefined && <ErrorState error={failure} />}
    </div>
  );
}
