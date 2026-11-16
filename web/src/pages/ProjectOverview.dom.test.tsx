import { render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectOverview } from "./ProjectOverview";

const EFFORT = {
  seconds: 0,
  tokens_in: 0,
  tokens_out: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  cache_write_1h_tokens: 0,
};
const COUNTS = { open: 1, in_progress: 0, done: 2, blocked: 1, skipped: 3 };
const BLOCKED_ITEM = {
  id: "it-9",
  plan_id: "plan-1",
  position: 3,
  kind: "task",
  status: "blocked",
  text: "Ollama unreachable from staging",
  updated_by: "pr-bob",
  updated_at: "2026-11-15T10:00:00Z",
  effort: EFFORT,
};
const BOARD = {
  phases: [
    {
      id: "ph-1",
      position: 0,
      status: "active",
      title: "Memories and search",
      goal: "",
      counts: COUNTS,
      effort: { ...EFFORT, seconds: 7200 },
    },
  ],
  plans: [
    {
      id: "plan-1",
      phase_id: "ph-1",
      status: "active",
      title: "Semantic search",
      created_by: "pr-bob",
      updated_at: "2026-11-15T10:00:00Z",
      counts: COUNTS,
      effort: EFFORT,
    },
  ],
  in_progress: [],
  blocked: [{ plan_title: "Semantic search", item: BLOCKED_ITEM }],
  presence: [],
  people: { "pr-bob": { kind: "user", name: "Bob" } },
};

const MEMORIES = [
  // Newer than the decision: a first page of every kind would be all notes.
  ...Array.from({ length: 50 }, (_, k) => ({
    id: `n-${k}`,
    kind: "note",
    title: `A passing note ${k}`,
    created_by: "pr-bob",
    created_at: "2026-11-15T09:00:00Z",
    tags: [],
  })),
  {
    id: "m-1",
    kind: "decision",
    title: "Vectors stay encrypted",
    created_by: "pr-bob",
    created_at: "2026-11-14T08:00:00Z",
    tags: [],
  },
];

/** Answers like the server; a path ending in one of `failing` answers 500. */
function stub(usageRows: unknown[] = [], failing: string[] = []) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
      const path = url.split("?")[0] ?? url;
      if (failing.some((f) => path.endsWith(f)))
        return json({ error: { code: "internal", message: "boom" } }, 500);
      if (path.endsWith("/v1/me"))
        return json({
          principal_id: "pr-me",
          kind: "user",
          display_name: "Me",
          has_password: true,
        });
      if (path.endsWith("/v1/projects/acme/work"))
        return json({
          id: "p-1",
          namespace: "acme",
          slug: "work",
          name: "Work",
          visibility: "private",
        });
      if (path.endsWith("/plans/board")) return json(BOARD);
      if (path.endsWith("/plans/overview"))
        return json({
          from: "2026-11-03",
          days: 14,
          items: [
            {
              project: "acme/work",
              name: "Work",
              role: "OWNER",
              counts: COUNTS,
              done_per_day: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1],
            },
          ],
        });
      if (path.endsWith("/usage"))
        return json({ from: "", to: "", group: [], rows: usageRows, total: {}, truncated: false });
      if (path.endsWith("/handoffs/current"))
        return json({ error: { code: "not_found", message: "none" } }, 404);
      if (path.endsWith("/handoffs/")) return json({ items: [] });
      if (path.endsWith("/ideas/"))
        return json({
          items: [
            {
              id: "idea-1",
              status: "proposed",
              title: "Stream the usage export",
              claimed_by: "pr-bob",
              proposed_at: "2026-11-16T08:00:00Z",
            },
          ],
          people: { "pr-bob": { kind: "user", name: "Bob" } },
        });
      if (path.endsWith("/memories/")) {
        const kind = new URL(url, "http://x").searchParams.get("kind");
        return json({
          items: MEMORIES.filter((m) => !kind || m.kind === kind).slice(0, 50),
          people: { "pr-bob": { kind: "user", name: "Bob" } },
        });
      }
      return json({}, 404);
    }),
  );
}

function wrap() {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter initialEntries={["/p/acme/work"]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route index element={<ProjectOverview />} />
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

async function findPanel(title: string) {
  return (await screen.findByRole("heading", { name: title })).closest("section") as HTMLElement;
}

function panel(title: string) {
  return screen.getByRole("heading", { name: title }).closest("section") as HTMLElement;
}

describe("a project's overview", () => {
  it("shows progress, phases, what waits on a person, and recent decisions", async () => {
    stub();
    render(wrap());

    // Skipped steps count in neither the share nor the total.
    expect(await screen.findByText("Done (2 of 4)")).toBeTruthy();
    expect(within(panel("Phases")).getByText("50% · 2 of 4 items")).toBeTruthy();

    const waiting = panel("Waiting on a person");
    expect(
      (await within(waiting).findByRole("link", { name: "Stream the usage export" })).getAttribute(
        "href",
      ),
    ).toBe("/p/acme/work/ideas");
    expect(
      within(waiting)
        .getByRole("link", { name: "Ollama unreachable from staging" })
        .getAttribute("href"),
    ).toBe("/p/acme/work/plans?plan=plan-1&item=it-9");

    const memories = panel("Recent decisions and gotchas");
    expect(await within(memories).findByText("Vectors stay encrypted")).toBeTruthy();
    expect(within(memories).queryByText(/A passing note/)).toBeNull();

    expect(
      await screen.findByRole("img", { name: /^Items finished per day: 0, 0, .*1, 1$/ }),
    ).toBeTruthy();
  });

  it("says time is optional rather than drawing zeros, and leaves branches out", async () => {
    stub([]);
    render(wrap());
    expect(await screen.findByText(/No time reported\./)).toBeTruthy();
    expect(screen.queryByRole("heading", { name: /Branches in flight/ })).toBeNull();
  });

  it("draws time per day and branches when a client reports usage", async () => {
    stub([{ day: "2026-11-16", branch: "feature/inbox", ticket_key: "CC-142", seconds: 3600 }]);
    render(wrap());
    const branches = (await screen.findByRole("heading", { name: /Branches in flight/ })).closest(
      "section",
    ) as HTMLElement;
    expect(within(branches).getByText("feature/inbox")).toBeTruthy();
    expect(within(branches).getByText("CC-142")).toBeTruthy();
  });

  it("shows a failed read as an error, not as nothing to show", async () => {
    stub([], ["/ideas/", "/usage"]);
    render(wrap());
    const waiting = await findPanel("Waiting on a person");
    expect(await within(waiting).findByText("Something went wrong")).toBeTruthy();
    expect(within(waiting).queryByText("Nothing waits on a person")).toBeNull();
    expect(within(waiting).getByRole("button", { name: "Try again" })).toBeTruthy();

    expect((await screen.findAllByText("Something went wrong")).length).toBeGreaterThan(1);
    expect(screen.queryByText(/No time reported\./)).toBeNull();
  });

  it("claims nothing about blocked steps when the board did not load", async () => {
    stub([], ["/plans/board"]);
    const fetchBefore = globalThis.fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = url.split("?")[0] ?? url;
        if (path.endsWith("/ideas/"))
          return new Response(JSON.stringify({ items: [], people: {} }), { status: 200 });
        return fetchBefore(url, init);
      }),
    );
    render(wrap());
    const waiting = await findPanel("Waiting on a person");
    expect(await within(waiting).findByText("No proposal to decide")).toBeTruthy();
    expect(within(waiting).queryByText("Nothing waits on a person")).toBeNull();
  });
});
