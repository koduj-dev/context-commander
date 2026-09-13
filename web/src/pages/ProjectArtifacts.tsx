import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api, type Artifact } from "../lib/api";
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
  Pager,
  Panel,
  Stack,
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

  const list = useList(
    (cursor) => api.artifacts(namespace, slug, cursor),
    [namespace, slug],
  );

  function select(path: string) {
    // `replace` so browsing paths does not build a back-button history of
    // every row someone glanced at.
    setParams(path ? { path } : {}, { replace: true });
  }

  return (
    <Grid cols="2-1">
      <Stack>
        <Panel
          title={t("artifacts.title")}
          sub={list.items.length ? undefined : t("artifacts.empty")}
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
          <p className="sec lead-out">{t("artifacts.lead")}</p>

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
        {selected ? (
          <ArtifactDetail path={selected} />
        ) : (
          <Panel title={t("artifacts.select")}>
            <EmptyState title={t("artifacts.select")} body={t("artifacts.selectBody")} />
          </Panel>
        )}
        <Panel title={t("project.notBuilt.title")}>
          <Note tone="info">{t("artifacts.noWrite")}</Note>
        </Panel>
      </Stack>
    </Grid>
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

/** Text is rendered; anything else is described rather than guessed at. */
function isText(contentType: string): boolean {
  return (
    contentType.startsWith("text/") ||
    contentType === "application/json" ||
    contentType === "application/yaml" ||
    contentType.endsWith("+json") ||
    contentType.endsWith("+yaml")
  );
}

function ArtifactDetail({ path }: { path: string }) {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();
  const [version, setVersion] = useState<number>();

  // A new path is a different artifact; the version from the previous one
  // means nothing here and would request a version that may not exist.
  useEffect(() => setVersion(undefined), [path]);

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

  return (
    <>
      <Panel title={t("artifacts.history")} sub={<Mono truncate>{path}</Mono>} tight>
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
                  {v.version === latest && <span className="inline-badge">{t("artifacts.latest")}</span>}
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

      <Panel
        title={t("artifacts.content")}
        sub={content.data ? t("artifacts.viewing", { version: content.data.version }) : undefined}
      >
        {content.loading || stat.loading ? (
          <Loading />
        ) : content.error ? (
          <ErrorState error={content.error} onRetry={content.reload} />
        ) : !renderable ? (
          <EmptyState title={t("artifacts.binary")} body={t("artifacts.binaryBody")} />
        ) : (
          <Code>{content.data?.text}</Code>
        )}
      </Panel>
    </>
  );
}
