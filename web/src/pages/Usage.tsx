import { useState } from "react";
import { Link } from "react-router-dom";
import { useT } from "../i18n";
import type { MessageKey } from "../i18n/en";
import { useFormat } from "../app/prefs";
import { useProject } from "./ProjectLayout";
import { api, type UsageGroup, type UsageRow, type UsageSummary } from "../lib/api";
import { useResource } from "../lib/resource";
import {
  Cell,
  DataTable,
  EmptyState,
  ErrorState,
  Loading,
  Meter,
  Mono,
  Note,
  PageHead,
  Panel,
  Row,
  Seg,
  Stack,
} from "../ui";

/**
 * The usage ledger (docs/adr/0027) as a table with a bar in each row.
 *
 * A table rather than a chart: the bar answers "which day, person or branch
 * was biggest" at a glance, the numbers beside it answer "how big", and the
 * table is already the accessible view a chart would need beside it. The bar
 * follows the cost estimate when the operator configured prices and time
 * otherwise, because those are the two measures a team compares. Token counts
 * are listed, not barred: cache reads dwarf everything else by orders of
 * magnitude and would make every other bar invisible.
 */

/** The UTC day `days - 1` days before today, and today, as YYYY-MM-DD. */
export function lastDays(days: number, now = new Date()): { from: string; to: string } {
  const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  const from = new Date(to.getTime() - (days - 1) * 86_400_000);
  const day = (d: Date) => d.toISOString().slice(0, 10);
  return { from: day(from), to: day(to) };
}

/** What the bar measures for a summary: cost when priced, else time. */
export function barMeasure(s: UsageSummary): (r: UsageRow) => number {
  const priced = s.currency !== undefined && s.rows.some((r) => r.cost !== undefined);
  return priced ? (r) => r.cost ?? 0 : (r) => r.seconds;
}

export const GROUP_LABEL: Record<UsageGroup, MessageKey> = {
  project: "usage.group.project",
  day: "usage.group.day",
  principal: "usage.group.principal",
  model: "usage.group.model",
  branch: "usage.group.branch",
  ticket: "usage.group.ticket",
};

function RowLabel({ row, group }: { row: UsageRow; group: UsageGroup }) {
  const t = useT();
  const f = useFormat();
  const unstated = <span className="dim">{t("usage.notStated")}</span>;
  switch (group) {
    case "day":
      return <span className="sec">{row.day ? f.calendarDate(row.day) : unstated}</span>;
    case "principal":
      return (
        <Cell
          primary={row.principal_name || <Mono dim>{row.principal_id ?? ""}</Mono>}
          secondary={
            <span className="dim">
              {row.principal_kind === "service" ? t("usage.agent") : t("usage.person")}
            </span>
          }
        />
      );
    case "project":
      return row.project_path ? (
        <Link to={`/p/${row.project_path}/usage`}>
          <Mono>{row.project_path}</Mono>
        </Link>
      ) : (
        unstated
      );
    case "model":
      return row.model ? <Mono>{row.model}</Mono> : unstated;
    case "branch":
      return row.branch ? <Mono>{row.branch}</Mono> : unstated;
    case "ticket":
      return row.ticket_key ? <Mono>{row.ticket_key}</Mono> : unstated;
  }
}

export function UsageTable({
  summary,
  group,
  limit,
}: {
  summary: UsageSummary;
  group: UsageGroup;
  limit?: number;
}) {
  const t = useT();
  const f = useFormat();
  const measure = barMeasure(summary);
  const rows = limit === undefined ? summary.rows : summary.rows.slice(0, limit);
  const max = Math.max(0, ...summary.rows.map(measure));
  const priced = summary.currency !== undefined;
  const money = (n?: number) =>
    n === undefined || !summary.currency ? (
      <span className="dim">—</span>
    ) : (
      <span className="num">{f.money(n, summary.currency)}</span>
    );

  if (summary.rows.length === 0) {
    return <EmptyState title={t("usage.none")} body={t("usage.noneBody")} />;
  }
  return (
    <DataTable
      columns={[
        { key: "label", label: t(GROUP_LABEL[group]) },
        { key: "bar", label: priced ? t("usage.column.cost") : t("usage.column.time") },
        { key: "time", label: t("usage.column.time"), align: "r" },
        { key: "in", label: t("usage.column.in"), align: "r" },
        { key: "out", label: t("usage.column.out"), align: "r" },
        { key: "cr", label: t("usage.column.cacheRead"), align: "r" },
        { key: "cw", label: t("usage.column.cacheWrite"), align: "r" },
        ...(priced ? [{ key: "cost", label: t("usage.column.cost"), align: "r" as const }] : []),
      ]}
    >
      {rows.map((r, i) => (
        <tr key={i}>
          <td>
            <RowLabel row={r} group={group} />
          </td>
          <td>
            <Meter percent={max > 0 ? Math.round((measure(r) / max) * 100) : 0} />
          </td>
          <td className="r">
            <span className="num">{f.duration(r.seconds)}</span>
          </td>
          <td className="r">
            <span className="num">{f.number(r.tokens_in)}</span>
          </td>
          <td className="r">
            <span className="num">{f.number(r.tokens_out)}</span>
          </td>
          <td className="r">
            <span className="num">{f.number(r.cache_read_tokens)}</span>
          </td>
          <td className="r">
            <span className="num">{f.number(r.cache_write_tokens)}</span>
          </td>
          {priced && <td className="r">{money(r.cost)}</td>}
        </tr>
      ))}
    </DataTable>
  );
}

/** The footer under a usage table: the total, and what the estimate leaves out. */
export function UsageFooter({ summary }: { summary: UsageSummary }) {
  const t = useT();
  const f = useFormat();
  const total = summary.total;
  return (
    <span className="dim">
      {t("usage.total", {
        time: f.duration(total.seconds),
        tokens: f.number(
          total.tokens_in + total.tokens_out + total.cache_read_tokens + total.cache_write_tokens,
        ),
      })}
      {summary.currency && total.cost !== undefined && (
        <> {t("usage.totalCost", { cost: f.money(total.cost, summary.currency) })}</>
      )}
      {summary.unpriced && summary.unpriced.length > 0 && <> {t("usage.unpriced")}</>}
      {summary.truncated && <> {t("usage.truncated")}</>}
    </span>
  );
}

const RANGES = [7, 30, 90] as const;
type Range = `${(typeof RANGES)[number]}`;
const GROUPS: UsageGroup[] = ["day", "principal", "branch", "ticket", "model"];

/** A project's Usage tab. */
export function ProjectUsage() {
  const t = useT();
  const { namespace, slug } = useProject();
  const [range, setRange] = useState<Range>("30");
  const [group, setGroup] = useState<UsageGroup>("day");
  const { from, to } = lastDays(Number(range));
  const usage = useResource(
    () => api.projectUsage(namespace, slug, { from, to, group: [group] }),
    [namespace, slug, from, to, group],
  );

  return (
    <>
      <PageHead title={t("usage.title")} lead={t("usage.lead")} />
      <Stack>
        <Row gap="sm">
          <Seg
            label={t("usage.range")}
            value={range}
            onChange={setRange}
            options={RANGES.map((d) => ({
              value: `${d}` as Range,
              label: t("usage.lastDays", { count: d }),
            }))}
          />
          <Seg
            label={t("usage.groupBy")}
            value={group}
            onChange={setGroup}
            options={GROUPS.map((g) => ({ value: g, label: t(GROUP_LABEL[g]) }))}
          />
        </Row>
        <Panel
          title={t("usage.panel", { from, to })}
          tight
          footer={usage.data && <UsageFooter summary={usage.data} />}
        >
          {usage.loading ? (
            <Loading />
          ) : usage.error || !usage.data ? (
            <ErrorState error={usage.error} onRetry={usage.reload} />
          ) : (
            <UsageTable summary={usage.data} group={group} />
          )}
        </Panel>
        <Note>{t("usage.howCounted")}</Note>
      </Stack>
    </>
  );
}
