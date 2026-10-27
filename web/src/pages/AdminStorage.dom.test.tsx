import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { AdminStorage, parseAmount } from "./AdminStorage";
import { StoragePanel } from "./AccountSecurity";

const ME = {
  principal_id: "pr-1",
  kind: "user",
  email: "admin@example.test",
  display_name: "Admin",
  is_installation_admin: true,
  has_password: true,
};

const NAMESPACES = {
  items: [
    {
      namespace_id: "ns-alice",
      slug: "alice",
      kind: "personal",
      display_name: "Alice",
      used_bytes: 900_000_000,
      limit_bytes: 1_000_000_000,
      created_at: "2026-10-01T00:00:00Z",
      override: "default",
    },
    {
      namespace_id: "ns-acme",
      slug: "acme",
      kind: "organization",
      display_name: "Acme",
      used_bytes: 5_000,
      created_at: "2026-10-02T00:00:00Z",
      override: "unlimited",
    },
  ],
};

type Call = { url: string; method: string; body?: string };

function stubServer(calls: Call[], routes: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? "GET", body: init?.body as string | undefined });
      if (init?.method === "PUT") return new Response(null, { status: 204 });
      for (const [suffix, body] of Object.entries(routes)) {
        if (url.endsWith(suffix)) return new Response(JSON.stringify(body), { status: 200 });
      }
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

describe("the storage screen", () => {
  it("says where each limit comes from and sends an exception in bytes", async () => {
    const calls: Call[] = [];
    stubServer(calls, { "/v1/me": ME, "/v1/installation/storage": NAMESPACES });
    render(wrap(<AdminStorage />));

    const alice = (await waitFor(() => screen.getByText("Alice"))).closest("tr")!;
    expect(within(alice).getByText("1.00 GB (default)")).toBeTruthy();
    const acme = screen.getByText("Acme").closest("tr")!;
    expect(within(acme).getByText("No limit")).toBeTruthy();

    fireEvent.click(within(alice).getByText("Change limit"));
    fireEvent.click(screen.getByText("Own limit"));
    const save = screen.getByText("Save limit").closest("button")!;
    // Nothing typed yet: there is no limit to send.
    expect(save.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("Amount"), { target: { value: "2,5" } });
    fireEvent.click(save);

    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    const put = calls.find((c) => c.method === "PUT")!;
    expect(put.url).toMatch(/\/v1\/installation\/storage\/ns-alice$/);
    expect(JSON.parse(put.body!)).toEqual({ limit_bytes: 2_500_000_000 });
  });

  it("puts a namespace back on the default with an empty body", async () => {
    const calls: Call[] = [];
    stubServer(calls, { "/v1/me": ME, "/v1/installation/storage": NAMESPACES });
    render(wrap(<AdminStorage />));

    const acme = (await waitFor(() => screen.getByText("Acme"))).closest("tr")!;
    fireEvent.click(within(acme).getByText("Change limit"));
    fireEvent.click(screen.getByText("Server default"));
    fireEvent.click(screen.getByText("Save limit"));

    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true));
    expect(JSON.parse(calls.find((c) => c.method === "PUT")!.body!)).toEqual({});
  });
});

describe("the reader's own storage", () => {
  it("shows what is used against the limit that applies, and nothing about how it was set", async () => {
    const calls: Call[] = [];
    stubServer(calls, {
      "/v1/me": ME,
      "/v1/me/storage": {
        items: [
          {
            namespace_id: "ns-1",
            slug: "me",
            kind: "personal",
            display_name: "Me",
            used_bytes: 250_000_000,
            limit_bytes: 1_000_000_000,
          },
          {
            namespace_id: "ns-2",
            slug: "team",
            kind: "organization",
            display_name: "Team",
            used_bytes: 0,
          },
        ],
      },
    });
    render(wrap(<StoragePanel />));

    expect(await waitFor(() => screen.getByText("250.00 MB of 1.00 GB"))).toBeTruthy();
    expect(screen.getByText("0 B · no limit")).toBeTruthy();
    expect(screen.queryByText(/default/)).toBeNull();
  });
});

describe("parseAmount", () => {
  it("reads both decimal separators and refuses what is not a number", () => {
    expect(parseAmount("2,5")).toBe(2.5);
    expect(parseAmount(" 10 ")).toBe(10);
    expect(parseAmount("1.25")).toBe(1.25);
    expect(parseAmount("")).toBeUndefined();
    expect(parseAmount("-1")).toBeUndefined();
    expect(parseAmount("ten")).toBeUndefined();
  });
});
