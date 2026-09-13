import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { CrumbsProvider } from "../app/crumbs";
import { AuthProvider } from "../app/auth";
import { PrefsProvider } from "../app/prefs";
import { Organizations } from "./Organizations";

/**
 * Picking an organization must show it.
 *
 * Written because the screen was reported as "the detail will not open" and
 * reading the code did not reveal why. An interaction test answers that in a
 * way staring at JSX does not.
 */
function stubApi(routes: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const path = url.replace(/^\/v1/, "").split("?")[0]!;
      const body = routes[path];
      if (body === undefined) {
        return new Response(JSON.stringify({ error: { code: "not_found" } }), { status: 404 });
      }
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
}

function renderScreen() {
  return render(
    <I18nProvider>
      <PrefsProvider>
        <AuthProvider>
          <MemoryRouter initialEntries={["/organizations"]}>
            <CrumbsProvider>
              <Organizations />
            </CrumbsProvider>
          </MemoryRouter>
        </AuthProvider>
      </PrefsProvider>
    </I18nProvider>,
  );
}

describe("Organizations", () => {
  beforeEach(() => {
    stubApi({
      "/me/organizations": [{ id: "org-1", name: "Koduj", role: "OWNER" }],
      "/organizations/org-1/members": [
        {
          user_id: "user-1",
          role: "OWNER",
          created_at: "2026-09-13T12:00:00Z",
          email: "jana@example.com",
          display_name: "Jana Dvořáková",
        },
      ],
      "/organizations/org-1/audit": { items: [] },
      // AuthProvider probes this on mount; without it the stub answers 404
      // and the screen renders signed out.
      "/me": {
        principal_id: "p-1",
        kind: "user",
        email: "filip@example.com",
        display_name: "Filip",
        is_installation_admin: false,
      },
    });
  });

  it("lists what the caller belongs to", async () => {
    renderScreen();
    expect(await screen.findByText("Koduj")).toBeTruthy();
  });

  it("opens the detail when one is picked", async () => {
    const user = userEvent.setup();
    renderScreen();

    await user.click(await screen.findByText("Koduj"));

    await waitFor(() => {
      expect(screen.queryByText("Select an organization")).toBeNull();
    });
    // Shown as a person, not as a UUID — the members endpoint joins the
    // user row precisely so this screen does not have to print an id.
    expect(await screen.findByText("Jana Dvořáková")).toBeTruthy();
  });
});
