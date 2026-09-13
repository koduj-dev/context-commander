import { useState } from "react";
import { useT } from "../i18n";
import { api } from "../lib/api";
import { scopeLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
  Btn,
  Code,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  PageHead,
  Panel,
  Row,
  Scope,
  SelectField,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * The gates that can refuse an operation here.
 *
 * Composed the same way skills are — installation, organization and project
 * bindings together — so the scope rail is the useful column: a gate bound at
 * the installation is one this project cannot simply switch off, and the
 * unbind control is absent on those rows for the same reason.
 */

// What the server actually registered. A binding naming a gate with no
// registered provider fails the whole operation rather than being ignored
// (internal/gates.Engine), so offering anything else here would be offering
// a way to break every write in the project.
const GATES = ["secret-scanner"];
const HOOKS = ["artifact.before_write"];

export function ProjectPolicies() {
  const t = useT();
  const { project, namespace, slug } = useProject();
  const [binding, setBinding] = useState(false);

  const { data, error, loading, reload } = useResource(
    () => api.gateBindings(namespace, slug),
    [namespace, slug],
  );

  const bindings = data ?? [];

  async function unbind(id: string, gate: string, hook: string) {
    if (!window.confirm(t("gates.confirmUnbind", { gate, hook }))) return;
    await api.unbindGate(namespace, slug, id);
    reload();
  }

  return (
    <>
      <PageHead
        title={t("gates.title")}
        lead={t("gates.lead")}
        actions={
          <Btn kind="primary" onClick={() => setBinding((v) => !v)}>
            {t("gates.bind")}
          </Btn>
        }
      />

      {binding && (
        <div className="mb">
          <BindGate
            onBound={() => {
              reload();
              setBinding(false);
            }}
            onCancel={() => setBinding(false)}
          />
        </div>
      )}

      <Panel title={t("gates.title")} tight>
        {loading ? (
          <Loading />
        ) : error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : bindings.length === 0 ? (
          <EmptyState title={t("gates.empty")} body={t("gates.emptyBody")} />
        ) : (
          <DataTable
            columns={[
              { key: "gate", label: t("gates.column.gate") },
              { key: "hook", label: t("gates.column.hook") },
              { key: "scope", label: t("gates.column.scope") },
              { key: "state", label: t("gates.column.enabled"), align: "r" },
              { key: "unbind", align: "r" },
            ]}
          >
            {bindings.map((b) => (
              <tr key={b.id}>
                <td>
                  <Mono>{b.gate_name}</Mono>
                  {b.config !== null && b.config !== undefined && (
                    <div className="sub-line">
                      <Code>{JSON.stringify(b.config)}</Code>
                    </div>
                  )}
                </td>
                <td>
                  <Mono dim>{b.hook}</Mono>
                </td>
                <td>
                  <Scope kind={b.scope}>{scopeLabel(t, b.scope)}</Scope>
                </td>
                <td className="r">
                  <Badge tone={b.enabled ? "ok" : "neutral"}>
                    {b.enabled ? t("gates.enabled") : t("gates.disabled")}
                  </Badge>
                </td>
                <td className="r">
                  {b.scope === "project" && b.scope_ref === project.id ? (
                    <Btn kind="danger" small onClick={() => void unbind(b.id, b.gate_name, b.hook)}>
                      {t("gates.unbind")}
                    </Btn>
                  ) : (
                    <span className="dim">{t("gates.inherited")}</span>
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

function BindGate({ onBound, onCancel }: { onBound: () => void; onCancel: () => void }) {
  const t = useT();
  const { project, namespace, slug } = useProject();
  const [gate, setGate] = useState(GATES[0]!);
  const [hook, setHook] = useState(HOOKS[0]!);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  async function submit() {
    if (busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await api.bindGate(namespace, slug, {
        scope: "project",
        scope_ref: project.id,
        gate_name: gate,
        hook,
      });
      onBound();
    } catch (err) {
      setFailure(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel title={t("gates.bind")} footer={<span className="dim">{t("gates.bindLead")}</span>}>
      <Row gap="sm">
        <SelectField
          label={t("gates.gateName")}
          value={gate}
          onChange={setGate}
          options={GATES.map((g) => ({ value: g, label: g }))}
        />
        <SelectField
          label={t("gates.hook")}
          value={hook}
          onChange={setHook}
          options={HOOKS.map((h) => ({ value: h, label: h }))}
        />
        <span className="spacer" />
        <Btn onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </Btn>
        <Btn kind="primary" onClick={() => void submit()} disabled={busy}>
          {busy ? t("common.loading") : t("gates.bind")}
        </Btn>
      </Row>
      {failure !== undefined && (
        <div className="mt">
          <ErrorState error={failure} />
        </div>
      )}
    </Panel>
  );
}
