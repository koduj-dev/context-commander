import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { App } from "../App";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "./auth";
import { CrumbsProvider } from "./crumbs";
import { PrefsProvider } from "./prefs";
import { ThemeProvider } from "./theme";

function renderAs(me: Record<string, unknown>) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url.endsWith("/v1/me")) return new Response(JSON.stringify(me), { status: 200 });
      return new Response(JSON.stringify({ items: [] }), { status: 200 });
    }),
  );
  render(
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter>
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

describe("a temporary password", () => {
  it("shows only the form to replace it, and asks the server for nothing else", async () => {
    const calls = renderAs({
      principal_id: "pr-1",
      kind: "user",
      display_name: "Bob",
      has_password: true,
      must_change_password: true,
    });
    expect(await screen.findByText(/An administrator set this password for you/)).toBeTruthy();
    expect(screen.getByLabelText("Current password")).toBeTruthy();
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(calls.filter((u) => !u.endsWith("/v1/me"))).toEqual([]);
  });
});
