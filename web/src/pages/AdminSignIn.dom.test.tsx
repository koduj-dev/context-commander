import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { AdminSignIn } from "./AdminSignIn";

const ME = {
  principal_id: "pr-1",
  kind: "user",
  email: "admin@example.test",
  display_name: "Admin",
  is_installation_admin: true,
  has_password: true,
};

const ORGS = {
  items: [
    { id: "org-a", name: "4NET" },
    { id: "org-b", name: "Partners" },
  ],
};

const RULES = {
  items: [
    {
      id: "rule-1",
      kind: "domain",
      value: "4net.tv",
      organizations: [{ id: "org-a", name: "4NET" }],
      created_at: "2026-11-13T00:00:00Z",
    },
  ],
};

type Call = { url: string; method: string; body?: string };

function stubServer(calls: Call[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      if (method === "POST") return new Response(JSON.stringify(RULES.items[0]), { status: 201 });
      if (method === "PUT") return new Response(JSON.stringify(RULES.items[0]), { status: 200 });
      if (url.endsWith("/v1/me")) return new Response(JSON.stringify(ME), { status: 200 });
      if (url.endsWith("/v1/installation/organizations"))
        return new Response(JSON.stringify(ORGS), { status: 200 });
      if (url.endsWith("/v1/installation/signin-rules"))
        return new Response(JSON.stringify(RULES), { status: 200 });
      return new Response("{}", { status: 404 });
    }),
  );
}

function wrap(children: ReactNode) {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter>
              <CrumbsProvider>{children}</CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

describe("the sign-in rules screen", () => {
  it("lists rules with what they grant", async () => {
    stubServer([]);
    render(wrap(<AdminSignIn />));
    const row = (await waitFor(() => screen.getByText("4net.tv"))).closest("tr")!;
    expect(within(row).getByText("Domain")).toBeTruthy();
    expect(within(row).getByText("4NET")).toBeTruthy();
  });

  it("sends the typed value and every picked organization", async () => {
    const calls: Call[] = [];
    stubServer(calls);
    render(wrap(<AdminSignIn />));
    await waitFor(() => screen.getByText("4net.tv"));

    const add = screen.getByText("Add rule").closest("button")!;
    expect(add.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Domain or address"), {
      target: { value: " guest@gmail.com " },
    });
    const picks = screen.getAllByRole("group", { name: "Organizations" })[0]!;
    fireEvent.click(within(picks).getByText("4NET"));
    fireEvent.click(within(picks).getByText("Partners"));
    fireEvent.click(within(picks).getByText("4NET")); // unpicked again
    fireEvent.click(add);

    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true));
    const post = calls.find((c) => c.method === "POST")!;
    expect(post.url).toMatch(/\/v1\/installation\/signin-rules$/);
    expect(JSON.parse(post.body!)).toEqual({
      value: "guest@gmail.com",
      organization_ids: ["org-b"],
    });
  });
});
