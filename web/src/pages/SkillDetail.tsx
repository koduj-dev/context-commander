import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { api, type SkillContent } from "../lib/api";
import { scopeLabel, trustLabel } from "../lib/labels";
import { useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Code,
  DataTable,
  Def,
  Defs,
  ErrorState,
  Grid,
  Loading,
  Mono,
  Note,
  PageHead,
  Pager,
  Panel,
  Scope,
  SelectField,
  Stack,
} from "../ui";

/**
 * One skill: its versions, what is inside them, and where it lives.
 *
 * The name and scope come from the catalogue rather than from a read of the
 * skill itself — there is no `GET /skills/{id}`, only versions and content —
 * so this page finds its own entry by searching for the name it was given.
 * That is a workaround, and it is here rather than hidden in the API client
 * so it is visible to whoever adds the missing endpoint.
 */
export function SkillDetail() {
  const t = useT();
  const { skillID = "" } = useParams();
  const [params, setParams] = useSearchParams();

  const versions = useList((cursor) => api.skillVersions(skillID, cursor), [skillID]);

  // Default to the newest version rather than the first row: versions come
  // back oldest-first and the newest is what anyone means by "this skill".
  const newest = versions.items.length
    ? versions.items.reduce((a, b) => (b.version > a.version ? b : a))
    : undefined;
  const selectedID = params.get("v") ?? newest?.id ?? "";

  const content = useResource(
    () => (selectedID ? api.skillVersion(selectedID) : Promise.resolve(undefined)),
    [selectedID],
  );

  const name = content.data?.skill_name ?? "";
  useCrumbs(`skill:${skillID}:${name}`, () => ({
    path: [t("registry.title")],
    current: name || skillID,
  }));

  if (versions.loading) return <Loading />;
  if (versions.error) return <ErrorState error={versions.error} onRetry={versions.reload} />;
  if (versions.items.length === 0) {
    return (
      <>
        <PageHead title={t("skill.notFound")} lead={t("skill.notFoundBody")} />
        <Link className="btn" to="/registry">
          {t("skill.backToRegistry")}
        </Link>
      </>
    );
  }

  return (
    <>
      <PageHead
        title={name || skillID}
        lead={content.data?.skill_origin ? <Mono dim>{content.data.skill_origin}</Mono> : undefined}
        actions={
          <Link className="btn" to="/registry">
            {t("skill.backToRegistry")}
          </Link>
        }
      />

      <Grid cols="2-1">
        <Stack>
          <Panel
            title={t("skill.content")}
            sub={content.data ? `v${content.data.version}` : undefined}
            footer={<span className="dim">{t("skill.contentLead")}</span>}
          >
            {content.loading ? (
              <Loading />
            ) : content.error ? (
              <ErrorState error={content.error} onRetry={content.reload} />
            ) : (
              <Code>{content.data?.content}</Code>
            )}
          </Panel>
        </Stack>

        <Stack top>
          <Panel
            title={t("skill.versions")}
            tight
            footer={
              <>
                <span className="dim">{t("skill.versionsLead")}</span>
                <Pager
                  shown={versions.items.length}
                  hasMore={versions.hasMore}
                  onLoadMore={versions.loadMore}
                  busy={versions.loadingMore}
                />
              </>
            }
          >
            <DataTable
              columns={[
                { key: "v", label: t("artifacts.column.version") },
                { key: "trust", label: t("skills.column.trust"), align: "r" },
              ]}
            >
              {versions.items.map((v) => (
                <tr key={v.id} className={v.id === selectedID ? "active" : ""}>
                  <td>
                    <Btn small onClick={() => setParams({ v: v.id }, { replace: true })}>
                      v{v.version}
                    </Btn>
                  </td>
                  <td className="r">
                    <Badge tone={v.trust_status === "signed" ? "ok" : "warn"}>
                      {trustLabel(t, v.trust_status)}
                    </Badge>
                  </td>
                </tr>
              ))}
            </DataTable>
          </Panel>

          {content.data && <Provenance version={content.data} />}
          <Promote skillID={skillID} />
        </Stack>
      </Grid>
    </>
  );
}

function Provenance({ version: data }: { version: SkillContent }) {
  const t = useT();

  return (
    <Panel title={t("skill.provenance")}>
      <Defs>
        <Def term={t("skill.checksum")}>
          <Mono truncate>{data.checksum}</Mono>
        </Def>
        {data.skill_author && <Def term={t("skill.author")}>{data.skill_author}</Def>}
        {data.skill_origin && (
          <Def term={t("skill.origin")}>
            <Mono>{data.skill_origin}</Mono>
          </Def>
        )}
        <Def term={t("skill.capabilities")}>
          {data.declared_capabilities.length === 0 ? (
            <span className="dim">{t("common.none")}</span>
          ) : (
            data.declared_capabilities.map((c) => (
              <span key={c} className="inline-badge">
                {c}
              </span>
            ))
          )}
        </Def>
      </Defs>
    </Panel>
  );
}

/**
 * Widening where a skill lives.
 *
 * Only "installation" is offered. Promoting to an organization needs the
 * organization's UUID as a scope_ref, and picking the right one is a choice
 * this screen cannot present honestly yet — a dropdown of the caller's
 * organizations is right, but the skill's current scope is not available
 * here to say which moves are actually upward.
 */
function Promote({ skillID }: { skillID: string }) {
  const t = useT();
  const [scope, setScope] = useState("installation");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [moved, setMoved] = useState<string>();

  async function submit() {
    if (busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const res = await api.promoteSkill(skillID, scope);
      setMoved(res.scope);
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("skill.promote")}>
      <p className="sec lead-out">{t("skill.promoteLead")}</p>
      <div className="row row-sm">
        <SelectField
          label={t("skill.promoteTo")}
          value={scope}
          onChange={setScope}
          options={[{ value: "installation", label: scopeLabel(t, "installation") }]}
        />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy}>
          {busy ? t("common.loading") : t("skill.promote")}
        </Btn>
      </div>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      {moved && (
        <div className="mt">
          <Note tone="info">
            {t("skill.promoted", { scope: scopeLabel(t, moved) })} <Scope kind={moved} />
          </Note>
        </div>
      )}
    </Panel>
  );
}
