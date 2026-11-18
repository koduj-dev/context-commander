import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { App } from "../App";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "./auth";
import { CrumbsProvider } from "./crumbs";
import { PrefsProvider } from "./prefs";
import { ThemeProvider } from "./theme";

function renderAs(admin: boolean) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200 });
      if (url.endsWith("/v1/me"))
        return json({
          principal_id: "pr-1",
          kind: "user",
          display_name: "Ada",
          has_password: true,
          is_installation_admin: admin,
        });
      if (url.endsWith("/v1/installation/version"))
        return json({
          current: "v0.1.0-beta.4",
          update_check: true,
          latest: "v0.1.0-beta.5",
          url: "https://example.test/beta.5",
          update_available: true,
        });
      if (url.endsWith("/v1/me/projects") || url.endsWith("/v1/me/handoffs")) return json([]);
      return json({ items: [] });
    }),
  );
  render(
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter initialEntries={["/nowhere"]}>
              <CrumbsProvider>
                <App />
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>,
  );
  return calls;
}

describe("the update notice", () => {
  it("tells an administrator that a newer release exists", async () => {
    renderAs(true);
    expect(await screen.findByText(/Version v0.1.0-beta.5 is available/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Release notes" }).getAttribute("href")).toBe(
      "https://example.test/beta.5",
    );
  });

  it("does not ask on behalf of anyone else", async () => {
    const calls = renderAs(false);
    await screen.findByRole("navigation");
    await waitFor(() => expect(calls.some((u) => u.endsWith("/v1/me"))).toBe(true));
    expect(calls.some((u) => u.endsWith("/v1/installation/version"))).toBe(false);
  });
});
