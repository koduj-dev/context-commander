import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { useCrumbs } from "../app/crumbs";
import { api, type MyProject } from "../lib/api";
import { roleLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
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
  PageHead,
  Panel,
  Row,
  TextField,
} from "../ui";

/**
 * Every project this principal is a member of.
 *
 * `/me/projects` returns the whole set rather than a page — membership is
 * bounded by how many projects a person was added to, not by how much data
 * the system holds — so the filter below is local. It is a filter, not a
 * search: it narrows what is already on screen and makes no request.
 */
export function Projects() {
  const t = useT();
  useCrumbs("projects", () => ({ path: [], current: t("projects.title") }));
  const [filter, setFilter] = useState("");
  const [creating, setCreating] = useState(false);
  const { data, error, loading, reload } = useResource(() => api.myProjects(), []);

  const rows = useMemo(() => {
    const all = data ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return all;
    return all.filter((p) => path(p).toLowerCase().includes(q) || p.name.toLowerCase().includes(q));
  }, [data, filter]);

  return (
    <>
      <PageHead
        title={t("projects.title")}
        lead={t("projects.lead")}
        actions={
          <Btn kind="primary" onClick={() => setCreating((v) => !v)}>
            {t("projects.create")}
          </Btn>
        }
      />

      {creating && (
        <div className="mb">
          <CreateProject
            onCreated={() => {
              reload();
              setCreating(false);
            }}
          />
        </div>
      )}

      <Panel
        sub={t("projects.count", { count: rows.length })}
        title={t("projects.title")}
        right={
          <TextField
            label={t("projects.filter")}
            type="search"
            value={filter}
            onChange={setFilter}
            placeholder={t("projects.filter")}
          />
        }
        tight
      >
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : rows.length === 0 ? (
          data?.length === 0 ? (
            <EmptyState title={t("projects.empty")} body={t("projects.emptyBody")} />
          ) : (
            <EmptyState title={t("empty.noResults")} body={t("empty.noResultsBody")} />
          )
        ) : (
          <DataTable
            columns={[
              { key: "project", label: t("projects.column.project") },
              { key: "role", label: t("projects.column.role") },
            ]}
          >
            {rows.map((p) => (
              <tr key={path(p)}>
                <td>
                  <Cell
                    primary={<Link to={`/p/${p.namespace}/${p.slug}`}>{p.name}</Link>}
                    secondary={<Mono dim>{path(p)}</Mono>}
                  />
                </td>
                <td>
                  <Badge tone={p.role === "OWNER" ? "active" : "neutral"}>
                    {roleLabel(t, p.role)}
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

function path(p: MyProject) {
  return `${p.namespace}/${p.slug}`;
}

/**
 * Creating a project.
 *
 * The path is `namespace/slug` and it is the one field that cannot be changed
 * afterwards — it is how people refer to the project and what every CLI and
 * MCP call names. A personal namespace is bound to its owner, so a second one
 * under the same account is refused; that refusal reads as a conflict here
 * rather than as an unexplained failure.
 */
function CreateProject({ onCreated }: { onCreated: () => void }) {
  const t = useT();
  const [path, setPath] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [created, setCreated] = useState<string>();

  async function submit() {
    if (!path.trim() || !name.trim() || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const project = await api.createProject(path.trim(), name.trim());
      setCreated(`${project.namespace}/${project.slug}`);
      setPath("");
      setName("");
      onCreated();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("projects.create")}>
      <p className="sec lead-out">{t("projects.createLead")}</p>
      <Row gap="sm">
        <TextField
          label={t("projects.path")}
          value={path}
          onChange={setPath}
          placeholder={t("projects.pathHelp")}
          grow
        />
        <TextField
          label={t("projects.name")}
          value={name}
          onChange={setName}
          placeholder={t("projects.name")}
          grow
        />
        <Btn
          kind="primary"
          onClick={() => void submit()}
          disabled={busy || !path.trim() || !name.trim()}
        >
          {busy ? t("common.loading") : t("projects.create")}
        </Btn>
      </Row>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
      {created && (
        <div className="mt">
          <Note tone="info">{t("projects.created", { path: created })}</Note>
        </div>
      )}
    </Panel>
  );
}
