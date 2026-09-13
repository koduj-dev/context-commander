import { useT } from "../i18n";
import { api } from "../lib/api";
import { scopeLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
  Code,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Mono,
  Panel,
  Scope,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * The gates that can refuse an operation here.
 *
 * Composed the same way skills are — installation, organization and project
 * bindings together — so the scope rail is the useful column: a gate bound at
 * the installation is one this project cannot simply switch off.
 *
 * Read-only. Binding and unbinding exist in the API but are the kind of
 * change that wants a review trail rather than a toggle, and nothing in this
 * interface could show what a config change would do before it took effect.
 */
export function ProjectPolicies() {
  const t = useT();
  const { namespace, slug } = useProject();
  const { data, error, loading, reload } = useResource(
    () => api.gateBindings(namespace, slug),
    [namespace, slug],
  );

  const bindings = data ?? [];

  return (
    <Panel title={t("gates.title")} tight>
      <p className="sec lead-out">{t("gates.lead")}</p>

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
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}
