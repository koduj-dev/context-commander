import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

const OVERVIEW = {
  from: "2026-11-03",
  days: 14,
  items: [
    {
      project: "acme/beta",
      name: "Beta",
      role: "MEMBER",
      counts: { open: 1, in_progress: 0, done: 2, blocked: 1, skipped: 3 },
      done_per_day: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1],
      last_activity: "2026-11-16T09:00:00Z",
    },
  ],
};

const PRESENCE = {
  items: [
    {
      client_id: "c-1",
      client_type: "claude-code",
      principal_id: "pr-2",
      you: false,
      project: "acme/beta",
      since: "2026-11-16T08:00:00Z",
      last_seen: "2026-11-16T09:00:00Z",
      idle: false,
    },
  ],
  people: { "pr-2": { kind: "user", name: "Ada Lovelace" } },
};

const IDEAS = {
  items: [{ project: "acme/beta", name: "Beta", open: 0, claimed: 0, lapsed: 0, proposed: 1 }],
};

/** A URL ending in one of `failing` answers 500; `pending` are projects scheduled for deletion. */
function stubServer(waiting: unknown = WAITING, failing: string[] = [], pending: unknown[] = []) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push(init?.method && init.method !== "GET" ? `${init.method} ${url}` : url);
      if (url.endsWith("/v1/me/projects/pending-deletion")) {
        return new Response(JSON.stringify({ items: pending }), { status: 200 });
      }
      if (url.includes("/pending-deletion/") && url.endsWith("/cancel")) {
        return new Response(null, { status: 204 });
      }
      if (failing.some((f) => url.endsWith(f))) {
        return new Response(JSON.stringify({ error: { code: "internal", message: "boom" } }), {
          status: 500,
        });
      }
      if (url.endsWith("/v1/me/projects/overview")) {
        return new Response(JSON.stringify(OVERVIEW), { status: 200 });
      }
      if (url.endsWith("/v1/me/presence")) {
        return new Response(JSON.stringify(PRESENCE), { status: 200 });
      }
      if (url.endsWith("/v1/me/ideas/counts")) {
        return new Response(JSON.stringify(IDEAS), { status: 200 });
      }
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

  it("shows each project's progress, people, and what waits, in one row", async () => {
    stubServer();
    render(wrap());

    const table = (await screen.findByRole("heading", { name: "Projects at a glance" })).closest(
      "section",
    )!;
    const beta = (await within(table as HTMLElement).findByText("acme/beta")).closest("tr")!;
    // Skipped items count in neither the share nor the total.
    expect(within(beta).getByText("50% · 2 of 4 items")).toBeTruthy();
    expect(
      within(beta).getByRole("img", { name: "2 done, 0 in progress, 1 blocked, 1 open" }),
    ).toBeTruthy();
    expect(within(beta).getByRole("img", { name: "Ada Lovelace" })).toBeTruthy();
    // One open handoff and one proposal to decide.
    expect(within(beta).getByText("2 items")).toBeTruthy();

    // A project the overview leaves out still has its row, with the reason.
    const alpha = within(table as HTMLElement)
      .getByText("acme/alpha")
      .closest("tr")!;
    expect(within(alpha).getByText("Your role does not show plans")).toBeTruthy();
  });

  it("says plainly when nothing is waiting", async () => {
    stubServer([]);
    render(wrap());

    await waitFor(() => expect(screen.getByText("Nothing is waiting on you")).toBeTruthy());
  });

  it("keeps every project's link when the overview fails", async () => {
    stubServer(WAITING, ["/v1/me/projects/overview"]);
    render(wrap());

    const table = (await screen.findByRole("heading", { name: "Projects at a glance" })).closest(
      "section",
    ) as HTMLElement;
    expect(await within(table).findByText("Something went wrong")).toBeTruthy();
    expect(within(table).getByRole("link", { name: "Alpha" }).getAttribute("href")).toBe(
      "/p/acme/alpha",
    );
    // The reason a row has no numbers is the failure, not the reader's role.
    expect(within(table).queryByText("Your role does not show plans")).toBeNull();
  });

  it("shows a failed project list as an error, not as no projects", async () => {
    stubServer(WAITING, ["/v1/me/projects", "/v1/me/projects/overview"]);
    render(wrap());

    const table = (await screen.findByRole("heading", { name: "Projects at a glance" })).closest(
      "section",
    ) as HTMLElement;
    expect((await within(table).findAllByText("Something went wrong")).length).toBe(2);
    expect(within(table).queryByText("You are not in any project yet")).toBeNull();
    expect(within(table).getAllByRole("button", { name: "Try again" })).toHaveLength(2);
  });

  it("offers the way back for a project scheduled for deletion", async () => {
    const calls = stubServer(
      WAITING,
      [],
      [
        {
          id: "p9",
          project: "acme/old",
          name: "Old",
          requested_at: "2026-11-16T10:00:00Z",
          requested_by: "pr-1",
          purge_after: "2026-11-23T10:00:00Z",
        },
      ],
    );
    render(wrap());

    const panel = (await screen.findByRole("heading", { name: "Scheduled for deletion" })).closest(
      "section",
    ) as HTMLElement;
    expect(within(panel).getByText("acme/old")).toBeTruthy();
    fireEvent.click(within(panel).getByRole("button", { name: "Cancel deletion" }));
    await waitFor(() => expect(calls).toContain("POST /v1/me/projects/pending-deletion/p9/cancel"));
  });

  it("draws nothing when no project is scheduled for deletion", async () => {
    stubServer();
    render(wrap());
    await screen.findByRole("heading", { name: "Projects at a glance" });
    expect(screen.queryByRole("heading", { name: "Scheduled for deletion" })).toBeNull();
  });
});
