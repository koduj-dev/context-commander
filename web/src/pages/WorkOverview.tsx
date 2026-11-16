import { useT } from "../i18n";
import { useFormat } from "../app/prefs";
import type { Counts } from "../lib/api";
import { Bars, Progress } from "../ui";

/**
 * Pieces the home screen and a project's overview share: a progress bar with
 * its reading, and a column per day.
 *
 * Skipped items count in neither the done share nor the total: a step
 * someone decided not to do is not progress, and it is not work left either.
 */

/** Items that are not skipped. */
export function workTotal(c: Counts): number {
  return c.done + c.in_progress + c.blocked + c.open;
}

/** The progress bar with the share done and the counts written beside it. */
export function ProgressCell({ counts }: { counts: Counts }) {
  const t = useT();
  const f = useFormat();
  const total = workTotal(counts);
  return (
    <div className="stack-sm">
      <Progress
        done={counts.done}
        doing={counts.in_progress}
        blocked={counts.blocked}
        open={counts.open}
        label={
          total
            ? t("overview.progressAria", {
                done: counts.done,
                doing: counts.in_progress,
                blocked: counts.blocked,
                open: counts.open,
              })
            : t("overview.noPlan")
        }
      />
      <span className="sub-line">
        {total
          ? t("overview.progress", {
              percent: f.percent(counts.done / total),
              done: f.number(counts.done),
              total,
            })
          : t("overview.noPlan")}
      </span>
    </div>
  );
}

/** The UTC calendar days of a window that starts on `from`, oldest first. */
export function windowDays(from: string, days: number): string[] {
  const start = Date.parse(`${from}T00:00:00Z`);
  return Array.from({ length: days }, (_, k) =>
    new Date(start + k * 86_400_000).toISOString().slice(0, 10),
  );
}

/** Items finished per day, as columns. */
export function DoneBars({
  from,
  values,
  spark,
}: {
  from: string;
  values: number[];
  spark?: boolean;
}) {
  const t = useT();
  const f = useFormat();
  const days = windowDays(from, values.length);
  const titles = values.map((n, k) =>
    t("overview.doneDay", { day: f.calendarDate(days[k] ?? from), count: n }),
  );
  return (
    <Bars
      values={values}
      titles={titles}
      spark={spark}
      label={t("overview.doneAria", { values: values.map((n) => f.number(n)).join(", ") })}
    />
  );
}
