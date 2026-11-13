import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectMemories, safeTicketURL } from "./ProjectMemories";

const now = new Date().toISOString();

const MEMORY = {
  id: "m-1",
  kind: "gotcha",
  status: "active",
  version: 1,
  title: "Webhook retries arrive out of order",
  body: "Sort by event_seq.",
  branch: "feature/webhooks",
  ticket_key: "PROJ-77",
  ticket_url: "https://tracker.example.test/PROJ-77",
  tags: ["webhooks"],
  assignee: "pr-a",
  created_by: "pr-a",
  created_at: now,
  updated_at: now,
};

const PEOPLE = { "pr-a": { kind: "user", name: "Alice" } };

type Call = { url: string; method: string; body?: string };

function stub(calls: Call[], memory: Record<string, unknown> = MEMORY) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
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
      if (url.includes("/memories/facets")) {
        return json({
          kinds: ["decision", "finding", "gotcha", "note", "status", "other"],
          branches: ["feature/webhooks"],
          tickets: ["PROJ-77"],
          tags: ["webhooks"],
          assignees: ["pr-a"],
          people: PEOPLE,
        });
      }
      if (url.includes("/memories/search")) {
        return json({
          items: [{ ...memory, body: "", match: "body", snippet: "Sort by event_seq." }],
          truncated: true,
          people: PEOPLE,
        });
      }
      if (url.includes("/memories/m-1")) return json(memory);
      if (url.includes("/memories/") && method === "POST") return json(memory, 201);
      if (url.includes("/memories/")) return json({ items: [memory], people: PEOPLE });
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
            <MemoryRouter initialEntries={["/p/acme/work/memories"]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route path="memories" element={<ProjectMemories />} />
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

afterEach(() => vi.unstubAllGlobals());

describe("the memories tab", () => {
  it("lists memories with their metadata and opens one with a safe ticket link", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());

    expect(await screen.findByText("Webhook retries arrive out of order")).toBeTruthy();
    expect(screen.getAllByText("feature/webhooks").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));

    const link = await screen.findByRole("link", { name: "PROJ-77" });
    expect(link.getAttribute("href")).toBe("https://tracker.example.test/PROJ-77");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(screen.getByText("Sort by event_seq.")).toBeTruthy();
  });

  it("never renders a non-http ticket URL as a link", async () => {
    const calls: Call[] = [];
    stub(calls, { ...MEMORY, ticket_url: "javascript:alert(1)" });
    render(wrap());
    fireEvent.click(await screen.findByRole("button", { name: "Open" }));
    await screen.findByText("Sort by event_seq.");
    expect(screen.queryByRole("link", { name: "PROJ-77" })).toBeNull();
    expect(safeTicketURL("javascript:alert(1)")).toBeUndefined();
    expect(safeTicketURL("data:text/html,x")).toBeUndefined();
    expect(safeTicketURL("https://ok.example.test/x")).toBe("https://ok.example.test/x");
  });

  it("searches with the filters set and says when the answer was cut", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());
    await screen.findByText("Webhook retries arrive out of order");

    fireEvent.change(screen.getByLabelText("Search memories"), { target: { value: "event_seq" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));

    expect(
      await screen.findByText(
        "More memories may match than were searched. Add a filter or narrow the words.",
      ),
    ).toBeTruthy();
    const search = calls.find((c) => c.url.includes("/memories/search"));
    expect(search?.url).toContain("q=event_seq");
  });

  it("creates a memory with its branch, ticket and tags", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());
    await screen.findByText("Webhook retries arrive out of order");

    fireEvent.click(screen.getByRole("button", { name: "New memory" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Use event_seq" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Branch" }), {
      target: { value: "main" },
    });
    fireEvent.change(screen.getByLabelText("Ticket key"), { target: { value: "PROJ-1" } });
    fireEvent.change(screen.getByLabelText("Tags, separated by commas"), {
      target: { value: "Webhooks, queue" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save memory" }));

    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const body = JSON.parse(calls.find((c) => c.method === "POST")!.body!);
    expect(body).toMatchObject({
      title: "Use event_seq",
      branch: "main",
      ticket_key: "PROJ-1",
      tags: ["Webhooks", "queue"],
      kind: "note",
    });
  });
});
