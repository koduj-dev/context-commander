import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { AdminUsage } from "./AdminUsage";

const STATS = {
  scale: { projects: 2, organizations: 1, users: 3, service_principals: 1 },
  content: {
    artifacts: 0,
    artifact_versions: 0,
    stored_bytes: 0,
    secrets: 0,
    conversations: 0,
    history_objects: 0,
  },
  coordination: {
    handoffs_open: 1,
    handoffs_claimed: 0,
    handoffs_closed: 1,
    skills: 0,
    skill_versions: 0,
    skill_bindings: 0,
    gate_bindings: 0,
  },
  activity: { events: 4, audit_records: 4, security_events: 0 },
  credentials: { active: 2, revoked: 0, expired: 0, used_last_7_days: 2 },
};

const week = (start: string, extra: object = {}) => ({
  week_start: start,
  active_users: 0,
  active_agents: 0,
  projects_created: 0,
  handoffs_created: 0,
  handoffs_claimed: 0,
  handoffs_claimed_by_other_client: 0,
  handoffs_closed: 0,
  events: 0,
  ...extra,
});

const TRENDS = {
  weeks: [
    week("2026-10-19"),
    week("2026-10-26", {
      active_users: 3,
      active_agents: 1,
      handoffs_created: 2,
      handoffs_claimed: 1,
      handoffs_claimed_by_other_client: 1,
      median_seconds_to_claim: 2700,
    }),
  ],
  handoff_flow: {
    handoffs: 2,
    claimed: 1,
    claimed_by_other_client: 1,
    stale_open: 0,
    median_seconds_to_claim: 2700,
    p90_seconds_to_claim: 2700,
    projects: 2,
    projects_with_takeover: 1,
    median_seconds_to_first_takeover: 7200,
  },
};

function stubServer(trends: () => Response) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/v1/installation/trends")) return trends();
      if (url.endsWith("/v1/installation/stats")) {
        return new Response(JSON.stringify(STATS), { status: 200 });
      }
      if (url.endsWith("/v1/me")) {
        return new Response(
          JSON.stringify({
            principal_id: "pr-1",
            kind: "user",
            email: "admin@example.test",
            display_name: "Admin",
            is_installation_admin: true,
            has_password: true,
          }),
          { status: 200 },
        );
      }
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
            <MemoryRouter>
              <CrumbsProvider>
                <AdminUsage />
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

describe("the usage screen's trends", () => {
  it("lists the newest week first and leaves an unmeasured median blank, not zero", async () => {
    stubServer(() => new Response(JSON.stringify(TRENDS), { status: 200 }));
    render(wrap());

    const heading = await waitFor(() => screen.getByText("Last 2 weeks"));
    const table = heading.closest(".panel")!.querySelector("table")!;
    const [newest, oldest, ...rest] = within(table).getAllByRole("row").slice(1); // past the header
    expect(rest).toHaveLength(0);
    expect(within(newest!).getByText("45 min")).toBeTruthy();
    // The empty week has no claims: a dash, because "0 sec" would read as
    // "claimed instantly".
    expect(within(oldest!).getByText("—")).toBeTruthy();
    expect(within(oldest!).queryByText(/sec/)).toBeNull();

    // The loop panel: a measured duration where there is one, and an honest
    // "not yet" for claim-to-close, which the server left out.
    expect(screen.getByText("2 hr")).toBeTruthy();
    expect(screen.getByText("Not measured yet")).toBeTruthy();
  });

  it("keeps the counts on screen when the trends fail", async () => {
    stubServer(
      () => new Response(JSON.stringify({ error: "internal", message: "boom" }), { status: 500 }),
    );
    render(wrap());

    await waitFor(() => expect(screen.getByText("Handoffs")).toBeTruthy());
    expect(screen.queryByText("Handoff loop")).toBeNull();
  });
});
