import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectMembers } from "./ProjectMembers";

const member = (id: string, name: string, role: string) => ({
  principal_id: id,
  role,
  created_at: "2026-11-01T10:00:00Z",
  display_name: name,
});

type Call = { method: string; path: string };

/** The reader is pr-me with `myRole`; DELETE of `refuse` answers 409 last_owner. */
function stub(myRole: string, refuse?: string) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
      const path = url.split("?")[0] ?? url;
      const method = init?.method ?? "GET";
      calls.push({ method, path });
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
      if (path.endsWith("/members") && method === "GET")
        return json([
          member("pr-ada", "Ada", "OWNER"),
          member("pr-me", "Me", myRole),
          member("pr-bob", "Bob", "CONTRIBUTOR"),
        ]);
      if (path.includes("/members/") && method === "DELETE" && refuse && path.endsWith(refuse))
        return json(
          {
            error: {
              code: "last_owner",
              message: "the project needs at least one OWNER; make someone else OWNER first",
            },
          },
          409,
        );
      if (path.includes("/members/")) return new Response(null, { status: 204 });
      if (path.endsWith("/invitations")) return json({ items: [] });
      return json({}, 404);
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
            <MemoryRouter initialEntries={["/p/acme/work/members"]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route path="members" element={<ProjectMembers />} />
                  </Route>
                  <Route path="/" element={<p>home</p>} />
                </Routes>
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

/** The table row of the member whose name cell reads `name`. */
async function row(name: string): Promise<HTMLElement> {
  return waitFor(() => {
    const found = screen
      .getAllByRole("row")
      .find((r) => r.querySelector("td")?.textContent?.startsWith(name));
    if (!found) throw new Error(`no row for ${name}`);
    return found;
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("project members", () => {
  it("lets an ADMIN manage everyone but an OWNER", async () => {
    const calls = stub("ADMIN");
    vi.stubGlobal(
      "confirm",
      vi.fn(() => true),
    );
    render(wrap());

    const ada = await row("Ada");
    expect(within(ada).queryByRole("button", { name: "Remove" })).toBeNull();
    expect(within(ada).queryByRole("combobox", { name: /Role of Ada/ })).toBeNull();

    const bob = await row("Bob");
    expect(within(bob).getByRole("combobox", { name: /Role of Bob/ })).toBeTruthy();
    fireEvent.click(within(bob).getByRole("button", { name: "Remove" }));
    await waitFor(() =>
      expect(calls).toContainEqual({
        method: "DELETE",
        path: "/v1/projects/acme/work/members/pr-bob",
      }),
    );

    expect(within(await row("Me")).getByRole("button", { name: "Leave project" })).toBeTruthy();
  });

  it("shows the server's refusal when the last OWNER tries to leave", async () => {
    stub("OWNER", "/members/pr-me");
    vi.stubGlobal(
      "confirm",
      vi.fn(() => true),
    );
    render(wrap());

    fireEvent.click(within(await row("Me")).getByRole("button", { name: "Leave project" }));
    expect(await screen.findByText(/at least one OWNER/)).toBeTruthy();
    expect(screen.queryByText("home")).toBeNull();
  });

  it("asks before removing, and does nothing on cancel", async () => {
    const calls = stub("OWNER");
    vi.stubGlobal(
      "confirm",
      vi.fn(() => false),
    );
    render(wrap());

    fireEvent.click(within(await row("Bob")).getByRole("button", { name: "Remove" }));
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
  });

  it("offers a READER nothing but leaving", async () => {
    stub("READER");
    render(wrap());
    const bob = await row("Bob");
    expect(within(bob).queryByRole("combobox")).toBeNull();
    expect(within(bob).queryByRole("button", { name: "Remove" })).toBeNull();
    expect(within(await row("Me")).getByRole("button", { name: "Leave project" })).toBeTruthy();
  });
});
