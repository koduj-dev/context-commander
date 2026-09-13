import { useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { api } from "../lib/api";
import { scopeLabel, trustLabel } from "../lib/labels";
import { useDebounced, useList, useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  PageHead,
  Pager,
  Panel,
  Scope,
  SelectField,
  TextField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * The skills that actually apply here.
 *
 * `/skills/effective` has already composed installation, organization and
 * project bindings and picked a winner per skill, so this table is a
 * readout of a decision the server made — it does not re-derive it. The scope
 * rail on each row is what makes the composition visible: violet for a
 * binding inherited from a wider scope, pink for one made here.
 */
export function ProjectSkills() {
  const t = useT();
  const { project, namespace, slug } = useProject();
  const [installing, setInstalling] = useState(false);
  const [pinning, setPinning] = useState<string>();
  const [overriding, setOverriding] = useState<string>();
  const { data, error, loading, reload } = useResource(
    () => api.effectiveSkills(namespace, slug),
    [namespace, slug],
  );

  const bindings = data ?? [];
  const installed = new Set(bindings.map((b) => b.skill_id));

  async function setPinned(bindingID: string, pinned: boolean) {
    setPinning(bindingID);
    try {
      await api.setSkillPinned(namespace, slug, bindingID, pinned);
    } finally {
      // Re-read either way: the call answers 204, so the effective set is
      // the only place the new state exists.
      setPinning(undefined);
      reload();
    }
  }

  return (
    <>
      <PageHead
        title={t("skills.title")}
        lead={t("skills.lead")}
        actions={
          <>
            <Link className="btn" to="/registry">
              {t("skills.browse")}
            </Link>
            <Btn kind="primary" onClick={() => setInstalling((v) => !v)}>
              {t("skills.install")}
            </Btn>
          </>
        }
      />

      {overriding && (
        <div className="mb">
          <OverrideSkill
            skillID={overriding}
            onDone={() => {
              reload();
              setOverriding(undefined);
            }}
            onCancel={() => setOverriding(undefined)}
          />
        </div>
      )}

      {installing && (
        <div className="mb">
          <InstallSkill
            installed={installed}
            onInstalled={() => {
              reload();
              setInstalling(false);
            }}
          />
        </div>
      )}

      <Panel
        title={t("skills.title")}
        tight
        footer={<span className="dim">{t("skill.pinLead")}</span>}
      >
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : bindings.length === 0 ? (
          <EmptyState title={t("skills.empty")} body={t("skills.emptyBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "skill", label: t("skills.column.skill") },
              { key: "scope", label: t("skills.column.livesAt") },
              { key: "version", label: t("skills.column.version") },
              { key: "state", label: t("skills.column.state"), align: "r" },
              { key: "pin", align: "r" },
            ]}
          >
            {bindings.map((b) => (
              <tr key={b.id}>
                <td>
                  <Link to={`/registry/${b.skill_id}`}>
                    <Mono truncate>{b.skill_id}</Mono>
                  </Link>
                </td>
                <td>
                  <Scope kind={b.scope}>{scopeLabel(t, b.scope)}</Scope>
                </td>
                <td>
                  <Mono dim truncate>
                    {b.skill_version_id}
                  </Mono>
                </td>
                <td className="r">
                  {b.managed && <Badge tone="info">managed</Badge>}{" "}
                  {b.overridable ? null : <Badge tone="warn">locked</Badge>}{" "}
                  {b.pinned && <Badge tone="active">{t("skill.pinned")}</Badge>}
                </td>
                <td className="r">
                  {/* An inherited binding can be shadowed, unless whoever
                      mandated it marked it not overridable. */}
                  {b.scope !== "project" && b.overridable && (
                    <Btn small onClick={() => setOverriding(b.skill_id)}>
                      {t("skills.override")}
                    </Btn>
                  )}{" "}
                  {/* Only a binding this project made is this project's to
                      hold; the server refuses an inherited one. */}
                  {b.scope === "project" && b.scope_ref === project.id ? (
                    <Btn
                      small
                      disabled={pinning !== undefined}
                      onClick={() => void setPinned(b.id, !b.pinned)}
                    >
                      {pinning === b.id
                        ? t("common.loading")
                        : b.pinned
                          ? t("skill.unpin")
                          : t("skill.pin")}
                    </Btn>
                  ) : (
                    <span className="dim">{t("skill.inherited")}</span>
                  )}
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>
    </>
  );
}

/**
 * Installing a skill into this project.
 *
 * It searches the same catalogue the registry screen does, because that is
 * the only list of installable things the API offers — and it pins the
 * latest version, since a binding names one concrete version and the newest
 * is the only one a person picking from a search result has actually seen.
 * A specific older version is chosen from the skill's own screen.
 */
function InstallSkill({
  installed,
  onInstalled,
}: {
  installed: Set<string>;
  onInstalled: () => void;
}) {
  const t = useT();
  const { project, namespace, slug } = useProject();
  const [query, setQuery] = useState("");
  const debounced = useDebounced(query);
  const [busy, setBusy] = useState<string>();
  const [failure, setFailure] = useState<unknown>();

  const list = useList((cursor) => api.searchSkills(debounced || undefined, cursor), [debounced]);

  async function install(skillID: string, versionID: string) {
    setBusy(skillID);
    setFailure(undefined);
    try {
      await api.installSkill(namespace, slug, {
        scope: "project",
        scope_ref: project.id,
        skill_id: skillID,
        skill_version_id: versionID,
        overridable: true,
      });
      onInstalled();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(undefined);
    }
  }

  return (
    <Panel
      title={t("skills.install")}
      right={
        <TextField
          label={t("registry.search")}
          type="search"
          value={query}
          onChange={setQuery}
          placeholder={t("registry.search")}
        />
      }
      tight
      footer={
        <>
          <span className="dim">{t("skill.installLead")}</span>
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
        <EmptyState title={t("registry.empty")} body={t("registry.emptyBody")} />
      ) : (
        <DataTable
          columns={[
            { key: "skill", label: t("skills.column.skill") },
            { key: "scope", label: t("registry.livesAt") },
            { key: "trust", label: t("skills.column.trust") },
            { key: "action", align: "r" },
          ]}
        >
          {list.items.map((s) => (
            <tr key={s.skill_id}>
              <td>
                <Cell
                  primary={<Link to={`/registry/${s.skill_id}`}>{s.name}</Link>}
                  secondary={<Mono dim>v{s.latest_version}</Mono>}
                />
              </td>
              <td>
                <Scope kind={s.scope}>{scopeLabel(t, s.scope)}</Scope>
              </td>
              <td>
                <Badge tone={s.trust_status === "signed" ? "ok" : "warn"}>
                  {trustLabel(t, s.trust_status)}
                </Badge>
              </td>
              <td className="r">
                {installed.has(s.skill_id) ? (
                  <span className="dim">{t("skill.installed")}</span>
                ) : (
                  <Btn
                    small
                    kind="primary"
                    disabled={busy !== undefined}
                    onClick={() => void install(s.skill_id, s.latest_version_id)}
                  >
                    {busy === s.skill_id ? t("common.loading") : t("skill.install")}
                  </Btn>
                )}
              </td>
            </tr>
          ))}
        </DataTable>
      )}
      {failure !== undefined && <ErrorState error={failure} />}
    </Panel>
  );
}

/**
 * Shadowing an inherited binding with a version this project chose.
 *
 * Offered only where it can work: a binding marked not overridable refuses
 * this, which is the whole point of the flag — it is how a mandated
 * security-review skill stays mandated.
 */
function OverrideSkill({
  skillID,
  onDone,
  onCancel,
}: {
  skillID: string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const versions = useList((cursor) => api.skillVersions(skillID, cursor), [skillID]);
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  // Default to the newest, which is what "override" usually means in
  // practice; versions come back oldest-first.
  const newest = versions.items.length
    ? versions.items.reduce((a, b) => (b.version > a.version ? b : a))
    : undefined;
  const selected = chosen || newest?.id || "";

  async function submit() {
    if (!selected || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.overrideSkill(namespace, slug, {
        skill_id: skillID,
        skill_version_id: selected,
        overridable: true,
      });
      onDone();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title={t("skills.override")}
      footer={<span className="dim">{t("skills.overrideLead")}</span>}
    >
      {versions.loading ? (
        <Loading />
      ) : versions.error ? (
        <ErrorState error={versions.error} onRetry={versions.reload} />
      ) : (
        <div className="row row-sm">
          <SelectField
            label={t("skills.overrideVersion")}
            value={selected}
            onChange={setChosen}
            options={versions.items.map((v) => ({ value: v.id, label: `v${v.version}` }))}
          />
          <span className="spacer" />
          <Btn onClick={onCancel} disabled={busy}>
            {t("common.cancel")}
          </Btn>
          <Btn kind="primary" onClick={() => void submit()} disabled={busy || !selected}>
            {busy ? t("common.loading") : t("skills.override")}
          </Btn>
        </div>
      )}
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
    </Panel>
  );
}
