import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectPlans, progress } from "./ProjectPlans";

const now = new Date().toISOString();
const effort = {
  seconds: 0,
  tokens_in: 0,
  tokens_out: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
};

const BOARD = {
  phases: [
    {
      id: "ph-1",
      position: 1,
      status: "active",
      title: "Release 1.2",
      goal: "",
      counts: { open: 1, in_progress: 1, done: 2, blocked: 0, skipped: 3 },
      effort: {
        seconds: 5400,
        tokens_in: 1000,
        tokens_out: 200,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
      },
    },
  ],
  plans: [
    {
      id: "plan-1",
      phase_id: "ph-1",
      status: "active",
      title: "Release checklist",
      created_by: "pr-a",
      updated_at: now,
      counts: { open: 1, in_progress: 1, done: 2, blocked: 0, skipped: 3 },
      effort,
    },
  ],
  in_progress: [],
  blocked: [],
  presence: [
    {
      client_id: "c-1",
      client_type: "codex",
      principal_id: "pr-agent",
      you: false,
      project: "acme/work",
      plan_id: "plan-1",
      plan_title: "Release checklist",
      item_id: "it-1",
      item_text: "Smoke test",
      activity: "running the suite",
      since: now,
      last_seen: now,
      idle: false,
    },
  ],
  people: {
    "pr-agent": { kind: "service", name: "Codex at home" },
    "pr-a": { kind: "user", name: "Alice" },
  },
};

const PLAN = {
  id: "plan-1",
  phase_id: "ph-1",
  status: "active",
  version: 3,
  title: "Release checklist",
  body: "",
  created_by: "pr-a",
  created_at: now,
  updated_at: now,
  items: [
    {
      id: "it-1",
      plan_id: "plan-1",
      position: 1,
      kind: "check",
      status: "open",
      text: "Smoke test",
      updated_at: now,
      effort: {
        seconds: 1800,
        tokens_in: 0,
        tokens_out: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
      },
    },
  ],
};

type Call = { url: string; method: string; body?: string };

function stub(calls: Call[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
      if (url.endsWith("/v1/me")) {
        return json({
          principal_id: "pr-me",
          kind: "user",
          email: "me@example.test",
          display_name: "Me",
          has_password: true,
        });
      }
      if (url.endsWith("/v1/projects/acme/work")) {
        return json({
          id: "p-1",
          namespace: "acme",
          slug: "work",
          name: "Work",
          visibility: "private",
        });
      }
      if (url.endsWith("/plans/board")) return json(BOARD);
      if (url.endsWith("/plans/plan-1")) return json(PLAN);
      if (url.includes("/plans/items/") && method === "PATCH")
        return json({ ...PLAN.items[0], status: "in_progress" });
      if (url.endsWith("/plans/presence") && method === "POST") return json(BOARD.presence[0]);
      return new Response("{}", { status: 404 });
    }),
  );
}

function wrap() {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter initialEntries={["/p/acme/work/plans"]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route path="plans" element={<ProjectPlans />} />
                  </Route>
                </Routes>
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

describe("the plans board", () => {
  it("shows who is where, and progress that leaves skipped steps out", async () => {
    stub([]);
    render(wrap());

    const who = await waitFor(() => screen.getByText("Codex at home"));
    const row = who.closest("tr")!;
    expect(within(row).getByText("Release checklist → Smoke test")).toBeTruthy();
    expect(within(row).getByText("running the suite")).toBeTruthy();
    expect(within(row).getByText("agent")).toBeTruthy();

    // 2 done of the 4 that count; the 3 skipped are neither done nor owed.
    expect(screen.getAllByText("2 of 4 done").length).toBeGreaterThan(0);
    expect(screen.getByText("1.5 hr tracked")).toBeTruthy();
    expect(screen.getByText("1,200 tokens reported")).toBeTruthy();
  });

  it("“Work on this” starts the step and puts the reader on the board", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());

    fireEvent.click(await waitFor(() => screen.getByText("Open")));
    fireEvent.click(await waitFor(() => screen.getByText("Work on this")));

    await waitFor(() =>
      expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/plans/presence"))).toBe(
        true,
      ),
    );
    const patch = calls.find((c) => c.method === "PATCH")!;
    expect(patch.url).toMatch(/\/plans\/items\/it-1$/);
    expect(JSON.parse(patch.body!)).toEqual({ status: "in_progress" });
    const report = calls.find((c) => c.method === "POST" && c.url.endsWith("/plans/presence"))!;
    expect(JSON.parse(report.body!)).toEqual({ item_id: "it-1" });
    // The check is labelled as one.
    expect(screen.getAllByText("Check").length).toBeGreaterThan(0);
  });
});

describe("progress", () => {
  it("is done over everything except skipped", () => {
    expect(progress({ open: 1, in_progress: 0, done: 1, blocked: 0, skipped: 5 })).toEqual({
      done: 1,
      total: 2,
      percent: 50,
    });
    expect(progress({ open: 0, in_progress: 0, done: 0, blocked: 0, skipped: 2 }).percent).toBe(0);
  });
});
