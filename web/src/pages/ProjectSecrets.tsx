import { useState } from "react";
import { useT } from "../i18n";
import { api } from "../lib/api";
import { useList } from "../lib/resource";
import {
  Btn,
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Note,
  Pager,
  Panel,
  Row,
  TextField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * Secrets, as far as this interface can see them.
 *
 * Names and references only. There is no read endpoint for a value and this
 * screen does not pretend otherwise: the value goes in once, is wrapped under
 * the project key, and what comes back out is a reference a gate resolves.
 * The only way to change one is to delete it and add it again, which the
 * panel says out loud rather than leaving people to discover.
 */
export function ProjectSecrets() {
  const t = useT();
  const { namespace, slug } = useProject();

  const list = useList((cursor) => api.secrets(namespace, slug, cursor), [namespace, slug]);

  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function add() {
    if (!name.trim() || !value || busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.createSecret(namespace, slug, name.trim(), value);
      setName("");
      setValue("");
      list.reload();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  async function remove(secretName: string) {
    // A confirm, because this is not undoable and the damage shows up
    // somewhere else — in whatever gate was resolving the reference.
    if (!window.confirm(t("secrets.confirmDelete", { name: secretName }))) return;
    setFailure(undefined);
    try {
      await api.deleteSecret(namespace, slug, secretName);
      list.reload();
    } catch (err) {
      setFailure(err);
    }
  }

  return (
    <>
      <Panel
        title={t("secrets.title")}
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
        <p className="sec lead-out">{t("secrets.lead")}</p>

        {list.loading ? (
          <Loading />
        ) : list.error ? (
          <ErrorState error={list.error} onRetry={list.reload} />
        ) : list.items.length === 0 ? (
          <EmptyState title={t("secrets.empty")} body={t("secrets.emptyBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "name", label: t("secrets.column.name") },
              { key: "actions", align: "r" },
            ]}
          >
            {list.items.map((s) => (
              <tr key={s.name}>
                <td>
                  <Cell primary={<Mono>{s.name}</Mono>} secondary={<Mono dim>{s.reference}</Mono>} />
                </td>
                <td className="r">
                  <Btn kind="danger" small onClick={() => void remove(s.name)}>
                    {t("secrets.delete")}
                  </Btn>
                </td>
              </tr>
            ))}
          </DataTable>
        )}
      </Panel>

      <div className="mt">
        <Panel title={t("secrets.add")}>
          <Row gap="sm">
            <TextField
              label={t("secrets.column.name")}
              value={name}
              onChange={setName}
              placeholder={t("secrets.column.name")}
            />
            <TextField
              label={t("secrets.value")}
              type="password"
              value={value}
              onChange={setValue}
              placeholder={t("secrets.value")}
              grow
            />
            <Btn kind="primary" onClick={() => void add()} disabled={busy || !name.trim() || !value}>
              {busy ? t("common.loading") : t("secrets.add")}
            </Btn>
          </Row>
          {failure !== undefined && (
            <div className="mt">
              <ErrorState error={failure} />
            </div>
          )}
          <div className="mt">
            <Note tone="warn">{t("secrets.neverShown")}</Note>
          </div>
        </Panel>
      </div>
    </>
  );
}
