import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { I18nProvider } from "../i18n";
import { PrefsProvider } from "../app/prefs";
import type { UsageSummary } from "../lib/api";
import { barMeasure, lastDays, UsageTable } from "./Usage";

function wrap(children: ReactNode) {
  return (
    <I18nProvider>
      <PrefsProvider>
        <MemoryRouter>{children}</MemoryRouter>
      </PrefsProvider>
    </I18nProvider>
  );
}

const row = (over: Partial<UsageSummary["rows"][number]>) => ({
  seconds: 0,
  tokens_in: 0,
  tokens_out: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  cache_write_1h_tokens: 0,
  turns: 0,
  ...over,
});

describe("usage", () => {
  it("covers the last N UTC days, today included", () => {
    expect(lastDays(7, new Date("2026-11-13T23:30:00Z"))).toEqual({
      from: "2026-11-07",
      to: "2026-11-13",
    });
  });

  it("bars the cost when priced and the time otherwise", () => {
    const priced: UsageSummary = {
      from: "",
      to: "",
      group: [],
      rows: [row({ seconds: 10, cost: 2 })],
      total: row({}),
      currency: "USD",
      truncated: false,
    };
    expect(barMeasure(priced)(priced.rows[0]!)).toBe(2);
    expect(barMeasure({ ...priced, currency: undefined })(priced.rows[0]!)).toBe(10);
  });

  it("names each branch and says when one was not stated", () => {
    const s: UsageSummary = {
      from: "2026-11-01",
      to: "2026-11-13",
      group: ["branch"],
      rows: [row({ branch: "feature/export", seconds: 600 }), row({ branch: "", seconds: 60 })],
      total: row({ seconds: 660 }),
      truncated: false,
    };
    render(wrap(<UsageTable summary={s} group="branch" />));
    const feature = screen.getByText("feature/export").closest("tr")!;
    expect(within(feature).getAllByRole("cell").length).toBe(7); // no cost column without prices
    expect(screen.getByText("Not stated")).toBeTruthy();
  });

  it("says so when there is nothing to show, rather than an empty table", () => {
    const s: UsageSummary = {
      from: "",
      to: "",
      group: ["day"],
      rows: [],
      total: row({}),
      truncated: false,
    };
    render(wrap(<UsageTable summary={s} group="day" />));
    expect(screen.getByText("No usage in this range")).toBeTruthy();
  });
});
