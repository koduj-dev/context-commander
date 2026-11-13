import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import { ProjectLayout } from "./ProjectLayout";
import { ProjectPlans, progress } from "./ProjectPlans";

const now = new Date().toISOString();
const effort = {
  seconds: 0,
  tokens_in: 0,
  tokens_out: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  cache_write_1h_tokens: 0,
};

const BOARD = {
  phases: [
    {
      id: "ph-1",
      position: 1,
      status: "active",
      title: "Release 1.2",
      goal: "",
      counts: { open: 1, in_progress: 1, done: 2, blocked: 0, skipped: 3 },
      effort: {
        seconds: 5400,
        tokens_in: 1000,
        tokens_out: 200,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cache_write_1h_tokens: 0,
        models: [
          {
            model: "",
            tokens_in: 400,
            tokens_out: 0,
            cache_read_tokens: 0,
            cache_write_tokens: 0,
            cache_write_1h_tokens: 0,
            estimated_cost: null,
          },
          {
            model: "claude-opus-5-5",
            tokens_in: 600,
            tokens_out: 200,
            cache_read_tokens: 0,
            cache_write_tokens: 0,
            cache_write_1h_tokens: 0,
            estimated_cost: 0.0064,
          },
        ],
        cost_estimate: {
          currency: "USD",
          prices_as_of: "2026-11-03",
          amount: 0.0064,
          unpriced_models: [""],
        },
      },
    },
  ],
  plans: [
    {
      id: "plan-1",
      phase_id: "ph-1",
      status: "active",
      title: "Release checklist",
      created_by: "pr-a",
      updated_at: now,
      counts: { open: 1, in_progress: 1, done: 2, blocked: 0, skipped: 3 },
      effort,
    },
  ],
  in_progress: [],
  blocked: [],
  presence: [
    {
      client_id: "c-1",
      client_type: "codex",
      principal_id: "pr-agent",
      you: false,
      project: "acme/work",
      plan_id: "plan-1",
      plan_title: "Release checklist",
      item_id: "it-1",
      item_text: "Smoke test",
      activity: "running the suite",
      since: now,
      last_seen: now,
      idle: false,
    },
  ],
  people: {
    "pr-agent": { kind: "service", name: "Codex at home" },
    "pr-a": { kind: "user", name: "Alice" },
  },
};

const PLAN = {
  id: "plan-1",
  phase_id: "ph-1",
  status: "active",
  version: 3,
  title: "Release checklist",
  body: "",
  created_by: "pr-a",
  created_at: now,
  updated_at: now,
  items: [
    {
      id: "it-1",
      plan_id: "plan-1",
      position: 1,
      kind: "check",
      status: "open",
      text: "Smoke test",
      updated_at: now,
      effort: {
        seconds: 1800,
        tokens_in: 0,
        tokens_out: 0,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        cache_write_1h_tokens: 0,
      },
    },
  ],
};

type Call = { url: string; method: string; body?: string };

const SENT = [
  {
    id: "m-1",
    from: "pr-me",
    audience: "session",
    to_principal: "pr-agent",
    to_client: "c-1",
    body: "Use staging, not production.",
    created_at: now,
    expires_at: now,
    expired: false,
    deliveries: [
      {
        client_id: "c-1",
        client_type: "codex",
        principal_id: "pr-agent",
        delivered_at: now,
        acked_at: now,
      },
    ],
  },
  {
    id: "m-2",
    from: "pr-me",
    audience: "project",
    body: "Freeze the schema.",
    created_at: now,
    expires_at: now,
    expired: false,
    deliveries: [],
  },
];

type StubOptions = {
  /** The board's event stream; absent, it is refused as a server without one would. */
  events?: () => ReadableStream<Uint8Array>;
  plan?: typeof PLAN;
  /** What GET …/messages returns: the messages the reader sent. */
  sent?: unknown[];
};

function stub(calls: Call[], opts: StubOptions = {}) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
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
      if (url.endsWith("/plans/events") && opts.events) {
        return new Response(opts.events(), {
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
        });
      }
      if (url.endsWith("/plans/board")) return json(BOARD);
      if (url.endsWith("/plans/plan-1")) return json(opts.plan ?? PLAN);
      // Abandoned, so not on the board; reachable by id only.
      if (url.endsWith("/plans/plan-old"))
        return json({
          ...PLAN,
          id: "plan-old",
          status: "abandoned",
          title: "Old approach",
          items: [{ ...PLAN.items[0], id: "it-old", plan_id: "plan-old", text: "Try the cache" }],
        });
      if (url.includes("/phases/") && method === "PATCH") return json(BOARD.phases[0]);
      if (url.includes("/plans/items/") && method === "PATCH")
        return json({ ...PLAN.items[0], status: "in_progress" });
      if (url.endsWith("/plans/presence") && method === "POST") return json(BOARD.presence[0]);
      if (url.endsWith("/acme/work/messages") && method === "POST")
        return new Response(JSON.stringify(SENT[0]), { status: 201 });
      if (url.includes("/acme/work/messages"))
        return json({ items: opts.sent ?? [], people: BOARD.people });
      return new Response("{}", { status: 404 });
    }),
  );
}

function wrap(entry = "/p/acme/work/plans") {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter initialEntries={[entry]}>
              <CrumbsProvider>
                <Routes>
                  <Route path="/p/:namespace/:slug" element={<ProjectLayout />}>
                    <Route path="plans" element={<ProjectPlans />} />
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

describe("the plans board", () => {
  it("shows who is where, and progress that leaves skipped steps out", async () => {
    stub([]);
    render(wrap());

    const who = await waitFor(() => screen.getByText("Codex at home"));
    const row = who.closest("tr")!;
    expect(within(row).getByText("Release checklist → Smoke test")).toBeTruthy();
    expect(within(row).getByText("running the suite")).toBeTruthy();
    expect(within(row).getByText("agent")).toBeTruthy();

    // 2 done of the 4 that count; the 3 skipped are neither done nor owed.
    expect(screen.getAllByText("2 of 4 done").length).toBeGreaterThan(0);
    expect(screen.getByText("1.5 hr tracked")).toBeTruthy();
    expect(screen.getByText("1,200 tokens reported")).toBeTruthy();
  });

  it("puts a cost estimate beside the tokens, and says what it leaves out", async () => {
    stub([]);
    render(wrap());

    const est = await waitFor(() =>
      screen.getByText("$0.0064 estimated, models without a price left out"),
    );
    const title = est.getAttribute("title")!;
    expect(title).toContain("model not stated: 400 input");
    expect(title).toContain("— no price");
    expect(title).toContain("claude-opus-5-5: 600 input · 200 output");
    expect(title).toContain("as of 2026-11-03; not what was spent.");
    // No price table, no estimate: the plan's effort carries none.
    expect(screen.getAllByText(/estimated/).length).toBe(1);
  });

  it("“Work on this” starts the step and puts the reader on the board", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());

    fireEvent.click(await waitFor(() => screen.getByText("Open")));
    fireEvent.click(await waitFor(() => screen.getByText("Work on this")));

    await waitFor(() =>
      expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/plans/presence"))).toBe(
        true,
      ),
    );
    const patch = calls.find((c) => c.method === "PATCH")!;
    expect(patch.url).toMatch(/\/plans\/items\/it-1$/);
    expect(JSON.parse(patch.body!)).toEqual({ status: "in_progress" });
    const report = calls.find((c) => c.method === "POST" && c.url.endsWith("/plans/presence"))!;
    expect(JSON.parse(report.body!)).toEqual({ item_id: "it-1" });
    // The check is labelled as one.
    expect(screen.getAllByText("Check").length).toBeGreaterThan(0);
  });
});

describe("a link from search", () => {
  it("opens the plan it names and marks the step", async () => {
    stub([]);
    render(wrap("/p/acme/work/plans?plan=plan-1&item=it-1"));

    // Open without a click: the detail is already there.
    const step = await waitFor(() => screen.getByText("Smoke test", { selector: "td *" }));
    const row = step.closest("tr")!;
    expect(row.getAttribute("aria-current")).toBe("true");
    expect(row.className).toContain("active");
  });

  it("loads a plan the board leaves out by its id", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap("/p/acme/work/plans?plan=plan-old&item=it-old"));

    expect(await waitFor(() => screen.getByText("Opened from search"))).toBeTruthy();
    const step = await waitFor(() => screen.getByText("Try the cache"));
    expect(step.closest("tr")!.getAttribute("aria-current")).toBe("true");
    expect(calls.some((c) => c.url.endsWith("/plans/plan-old"))).toBe(true);
  });
});

/** A stream the test writes Server-Sent Events frames into. */
function eventStream() {
  let push: (frame: string) => void = () => {};
  const encoder = new TextEncoder();
  const open = () =>
    new ReadableStream<Uint8Array>({
      start(controller) {
        push = (frame) => controller.enqueue(encoder.encode(frame));
      },
    });
  return { open, push: (frame: string) => push(frame) };
}

describe("the board, live", () => {
  it("re-reads when the stream says it changed, and says it is live", async () => {
    const calls: Call[] = [];
    const events = eventStream();
    stub(calls, { events: events.open });
    render(wrap());
    const boardReads = () => calls.filter((c) => c.url.endsWith("/plans/board")).length;

    await waitFor(() => screen.getByText("Codex at home"));
    expect(screen.getByText("Refreshes every 10 seconds")).toBeTruthy();

    events.push("retry: 5000\nevent: ready\ndata: {}\n\n");
    await waitFor(() => screen.getByText("Updates as soon as something changes"));
    // "ready" re-reads once, to cover what changed before the stream opened.
    await waitFor(() => expect(boardReads()).toBe(2));

    events.push(": ping\n\n");
    events.push("event: changed\ndata: {}\n\n");
    await waitFor(() => expect(boardReads()).toBe(3));
  });

  it("keeps polling when the server offers no stream", async () => {
    stub([]);
    render(wrap());
    await waitFor(() => screen.getByText("Codex at home"));
    expect(screen.getByText("Refreshes every 10 seconds")).toBeTruthy();
  });
});

describe("reordering", () => {
  it("moves a step with the keyboard-reachable buttons, by 1-based place", async () => {
    const calls: Call[] = [];
    const first = PLAN.items[0]!;
    const second = { ...first, id: "it-2", position: 2, kind: "task", text: "Tag" };
    stub(calls, { plan: { ...PLAN, items: [first, second] } });
    render(wrap());

    fireEvent.click(await waitFor(() => screen.getByText("Open")));
    const up = await waitFor(() => screen.getByRole("button", { name: "Move “Tag” up" }));
    expect(
      (screen.getByRole("button", { name: "Move “Smoke test” up" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Move “Tag” down" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    fireEvent.click(up);

    await waitFor(() =>
      expect(calls.some((c) => c.method === "PATCH" && c.url.endsWith("/plans/items/it-2"))).toBe(
        true,
      ),
    );
    const patch = calls.find((c) => c.method === "PATCH" && c.url.endsWith("/plans/items/it-2"))!;
    expect(JSON.parse(patch.body!)).toEqual({ position: 1 });
  });

  it("offers no move for the only phase", async () => {
    stub([]);
    render(wrap());
    const up = await waitFor(() => screen.getByRole("button", { name: "Move “Release 1.2” up" }));
    expect((up as HTMLButtonElement).disabled).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Move “Release 1.2” down" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
});

describe("progress", () => {
  it("is done over everything except skipped", () => {
    expect(progress({ open: 1, in_progress: 0, done: 1, blocked: 0, skipped: 5 })).toEqual({
      done: 1,
      total: 2,
      percent: 50,
    });
    expect(progress({ open: 0, in_progress: 0, done: 0, blocked: 0, skipped: 2 }).percent).toBe(0);
  });

  it("sends a message to an agent working on the project, and to everyone", async () => {
    const calls: Call[] = [];
    stub(calls);
    render(wrap());

    const row = (await waitFor(() => screen.getByText("Codex at home"))).closest("tr")!;
    fireEvent.click(within(row).getByLabelText("Send a message to this agent"));
    expect(screen.getByText("To Codex at home (codex)")).toBeTruthy();
    const send = screen.getByText("Send") as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "Use staging, not production." },
    });
    fireEvent.click(send);
    await waitFor(() =>
      expect(calls.some((c) => c.method === "POST" && c.url.endsWith("/messages"))).toBe(true),
    );
    const post = calls.find((c) => c.method === "POST" && c.url.endsWith("/messages"))!;
    expect(JSON.parse(post.body!)).toEqual({
      body: "Use staging, not production.",
      to_client: "c-1",
    });
    await waitFor(() => expect(screen.queryByText("To Codex at home (codex)")).toBeNull());

    fireEvent.click(screen.getByText("Message everyone"));
    expect(screen.getByText("To everyone working on this project")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "Freeze the schema." } });
    fireEvent.click(screen.getByText("Send"));
    await waitFor(() =>
      expect(calls.filter((c) => c.method === "POST" && c.url.endsWith("/messages")).length).toBe(
        2,
      ),
    );
    const broadcast = calls.filter((c) => c.method === "POST" && c.url.endsWith("/messages"))[1];
    expect(JSON.parse(broadcast!.body!)).toEqual({ body: "Freeze the schema." });
  });

  it("lists sent messages with whether they were delivered and acknowledged", async () => {
    stub([], { sent: SENT });
    render(wrap());

    const acked = (await waitFor(() => screen.getByText("Use staging, not production."))).closest(
      "tr",
    )!;
    expect(within(acked).getByText("Codex at home · one session")).toBeTruthy();
    expect(within(acked).getByText("Acknowledged: 1 session")).toBeTruthy();
    const waiting = screen.getByText("Freeze the schema.").closest("tr")!;
    expect(within(waiting).getByText("Everyone on the project")).toBeTruthy();
    expect(within(waiting).getByText("Waiting")).toBeTruthy();
  });

  it("offers no message to a browser session", async () => {
    stub([]);
    const browser = { ...BOARD.presence[0]!, client_id: "c-2", client_type: "session" };
    BOARD.presence.push(browser);
    try {
      render(wrap());
      await waitFor(() => screen.getAllByText("Codex at home"));
      expect(screen.getAllByLabelText("Send a message to this agent").length).toBe(1);
    } finally {
      BOARD.presence.pop();
    }
  });
});
