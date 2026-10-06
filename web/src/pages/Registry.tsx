import { useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useAuth } from "../app/auth";
import { useCrumbs } from "../app/crumbs";
import { api, principalName } from "../lib/api";
import { scopeLabel, trustLabel } from "../lib/labels";
import { useDebounced, useList } from "../lib/resource";
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
  Pager,
  PageHead,
  Panel,
  Row,
  Scope,
  SelectField,
  TextArea,
  TextField,
} from "../ui";

/**
 * The catalogue an agent searches, rendered for a person.
 *
 * Scoped, not public: what comes back is what this principal can see —
 * skills in their projects, in their organizations, and on the installation
 * shelf. The scope column is not decoration; it is the difference between
 * "everyone here has this" and "this lives in one project".
 */
export function Registry() {
  const t = useT();
  useCrumbs("registry", () => ({ path: [], current: t("registry.title") }));
  const [query, setQuery] = useState("");
  const [publishing, setPublishing] = useState(false);
  const debounced = useDebounced(query);

  const list = useList((cursor) => api.searchSkills(debounced || undefined, cursor), [debounced]);

  return (
    <>
      <PageHead
        title={t("registry.title")}
        lead={t("registry.lead")}
        actions={
          <Btn kind="primary" onClick={() => setPublishing((v) => !v)}>
            {t("registry.publish")}
          </Btn>
        }
      />

      {publishing && (
        <div className="mb">
          <PublishSkill
            onPublished={() => {
              list.reload();
              setPublishing(false);
            }}
            onCancel={() => setPublishing(false)}
          />
        </div>
      )}

      <Panel
        title={t("registry.title")}
        sub={
          debounced
            ? t("registry.results", { count: list.items.length, query: debounced })
            : undefined
        }
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
          <Pager
            shown={list.items.length}
            hasMore={list.hasMore}
            onLoadMore={list.loadMore}
            busy={list.loadingMore}
          />
        }
      >
        {list.loading ? (
          <Loading />
        ) : list.error ? (
          <ErrorState error={list.error} onRetry={list.reload} />
        ) : list.items.length === 0 ? (
          debounced ? (
            <EmptyState title={t("empty.noResults")} body={t("empty.noResultsBody")} />
          ) : (
            <EmptyState title={t("registry.empty")} body={t("registry.emptyBody")} />
          )
        ) : (
          <DataTable
            columns={[
              { key: "skill", label: t("skills.column.skill") },
              { key: "author", label: t("registry.column.author") },
              { key: "scope", label: t("registry.livesAt") },
              { key: "latest", label: t("registry.column.latest"), align: "r" },
              { key: "trust", label: t("skills.column.trust"), align: "r" },
            ]}
          >
            {list.items.map((s) => (
              <tr key={s.skill_id}>
                <td>
                  <Cell
                    primary={<Link to={`/registry/${s.skill_id}`}>{s.name}</Link>}
                    secondary={
                      <Mono dim truncate>
                        {s.latest_checksum}
                      </Mono>
                    }
                  />
                </td>
                <td>
                  <span className="sec">{s.author ?? t("common.unknown")}</span>
                </td>
                <td>
                  <Scope kind={s.scope}>{scopeLabel(t, s.scope)}</Scope>
                </td>
                <td className="r">
                  <span className="num">v{s.latest_version}</span>
                </td>
                <td className="r">
                  <Badge tone={s.trust_status === "signed" ? "ok" : "warn"}>
                    {trustLabel(t, s.trust_status)}
                  </Badge>
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
 * Publishing a skill.
 *
 * Mounted at the top level rather than under a project, mirroring the API: a
 * skill can go straight onto the installation shelf, and a form that demanded
 * a project would have excluded the caller a self-hosted instance most wants
 * publishing.
 *
 * Publishing the same name twice creates a second skill, not a second
 * version — a new version needs the existing skill's id, which the skill's
 * own screen is the place to act from.
 */
function PublishSkill({
  onPublished,
  onCancel,
}: {
  onPublished: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const { me } = useAuth();
  const [name, setName] = useState("");
  const [author, setAuthor] = useState(principalName(me));
  const [origin, setOrigin] = useState("");
  const [scope, setScope] = useState("installation");
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [version, setVersion] = useState<number>();

  async function submit() {
    if (!name.trim() || !content.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const v = await api.publishSkill({
        scope,
        name: name.trim(),
        author: author.trim(),
        origin: origin.trim(),
        content,
      });
      setVersion(v.version);
      onPublished();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title={t("registry.publish")}
      footer={<span className="dim">{t("registry.publishScopeHelp")}</span>}
    >
      <p className="sec lead-out">{t("registry.publishLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("registry.publishName")}
          value={name}
          onChange={setName}
          placeholder={t("registry.publishName")}
          grow
        />
        <TextField
          label={t("registry.publishAuthor")}
          value={author}
          onChange={setAuthor}
          placeholder={t("registry.publishAuthor")}
          grow
        />
        <TextField
          label={t("registry.publishOrigin")}
          value={origin}
          onChange={setOrigin}
          placeholder={t("registry.publishOrigin")}
          grow
        />
        <SelectField
          label={t("registry.publishScope")}
          value={scope}
          onChange={setScope}
          options={[{ value: "installation", label: scopeLabel(t, "installation") }]}
        />
      </Row>

      <div className="mt">
        <TextArea
          label={t("registry.publishContent")}
          value={content}
          onChange={setContent}
          disabled={busy}
          labelled
        />
      </div>

      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      {version !== undefined && (
        <div className="mt">
          <Note tone="info">{t("registry.published", { version })}</Note>
        </div>
      )}

      <div className="mt row row-sm">
        <span className="spacer" />
        <Btn onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn
          kind="primary"
          onClick={() => void submit()}
          disabled={busy || !name.trim() || !content.trim()}
        >
          {busy ? t("common.loading") : t("registry.publish")}
        </Btn>
      </div>
    </Panel>
  );
}
