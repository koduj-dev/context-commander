import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { ApiError, api, type Artifact } from "../lib/api";
import { useList, useResource } from "../lib/resource";
import {
  Btn,
  Cell,
  Code,
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
  SelectField,
  Stack,
  TextArea,
  TextField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * The shared store, as a person sees it.
 *
 * The selected path lives in the URL rather than in component state, so a
 * link to one artifact is a link to one artifact — this is a screen people
 * paste at each other.
 */
export function ProjectArtifacts() {
  const t = useT();
  const { namespace, slug } = useProject();
  const [params, setParams] = useSearchParams();
  const selected = params.get("path") ?? "";
  const [creating, setCreating] = useState(false);

  const list = useList((cursor) => api.artifacts(namespace, slug, cursor), [namespace, slug]);

  function select(path: string) {
    // `replace` so browsing paths does not build a back-button history of
    // every row someone glanced at.
    setParams(path ? { path } : {}, { replace: true });
    setCreating(false);
  }

  return (
    <>
      <PageHead
        title={t("artifacts.title")}
        lead={t("artifacts.lead")}
        actions={
          <Btn
            kind="primary"
            onClick={() => {
              setCreating(true);
              setParams({}, { replace: true });
            }}
          >
            {t("artifacts.new")}
          </Btn>
        }
      />

      <Grid cols="2-1">
        <Stack>
          <Panel
            title={t("artifacts.title")}
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
              <EmptyState title={t("artifacts.empty")} body={t("artifacts.emptyBody")} />
            ) : (
              <DataTable
                columns={[
                  { key: "path", label: t("artifacts.column.path") },
                  { key: "version", label: t("artifacts.column.version"), align: "r" },
                ]}
              >
                {list.items.map((a) => (
                  <ArtifactRow
                    key={a.logical_path}
                    artifact={a}
                    active={a.logical_path === selected}
                    onSelect={() => select(a.logical_path)}
                  />
                ))}
              </DataTable>
            )}
          </Panel>
        </Stack>

        <Stack top>
          {creating ? (
            <NewArtifact
              onCreated={(path) => {
                list.reload();
                select(path);
              }}
              onCancel={() => setCreating(false)}
            />
          ) : selected ? (
            <ArtifactDetail
              path={selected}
              onChanged={list.reload}
              onDeleted={() => {
                list.reload();
                select("");
              }}
            />
          ) : (
            <Panel title={t("artifacts.select")}>
              <EmptyState title={t("artifacts.select")} body={t("artifacts.selectBody")} />
            </Panel>
          )}
        </Stack>
      </Grid>
    </>
  );
}

function ArtifactRow({
  artifact,
  active,
  onSelect,
}: {
  artifact: Artifact;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <tr className={active ? "active" : ""}>
      <td>
        <Cell
          primary={
            <a
              href={`?path=${encodeURIComponent(artifact.logical_path)}`}
              onClick={(e) => {
                e.preventDefault();
                onSelect();
              }}
            >
              <Mono>{artifact.logical_path}</Mono>
            </a>
          }
          secondary={<Mono dim>{artifact.content_type}</Mono>}
        />
      </td>
      <td className="r">
        <span className="num">v{artifact.current_version}</span>
      </td>
    </tr>
  );
}

const TEXT_TYPES = [
  "text/markdown",
  "text/plain",
  "application/json",
  "application/yaml",
  "text/csv",
];

/** Text is rendered and editable; anything else is described, not guessed at. */
function isText(contentType: string): boolean {
  return (
    contentType.startsWith("text/") ||
    contentType === "application/json" ||
    contentType === "application/yaml" ||
    contentType.endsWith("+json") ||
    contentType.endsWith("+yaml")
  );
}

function ArtifactDetail({
  path,
  onChanged,
  onDeleted,
}: {
  path: string;
  onChanged: () => void;
  onDeleted: () => void;
}) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const [version, setVersion] = useState<number>();
  const [editing, setEditing] = useState(false);

  // A new path is a different artifact; the version from the previous one
  // means nothing here and would request a version that may not exist.
  useEffect(() => {
    setVersion(undefined);
    setEditing(false);
  }, [path]);

  const stat = useResource(() => api.artifactStat(namespace, slug, path), [namespace, slug, path]);
  const history = useResource(
    () => api.artifactHistory(namespace, slug, path),
    [namespace, slug, path],
  );
  const content = useResource(
    () => api.artifactContent(namespace, slug, path, version),
    [namespace, slug, path, version],
  );

  const versions = history.data ?? [];
  const latest = stat.data?.current_version;
  const renderable = stat.data ? isText(stat.data.content_type) : false;

  function reloadAll() {
    setVersion(undefined);
    stat.reload();
    history.reload();
    content.reload();
    onChanged();
  }

  async function remove() {
    if (!window.confirm(t("artifacts.confirmDelete", { path }))) return;
    await api.deleteArtifact(namespace, slug, path);
    onDeleted();
  }

  return (
    <>
      <Panel
        title={t("artifacts.history")}
        sub={<Mono truncate>{path}</Mono>}
        tight
        right={
          <Btn kind="danger" small onClick={() => void remove()}>
            {t("artifacts.delete")}
          </Btn>
        }
      >
        {history.loading ? (
          <Loading />
        ) : history.error ? (
          <ErrorState error={history.error} onRetry={history.reload} />
        ) : (
          <DataTable
            columns={[
              { key: "v", label: t("artifacts.column.version") },
              { key: "size", label: t("artifacts.column.size"), align: "r" },
              { key: "hash", label: t("artifacts.column.hash") },
            ]}
          >
            {versions.map((v) => (
              // Highlighted against the version actually on screen, not
              // against the first row: history comes back oldest-first while
              // a versionless read returns the newest, so anything derived
              // from the row order marks the wrong one.
              <tr key={v.version} className={v.version === content.data?.version ? "active" : ""}>
                <td>
                  <Btn small onClick={() => setVersion(v.version)}>
                    v{v.version}
                  </Btn>
                  {v.version === latest && (
                    <span className="inline-badge">{t("artifacts.latest")}</span>
                  )}
                </td>
                <td className="r">
                  <span className="num">{f.bytes(v.size_bytes)}</span>
                </td>
                <td>
                  <Mono dim truncate>
                    {v.content_hash}
                  </Mono>
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>

      {editing && content.data && stat.data ? (
        <Editor
          path={path}
          contentType={stat.data.content_type}
          startedFrom={content.data.version}
          initial={content.data.text}
          onSaved={() => {
            setEditing(false);
            reloadAll();
          }}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <Panel
          title={t("artifacts.content")}
          sub={content.data ? t("artifacts.viewing", { version: content.data.version }) : undefined}
          right={
            renderable && (
              <Btn small onClick={() => setEditing(true)}>
                {t("artifacts.edit")}
              </Btn>
            )
          }
        >
          {content.loading || stat.loading ? (
            <Loading />
          ) : content.error ? (
            <ErrorState error={content.error} onRetry={content.reload} />
          ) : !renderable ? (
            <>
              <EmptyState title={t("artifacts.binary")} body={t("artifacts.binaryBody")} />
              <Note tone="info">{t("artifacts.binaryNoEdit")}</Note>
            </>
          ) : (
            <Code>{content.data?.text}</Code>
          )}
        </Panel>
      )}
    </>
  );
}

/**
 * Editing one version into the next.
 *
 * The save carries the version it started from, so a write that lands after
 * someone else's is refused rather than applied. That refusal is rendered as
 * what it is — their work still exists and yours has not been lost, it just
 * has to be merged — and never as a generic failure.
 */
function Editor({
  path,
  contentType,
  startedFrom,
  initial,
  onSaved,
  onCancel,
}: {
  path: string;
  contentType: string;
  startedFrom: number;
  initial: string;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [text, setText] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [stale, setStale] = useState(false);

  async function save() {
    if (busy) return;
    setBusy(true);
    setFailure(undefined);
    setStale(false);
    try {
      await api.writeArtifact(namespace, slug, path, text, contentType, startedFrom);
      onSaved();
    } catch (err) {
      if (err instanceof ApiError && err.kind === "conflict") setStale(true);
      else setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title={t("artifacts.edit")}
      sub={t("artifacts.editingFrom", { version: startedFrom })}
      footer={<span className="dim">{t("artifacts.concurrency")}</span>}
    >
      <TextArea label={t("artifacts.content")} value={text} onChange={setText} disabled={busy} />

      {stale && (
        <div className="mt">
          <Note tone="warn">
            <b>{t("artifacts.staleWrite")}</b>
            <div className="mt-sm">{t("artifacts.staleWriteBody")}</div>
          </Note>
          <div className="mt">
            <Btn onClick={onSaved}>{t("artifacts.reload")}</Btn>
          </div>
        </div>
      )}
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}

      <div className="mt row row-sm">
        <span className="spacer" />
        <Btn onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn kind="primary" onClick={() => void save()} disabled={busy || text === initial}>
          {busy ? t("common.loading") : t("artifacts.save")}
        </Btn>
      </div>
    </Panel>
  );
}

/** A path that does not exist yet is written with expected_version 0. */
function NewArtifact({
  onCreated,
  onCancel,
}: {
  onCreated: (path: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const { namespace, slug } = useProject();
  const [path, setPath] = useState("");
  const [contentType, setContentType] = useState("text/markdown");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function create() {
    if (!path.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.writeArtifact(namespace, slug, path.trim(), text, contentType, 0);
      onCreated(path.trim());
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      title={t("artifacts.new")}
      sub={t("artifacts.editingNew")}
      footer={<span className="dim">{t("artifacts.concurrency")}</span>}
    >
      <Row gap="sm">
        <TextField
          label={t("artifacts.newPath")}
          value={path}
          onChange={setPath}
          placeholder={t("artifacts.newPathHelp")}
          grow
        />
        <SelectField
          label={t("artifacts.contentType")}
          value={contentType}
          onChange={setContentType}
          options={TEXT_TYPES.map((v) => ({ value: v, label: v }))}
        />
      </Row>

      <div className="mt">
        <TextArea label={t("artifacts.content")} value={text} onChange={setText} disabled={busy} />
      </div>

      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}

      <div className="mt row row-sm">
        <span className="spacer" />
        <Btn onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn kind="primary" onClick={() => void create()} disabled={busy || !path.trim()}>
          {busy ? t("common.loading") : t("artifacts.save")}
        </Btn>
      </div>
    </Panel>
  );
}
