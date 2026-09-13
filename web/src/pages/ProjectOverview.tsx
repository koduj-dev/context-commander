import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { ApiError, api, type Handoff } from "../lib/api";
import { useResource } from "../lib/resource";
import {
  Badge,
  Btn,
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
            <EmptyState title={t("handoff.none")} body={t("handoff.noneBody")} />
          ) : handoff.error ? (
            <ErrorState error={handoff.error} onRetry={handoff.reload} />
          ) : handoff.data ? (
            <HandoffBody handoff={handoff.data} onChanged={handoff.reload} />
          ) : null}
        </Panel>
      </Stack>

      <Stack top>
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

function HandoffStatus({ handoff }: { handoff: Handoff }) {
  const t = useT();
  const tone = handoff.status === "open" ? "active" : handoff.status === "claimed" ? "warn" : "neutral";
  const label =
    handoff.status === "open"
      ? t("handoff.status.open")
      : handoff.status === "claimed"
        ? t("handoff.status.claimed")
        : t("handoff.status.closed");
  return <Badge tone={tone}>{label}</Badge>;
}

function HandoffBody({ handoff, onChanged }: { handoff: Handoff; onChanged: () => void }) {
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
          <Btn kind="primary" small onClick={claim}>
            {t("handoff.claim")}
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
