import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { api } from "../lib/api";
import { scopeLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
import {
  Badge,
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
  const { namespace, slug } = useProject();
  const { data, error, loading, reload } = useResource(
    () => api.effectiveSkills(namespace, slug),
    [namespace, slug],
  );

  const bindings = data ?? [];

  return (
    <Panel
      title={t("skills.title")}
      tight
      right={
        <Link className="btn btn-sm" to="/registry">
          {t("skills.browse")}
        </Link>
      }
    >
      <p className="sec lead-out">{t("skills.lead")}</p>

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
          ]}
        >
          {bindings.map((b) => (
            <tr key={b.id}>
              <td>
                <Mono truncate>{b.skill_id}</Mono>
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
                {b.overridable ? null : <Badge tone="warn">locked</Badge>}
              </td>
            </tr>
          ))}
        </DataTable>
      )}
    </Panel>
  );
}
