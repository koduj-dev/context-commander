import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { Dashboard } from "./Dashboard";

/**
 * The dashboard asks once, not once per project.
 *
 * It used to call `handoffs/current` for every membership — N requests to
 * answer one question, and the shape that stops working long before it stops
 * being tempting. `GET /me/handoffs` replaced it, and this test is what keeps
 * it replaced: nothing else would notice a loop creeping back, because the
 * screen looks identical either way.
 */

const PROJECTS = [
  {
    id: "p1",
    namespace: "acme",
    slug: "alpha",
    name: "Alpha",
    visibility: "private",
    role: "OWNER",
  },
  {
    id: "p2",
    namespace: "acme",
    slug: "beta",
    name: "Beta",
    visibility: "private",
    role: "MEMBER",
  },
  {
    id: "p3",
    namespace: "acme",
    slug: "gamma",
    name: "Gamma",
    visibility: "private",
    role: "MEMBER",
  },
];

const WAITING = [
  {
    project: "acme/beta",
    role: "MEMBER",
    id: "h-1",
    status: "open",
    created_at: "2026-10-08T09:00:00Z",
    summary: "Picked the schema, migration is next",
  },
];

function stubServer(waiting: unknown = WAITING) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url.endsWith("/v1/me/handoffs")) {
        return new Response(JSON.stringify(waiting), { status: 200 });
      }
      if (url.endsWith("/v1/me/projects")) {
        return new Response(JSON.stringify(PROJECTS), { status: 200 });
      }
      if (url.endsWith("/v1/me")) {
        return new Response(
          JSON.stringify({
            principal_id: "pr-1",
            kind: "user",
            email: "someone@example.test",
            display_name: "Someone",
            is_installation_admin: false,
            has_password: true,
          }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 404 });
    }),
  );
  return calls;
}

function wrap() {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter>
              <CrumbsProvider>
                <Dashboard />
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

describe("the dashboard", () => {
  it("asks for waiting work once, whatever the number of projects", async () => {
    const calls = stubServer();
    render(wrap());

    await waitFor(() =>
      expect(screen.getByText("Picked the schema, migration is next")).toBeTruthy(),
    );

    const waiting = calls.filter((u) => u.endsWith("/v1/me/handoffs"));
    expect(waiting).toHaveLength(1);
    // The loop this replaced. Three projects would have meant three of these.
    expect(calls.filter((u) => u.includes("/handoffs/current"))).toHaveLength(0);
  });

  it("labels a row with its project's name, from the list it already loads", async () => {
    stubServer();
    render(wrap());

    // Scoped to the waiting row, because the projects panel beside it links
    // to the same project by the same name — a bare getByRole finds both.
    const summary = await waitFor(() => screen.getByText("Picked the schema, migration is next"));
    const row = summary.closest("tr");
    expect(row).not.toBeNull();

    // The row carries "acme/beta" and no name; the name comes from
    // /me/projects. Showing the path where a name belongs would be a
    // regression nobody would read as one.
    const link = within(row!).getByRole("link", { name: "Beta" });
    expect(link.getAttribute("href")).toBe("/p/acme/beta");
  });

  it("keeps a row whose project is not in the list yet", async () => {
    // The two reads are not one transaction, so a project joined a moment ago
    // can appear in the handoff row before it appears in the list. Dropping
    // the row would hide work that is waiting on someone.
    stubServer([{ ...WAITING[0], project: "acme/brand-new", id: "h-2" }]);
    render(wrap());

    const summary = await waitFor(() => screen.getByText("Picked the schema, migration is next"));
    const link = within(summary.closest("tr")!).getByRole("link", { name: "acme/brand-new" });
    expect(link.getAttribute("href")).toBe("/p/acme/brand-new");
  });

  it("says plainly when nothing is waiting", async () => {
    stubServer([]);
    render(wrap());

    await waitFor(() => expect(screen.getByText("Nothing is waiting on you")).toBeTruthy());
  });
});
