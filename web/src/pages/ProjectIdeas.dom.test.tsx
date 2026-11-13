import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectIdeas } from "./ProjectIdeas";

const now = new Date().toISOString();
const later = new Date(Date.now() + 3_600_000).toISOString();

const base = {
  position: 1,
  version: 1,
  created_by: "pr-a",
  claimed_by_you: false,
  claim_lapsed: false,
  created_at: now,
  updated_at: now,
};

const PROPOSED = {
  ...base,
  id: "idea-p",
  status: "proposed",
  priority: "high",
  title: "Retry failed webhooks",
  claimed_by: "pr-agent",
  proposed_at: now,
  resulting_plan_id: "plan-9",
};
const CLAIMED = {
  ...base,
  id: "idea-c",
  status: "claimed",
  priority: "normal",
  title: "Speed up the export",
  claimed_by: "pr-agent",
  claim_expires_at: later,
};
const OPEN = [
  { ...base, id: "idea-1", status: "open", priority: "normal", title: "First in line" },
  { ...base, id: "idea-2", status: "open", priority: "normal", title: "Second", position: 2 },
];
const PEOPLE = {
  "pr-a": { kind: "user", name: "Alice" },
  "pr-agent": { kind: "service", name: "Planner" },
};

type Call = { url: string; method: string; body?: string };

function stub(calls: Call[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
      if (url.endsWith("/v1/me"))
        return json({
          principal_id: "pr-me",
          kind: "user",
          email: "me@example.test",
          display_name: "Me",
        });
      if (url.endsWith("/v1/projects/acme/work"))
        return json({
          id: "p-1",
          namespace: "acme",
          slug: "work",
          name: "Work",
          visibility: "private",
        });
      if (url.includes("/ideas/?")) {
        const q = new URLSearchParams(url.split("?")[1]).getAll("status");
        if (q.includes("proposed")) return json({ items: [PROPOSED, CLAIMED], people: PEOPLE });
        if (q.includes("open")) return json({ items: OPEN, people: PEOPLE });
        return json({ items: [], people: {} });
      }
      if (url.endsWith("/ideas/idea-p"))
        return json({
          ...PROPOSED,
          body: "They are dropped today",
          proposal: "## Proposal\n<script>alert(1)</script> Add an outbox.",
          people: PEOPLE,
        });
      if (url.endsWith("/ideas/idea-p/accept") && method === "POST")
        return json({ ...PROPOSED, status: "accepted", people: PEOPLE });
      if (url.endsWith("/ideas/idea-2") && method === "PATCH")
        return json({ ...OPEN[1], people: PEOPLE });
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
            <MemoryRouter initialEntries={["/p/acme/work/ideas"]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route path="ideas" element={<ProjectIdeas />} />
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

describe("the idea stack", () => {
  it("shows the proposal as text, links its draft plan, and accepts it", async () => {
    const calls: Call[] = [];
    stub(calls);
    const { container } = render(wrap());

    const title = await waitFor(() => screen.getByText("Retry failed webhooks"));
    expect(screen.getByText("Speed up the export")).toBeTruthy();
    fireEvent.click(within(title.closest("tr")!).getByText("Open"));

    // The agent's Markdown arrives as text: a tag in it is shown, not run.
    await waitFor(() => screen.getByText(/Add an outbox/));
    expect(container.querySelector("script")).toBeNull();
    const link = screen.getByText("Open the draft plan").closest("a")!;
    expect(link.getAttribute("href")).toBe("/p/acme/work/plans?plan=plan-9");

    fireEvent.click(screen.getByText("Accept"));
    await waitFor(() =>
      expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/ideas/idea-p/accept"))).toBe(
        true,
      ),
    );
  });

  it("moves an open idea within its priority", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());

    await waitFor(() => screen.getByText("Second"));
    fireEvent.click(screen.getByLabelText("Move “Second” up"));
    await waitFor(() => {
      const patch = calls.find((c) => c.method === "PATCH" && c.url.endsWith("/ideas/idea-2"));
      expect(patch && JSON.parse(patch.body!)).toEqual({ position: 1 });
    });
    expect((screen.getByLabelText("Move “First in line” up") as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
});
