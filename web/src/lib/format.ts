/**
 * Formatting values for a reader.
 *
 * Nothing here invents a format. Everything goes through `Intl`, which knows
 * that a Czech reader writes 2,4 GB and 13. 9. 2026 while an American writes
 * 2.4 GB and 9/13/2026 — and knows it for every locale, not only the three
 * this UI is translated into. A hand-rolled `toFixed(1) + " GB"` is a choice
 * made on the reader's behalf by someone who did not know who they were.
 *
 * The server never sends a formatted string (docs/CURRENT-ARCHITECTURE.md):
 * it sends RFC3339 timestamps and integer byte counts, and this is where they
 * become readable. That split is also what keeps the API parseable by a
 * model — its reader is not a person and does not need it pretty.
 */

export type UnitSystem = "decimal" | "binary";

/** An absolute date and time, in the reader's locale and zone. */
export function dateTime(iso: string, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  }).format(new Date(iso));
}

export function dateOnly(iso: string, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone }).format(new Date(iso));
}

export function timeOnly(iso: string, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, { timeStyle: "medium", timeZone }).format(new Date(iso));
}

/**
 * "2 minutes ago", "vor 2 Minuten", "před 2 minutami".
 *
 * Intl.RelativeTimeFormat handles the grammar, including languages that
 * inflect the unit by the number. Past a week it gives up and returns the
 * date, because "47 days ago" is a worse answer than the date itself.
 */
export function relativeTime(iso: string, locale: string, timeZone?: string): string {
  const then = new Date(iso).getTime();
  const seconds = Math.round((then - Date.now()) / 1000);
  const abs = Math.abs(seconds);

  if (abs > 7 * 86400) return dateOnly(iso, locale, timeZone);

  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (abs < 60) return rtf.format(seconds, "second");
  if (abs < 3600) return rtf.format(Math.round(seconds / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(seconds / 3600), "hour");
  return rtf.format(Math.round(seconds / 86400), "day");
}

export function number(value: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(value);
}

/**
 * A length of time, in the one unit that reads best for its size: "45 min",
 * "3.5 h", "2 days". Not a breakdown ("3 h 27 min") — every figure this
 * formats is a median or a percentile, and precision past the first unit
 * would claim an accuracy the statistic does not have.
 *
 * `Intl.NumberFormat`'s unit style rather than `Intl.DurationFormat`, which
 * not every supported browser has yet.
 */
export function duration(seconds: number, locale: string): string {
  const unit = (value: number, u: string, digits = 0) =>
    new Intl.NumberFormat(locale, {
      style: "unit",
      unit: u,
      unitDisplay: "short",
      maximumFractionDigits: digits,
    }).format(value);
  const abs = Math.abs(seconds);
  if (abs < 90) return unit(seconds, "second");
  if (abs < 90 * 60) return unit(seconds / 60, "minute");
  if (abs < 48 * 3600) return unit(seconds / 3600, "hour", 1);
  return unit(seconds / 86400, "day", 1);
}

/**
 * A calendar date the server already fixed — a UTC week's Monday — written
 * in the reader's preferred order but never shifted into their zone. Shifting
 * "2026-10-26" west of Greenwich would print the Sunday before.
 */
export function calendarDate(day: string, locale: string, order: DateOrder): string {
  return date(`${day}T00:00:00Z`, locale, order, "UTC");
}

const DECIMAL = ["B", "kB", "MB", "GB", "TB", "PB"];
const BINARY = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

/**
 * A byte count, in whichever unit system the reader prefers.
 *
 * 2,400,000,000 bytes is 2.4 GB and 2.24 GiB. Both are correct and they are
 * the same value — which one appears is the reader's call, which is why the
 * server sends the integer and never the sentence.
 */
export function bytes(
  value: number,
  locale: string,
  system: UnitSystem = "decimal",
  precision = 2,
): string {
  const step = system === "binary" ? 1024 : 1000;
  const units = system === "binary" ? BINARY : DECIMAL;

  let n = value;
  let i = 0;
  while (n >= step && i < units.length - 1) {
    n /= step;
    i++;
  }
  const digits = i === 0 ? 0 : precision;
  const formatted = new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(n);
  return `${formatted} ${units[i]}`;
}

/**
 * The order a reader writes a numeric date in.
 *
 * "locale" is what their language does and is the right default — an English
 * reader is not surprised by 13 Sep 2026. The three explicit orders exist
 * because the default is a convention, not a preference: people who read logs
 * all day often want YYYY-MM-DD regardless of what their language does, and
 * 03-09-2026 means two different days depending on who is reading it, which
 * is precisely the ambiguity an explicit choice removes.
 */
export type DateOrder = "locale" | "dmy" | "mdy" | "ymd";

/** "locale" follows the language; the other two override it. */
export type HourCycle = "locale" | "h12" | "h24";

/**
 * Pulls year/month/day out of an instant as seen in `timeZone`.
 *
 * The locale here is not a display choice — nothing from this formatter is
 * shown. It is pinned to a Gregorian calendar because several locales default
 * to another one (ar-SA to the Islamic calendar, fa-IR to the Persian), and
 * the numbers this function returns are then assembled into a fixed order
 * that only makes sense in the Gregorian one.
 */
function gregorianParts(d: Date, timeZone?: string) {
  const parts = new Intl.DateTimeFormat("en-US-u-ca-gregory", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    timeZone,
  }).formatToParts(d);
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
  return { year: get("year"), month: get("month"), day: get("day") };
}

/** A date, in the reader's order and zone. */
export function date(iso: string, locale: string, order: DateOrder, timeZone?: string): string {
  const d = new Date(iso);
  if (order === "locale") return dateOnly(iso, locale, timeZone);
  const { year, month, day } = gregorianParts(d, timeZone);
  // Dash-separated on purpose: these three orders are chosen by people who
  // want the date unambiguous, and a separator that varies by locale would
  // put the ambiguity back.
  if (order === "ymd") return `${year}-${month}-${day}`;
  if (order === "mdy") return `${month}-${day}-${year}`;
  return `${day}-${month}-${year}`;
}

/** A time of day, honouring an explicit 12/24-hour choice. */
export function time(iso: string, locale: string, cycle: HourCycle, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    timeZone,
    ...(cycle === "locale" ? {} : { hour12: cycle === "h12" }),
  }).format(new Date(iso));
}

/**
 * Date and time together.
 *
 * Assembled from the two parts rather than from `dateStyle`/`timeStyle`,
 * because the reader may have overridden either of them and a single `Intl`
 * call cannot honour both overrides at once.
 */
export function stamp(
  iso: string,
  locale: string,
  order: DateOrder,
  cycle: HourCycle,
  timeZone?: string,
): string {
  if (order === "locale" && cycle === "locale") return dateTime(iso, locale, timeZone);
  return `${date(iso, locale, order, timeZone)} ${time(iso, locale, cycle, timeZone)}`;
}

/** The same instant in UTC, for readers who keep a second clock. */
export function utcStamp(iso: string): string {
  return `${new Date(iso).toISOString().slice(0, 16).replace("T", " ")} UTC`;
}

/** The reader's own zone, for showing what "local" currently means. */
export function localTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
