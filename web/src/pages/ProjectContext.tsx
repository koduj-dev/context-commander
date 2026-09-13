import { useState } from "react";
import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import { api } from "../lib/api";
import { roleLabel, scopeLabel } from "../lib/labels";
import { useResource } from "../lib/resource";
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
  Kpi,
  Loading,
  Meter,
  Mono,
  Note,
  PageHead,
  Panel,
  Scope,
  SelectField,
  Stack,
} from "../ui";
import { useProject } from "./ProjectLayout";

/**
 * What an agent actually receives.
 *
 * This is the one screen that answers "why is the agent behaving like that"
 * without reading a transcript: it renders the same assembled pack MCP
 * returns, including the budget report — which names what did not fit rather
 * than silently dropping it.
 *
 * The controls re-assemble the pack rather than filtering what is shown. A
 * filter would answer "what would I see with a smaller budget" with a guess;
 * asking the server answers it with the truth, including which items it
 * decides to drop first.
 */
const BUDGETS = [
  { value: "", label: "" },
  { value: "16384", label: "16 kB" },
  { value: "65536", label: "64 kB" },
  { value: "262144", label: "256 kB" },
];

export function ProjectContext() {
  const t = useT();
  const f = useFormat();
  const { namespace, slug } = useProject();

  const [budget, setBudget] = useState("");
  const [applied, setApplied] = useState("");

  const pack = useResource(
    () => api.contextPack(namespace, slug, { budget: applied ? Number(applied) : undefined }),
    [namespace, slug, applied],
  );

  if (pack.loading) return <Loading />;
  if (pack.error || !pack.data) return <ErrorState error={pack.error} onRetry={pack.reload} />;

  const p = pack.data;
  const used = p.budget.used_bytes;
  const limit = p.budget.limit_bytes;
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;

  return (
    <>
      <PageHead
        title={t("context.title")}
        lead={t("context.lead")}
        actions={
          <>
            <SelectField
              label={t("context.budgetLimit")}
              value={budget}
              onChange={setBudget}
              options={BUDGETS.map((b) => ({
                value: b.value,
                label: b.label || t("common.none"),
              }))}
            />
            <Btn onClick={() => setApplied(budget)}>{t("context.apply")}</Btn>
          </>
        }
      />

      <Grid cols="2-1">
        <Stack>
          <Panel title={t("handoff.current")}>
            {p.handoff ? (
              <>
                <p className="sec lead-out">{p.handoff.summary}</p>
                {p.handoff.completed_work && (
                  <>
                    <div className="label-caps">{t("handoff.completed")}</div>
                    <p className="sec">{p.handoff.completed_work}</p>
                  </>
                )}
                {p.handoff.next_actions && (
                  <>
                    <div className="label-caps">{t("handoff.next")}</div>
                    <p className="sec">{p.handoff.next_actions}</p>
                  </>
                )}
              </>
            ) : (
              <EmptyState title={t("context.noHandoff")} body={t("context.noHandoffBody")} />
            )}
          </Panel>

          <Panel
            title={t("skills.title")}
            sub={t("context.skills", { count: p.skills.length })}
            tight
          >
            <DataTable
              columns={[
                { key: "name", label: t("skills.column.skill") },
                { key: "scope", label: t("registry.livesAt") },
                { key: "trust", label: t("skills.column.trust"), align: "r" },
              ]}
            >
              {p.skills.map((s) => (
                <tr key={s.binding_id}>
                  <td>
                    {s.name}
                    <div className="sub-line">
                      <Mono dim>v{s.version}</Mono>
                    </div>
                  </td>
                  <td>
                    <Scope kind={s.scope}>{scopeLabel(t, s.scope)}</Scope>
                  </td>
                  <td className="r">
                    <Badge tone={s.trust_status === "signed" ? "ok" : "warn"}>
                      {s.trust_status}
                    </Badge>
                  </td>
                </tr>
              ))}
            </DataTable>
          </Panel>

          <Panel
            title={t("artifacts.title")}
            sub={t("context.artifacts", { count: p.artifacts.length })}
            tight
          >
            <DataTable
              columns={[
                { key: "path", label: t("artifacts.column.path") },
                { key: "v", label: t("artifacts.column.version"), align: "r" },
                { key: "size", label: t("artifacts.column.size"), align: "r" },
              ]}
            >
              {p.artifacts.map((a) => (
                <tr key={a.logical_path}>
                  <td>
                    <Mono>{a.logical_path}</Mono>
                  </td>
                  <td className="r">
                    <span className="num">v{a.version}</span>
                  </td>
                  <td className="r">
                    <span className="num">{f.bytes(a.size_bytes)}</span>
                  </td>
                </tr>
              ))}
            </DataTable>
          </Panel>
        </Stack>

        <Stack top>
          <Panel
            title={t("context.budget")}
            footer={<span className="dim">{t("context.budgetLead")}</span>}
          >
            <Kpi
              value={f.bytes(used)}
              label={t("context.budgetUsed", { used: f.bytes(used), limit: f.bytes(limit) })}
              tone={percent > 85 ? "warn" : undefined}
            />
            <div className="mt-sm">
              <Meter percent={percent} tone={percent > 85 ? "warn" : undefined} />
            </div>
          </Panel>

          <Panel title={t("context.truncated")} tight>
            {p.budget.truncated.length === 0 ? (
              <div className="body">
                <EmptyState
                  title={t("context.nothingTruncated")}
                  body={t("context.nothingTruncatedBody")}
                />
              </div>
            ) : (
              <DataTable
                columns={[
                  { key: "kind", label: t("context.column.what") },
                  { key: "ref", label: t("context.column.ref") },
                  { key: "why", label: t("context.column.why") },
                ]}
              >
                {p.budget.truncated.map((n, i) => (
                  <tr key={`${n.kind}:${n.ref}:${i}`}>
                    <td>
                      <Badge tone="warn">{n.kind}</Badge>
                    </td>
                    <td>
                      <Mono truncate>{n.ref}</Mono>
                      {n.size_bytes !== undefined && (
                        <div className="sub-line">
                          <span className="dim">{f.bytes(n.size_bytes)}</span>
                        </div>
                      )}
                    </td>
                    <td>
                      <span className="dim">{n.reason}</span>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>

          <Panel title={t("context.included")}>
            <Defs>
              <Def term={t("context.role")}>
                <Badge tone="active">{roleLabel(t, p.role)}</Badge>
              </Def>
              <Def term={t("context.capabilities")}>
                {p.capabilities.map((c) => (
                  <span key={c} className="inline-badge">
                    {c}
                  </span>
                ))}
              </Def>
              <Def term={t("context.assembled")}>
                <span className="dim">{f.stamp(p.assembled_at)}</span>
              </Def>
            </Defs>
          </Panel>

          <Panel title={t("context.recentEvents")} tight>
            {p.events.recent.length === 0 ? (
              <div className="body">
                <EmptyState title={t("events.empty")} />
              </div>
            ) : (
              <DataTable columns={[{ key: "e" }]}>
                {p.events.recent.map((e, i) => (
                  <tr key={i}>
                    <td>
                      <Mono>{e.type}</Mono>
                      <div className="sub-line">
                        <Mono dim truncate>
                          {e.resource_type}
                        </Mono>
                      </div>
                    </td>
                  </tr>
                ))}
              </DataTable>
            )}
          </Panel>

          {p.events.next_cursor && (
            <Panel title={t("events.title")}>
              {/* The cursor is what turns a snapshot into something an agent
                  can stay current with, so it is shown rather than hidden. */}
              <Code>{p.events.next_cursor}</Code>
            </Panel>
          )}
        </Stack>
      </Grid>

      {p.budget.truncated.length > 0 && (
        <div className="mt">
          <Note tone="warn">{t("context.budgetLead")}</Note>
        </div>
      )}
    </>
  );
}
