import { useEffect, useState } from "react";
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
  EmptyState,
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
  TextArea,
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

  // There is no `GET /skills/{id}`, so where the skill lives is learned by
  // finding it in the catalogue. Worth the extra request: without it this
  // screen would offer to promote a skill that is already as wide as it
  // goes, and the server's refusal would arrive as an unexplained conflict.
  const name = content.data?.skill_name ?? "";
  const catalogue = useResource(
    () => (name ? api.searchSkills(name, undefined, 25) : Promise.resolve({ items: [] })),
    [name],
  );
  const entry = (catalogue.data?.items ?? []).find((e) => e.skill_id === skillID);
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
          {content.data && (
            <NewVersion
              skillID={skillID}
              from={content.data}
              onPublished={() => {
                versions.reload();
                content.reload();
              }}
            />
          )}

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
          {entry && <Promote skillID={skillID} scope={entry.scope} />}
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
 * Only moves that exist are offered. A skill already on the installation
 * shelf has nowhere further to go, and the server refuses that with a
 * conflict — a control that produces a guaranteed refusal is worse than no
 * control, because the refusal reads as a fault.
 */
function Promote({ skillID, scope: current }: { skillID: string; scope: string }) {
  const t = useT();
  const orgs = useResource(() => api.myOrganizations(), []);
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [moved, setMoved] = useState<string>();

  // Organization is upward only from a project; installation is upward from
  // both. The value carries the scope and, for an organization, which one.
  const options = [
    ...(current === "project"
      ? (orgs.data ?? []).map((o) => ({ value: `organization:${o.id}`, label: o.name }))
      : []),
    { value: "installation:", label: scopeLabel(t, "installation") },
  ];
  const chosen = target || options[0]?.value || "";

  async function submit() {
    if (busy || !chosen) return;
    const [scope, ref] = chosen.split(":");
    setBusy(true);
    setFailure(undefined);
    try {
      const res = await api.promoteSkill(skillID, scope!, ref || undefined);
      setMoved(res.scope);
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (current === "installation") {
    return (
      <Panel title={t("skill.promote")}>
        <Defs>
          <Def term={t("skill.livesAtNow")}>
            <Scope kind={current}>{scopeLabel(t, current)}</Scope>
          </Def>
        </Defs>
        <div className="mt">
          <EmptyState title={t("skill.alreadyWidest")} body={t("skill.alreadyWidestBody")} />
        </div>
      </Panel>
    );
  }

  return (
    <Panel title={t("skill.promote")}>
      <p className="sec lead-out">{t("skill.promoteLead")}</p>
      <Defs>
        <Def term={t("skill.livesAtNow")}>
          <Scope kind={current}>{scopeLabel(t, current)}</Scope>
        </Def>
      </Defs>
      <div className="mt row row-sm">
        <SelectField
          label={t("skill.promoteTo")}
          value={chosen}
          onChange={setTarget}
          options={options}
        />
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || !chosen}>
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

/**
 * Correcting a skill.
 *
 * A published version is immutable, so the only way to fix one is to publish
 * the next. The editor starts from the version on screen rather than empty,
 * because "fix a typo" is the common case and retyping a skill to change one
 * line is how people stop fixing typos.
 */
function NewVersion({
  skillID,
  from,
  onPublished,
}: {
  skillID: string;
  from: SkillContent;
  onPublished: () => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState(from.content);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [published, setPublished] = useState<number>();

  // Opening the editor on a different version must start from that version.
  useEffect(() => setText(from.content), [from.content]);

  async function submit() {
    if (busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const v = await api.publishSkill({
        scope: "project",
        skill_id: skillID,
        name: from.skill_name,
        author: from.skill_author ?? "",
        origin: from.skill_origin ?? "",
        content: text,
      });
      setPublished(v.version);
      setOpen(false);
      onPublished();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <Panel title={t("skill.newVersion")}>
        <p className="sec lead-out">{t("skill.newVersionLead")}</p>
        <div className="row row-sm">
          {published !== undefined && (
            <Note tone="info">{t("skill.publishedVersion", { version: published })}</Note>
          )}
          <span className="spacer" />
          <Btn onClick={() => setOpen(true)}>{t("skill.newVersion")}</Btn>
        </div>
      </Panel>
    );
  }

  return (
    <Panel
      title={t("skill.newVersion")}
      sub={t("skill.newVersionFrom", { version: from.version })}
      footer={<span className="dim">{t("skill.newVersionLead")}</span>}
    >
      <TextArea
        label={t("registry.publishContent")}
        value={text}
        onChange={setText}
        disabled={busy}
        labelled
      />
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      <div className="mt row row-sm">
        <span className="spacer" />
        <Btn onClick={() => setOpen(false)} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn kind="primary" onClick={() => void submit()} disabled={busy || text === from.content}>
          {busy ? t("common.loading") : t("registry.publish")}
        </Btn>
      </div>
    </Panel>
  );
}
