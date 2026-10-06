import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "./auth";
import { PrefsProvider } from "./prefs";
import { ThemeProvider } from "./theme";
import { App } from "../App";

/**
 * What someone sees before they have a credential.
 *
 * Worth a DOM test rather than only an API test, because the decisions here
 * are routing ones: a fresh installation must land on first-run setup and not
 * on a login form asking for a password that cannot exist yet, and the
 * sign-in screen must offer only the methods the server says it has. Both are
 * invisible to the type checker and to the server's own tests.
 */

type Route = { status: number; body: unknown };

/** Routes every call the pre-credential screens make; anything else 404s. */
function stubServer(routes: Record<string, Route>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? "GET"} ${url}`;
      const route = routes[key];
      if (!route) return new Response("{}", { status: 404 });
      return new Response(JSON.stringify(route.body), { status: route.status });
    }),
  );
}

const UNAUTHORIZED: Route = {
  status: 401,
  body: { error: { code: "unauthorized", message: "no" } },
};

function wrap() {
  return (
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter>
              <App />
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  localStorage.clear();
});

describe("before signing in", () => {
  it("routes an installation with no users to first-run setup", async () => {
    stubServer({
      "GET /v1/me": UNAUTHORIZED,
      "GET /v1/setup/status": { status: 200, body: { needs_setup: true } },
      "GET /v1/auth/methods": { status: 200, body: { methods: [{ kind: "password" }] } },
    });
    render(wrap());

    // The setup code field is the thing that distinguishes this screen from
    // sign-in; asking for a password on an installation with no accounts is
    // the bug this screen exists to remove.
    await waitFor(() => expect(screen.getByLabelText("Setup code")).toBeTruthy());
    expect(screen.getByText("Create the first administrator")).toBeTruthy();
    expect(screen.queryByLabelText("Access token")).toBeNull();
  });

  it("routes an installation that has users to sign-in", async () => {
    stubServer({
      "GET /v1/me": UNAUTHORIZED,
      "GET /v1/setup/status": { status: 200, body: { needs_setup: false } },
      "GET /v1/auth/methods": {
        status: 200,
        body: { methods: [{ kind: "password" }, { kind: "token" }] },
      },
    });
    render(wrap());

    await waitFor(() => expect(screen.getByLabelText("Email")).toBeTruthy());
    expect(screen.queryByLabelText("Setup code")).toBeNull();
  });

  it("offers only the methods the server reports", async () => {
    // An installation with no local passwords must not show a password form.
    // This is the UI's standing rule — never offer a control for a capability
    // the backend lacks — on the one screen that used to break it.
    stubServer({
      "GET /v1/me": UNAUTHORIZED,
      "GET /v1/setup/status": { status: 200, body: { needs_setup: false } },
      "GET /v1/auth/methods": { status: 200, body: { methods: [{ kind: "token" }] } },
    });
    render(wrap());

    await waitFor(() => expect(screen.getByLabelText("Access token")).toBeTruthy());
    expect(screen.queryByLabelText("Password")).toBeNull();
    // With one method there is nothing to switch between.
    expect(screen.queryByText("Email and password")).toBeNull();
  });

  it("switches between password and token when both exist", async () => {
    stubServer({
      "GET /v1/me": UNAUTHORIZED,
      "GET /v1/setup/status": { status: 200, body: { needs_setup: false } },
      "GET /v1/auth/methods": {
        status: 200,
        body: { methods: [{ kind: "password" }, { kind: "token" }] },
      },
    });
    render(wrap());

    await waitFor(() => expect(screen.getByLabelText("Email")).toBeTruthy());
    await userEvent.click(screen.getByRole("button", { name: "Access token" }));
    expect(screen.getByLabelText("Access token")).toBeTruthy();
    expect(screen.queryByLabelText("Email")).toBeNull();
  });

  it("shows both methods when the probe itself failed", async () => {
    // A network blip must not hide the way in. Empty `methods` means "we do
    // not know", not "nothing is offered".
    stubServer({ "GET /v1/me": UNAUTHORIZED });
    render(wrap());

    await waitFor(() => expect(screen.getByLabelText("Email")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Access token" })).toBeTruthy();
  });

  it("does not sign out a reader because the server is down", async () => {
    // Only 401 means "not signed in". A 500 must render neither sign-in nor
    // setup: asking for a password that was never the problem is how a
    // reader loses work over a blip.
    //
    // Waiting for the error state rather than for the ABSENCE of the login
    // form, because absence is also true during loading — written the other
    // way round, this test passed on the first tick while the code did
    // exactly the wrong thing.
    stubServer({
      "GET /v1/me": { status: 500, body: { error: { code: "internal", message: "boom" } } },
    });
    render(wrap());

    await waitFor(() => expect(screen.getByText("The server is not responding")).toBeTruthy());
    expect(screen.queryByLabelText("Email")).toBeNull();
    expect(screen.queryByLabelText("Setup code")).toBeNull();
  });

  it("retries the identity probe when asked", async () => {
    let calls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.endsWith("/v1/me")) {
          calls += 1;
          if (calls === 1) {
            return new Response(JSON.stringify({ error: { code: "internal", message: "boom" } }), {
              status: 500,
            });
          }
          return new Response(JSON.stringify({ error: { code: "unauthorized" } }), { status: 401 });
        }
        if (url.endsWith("/v1/setup/status"))
          return new Response(JSON.stringify({ needs_setup: false }), { status: 200 });
        if (url.endsWith("/v1/auth/methods"))
          return new Response(JSON.stringify({ methods: [{ kind: "password" }] }), { status: 200 });
        return new Response("{}", { status: 404 });
      }),
    );
    render(wrap());

    await waitFor(() => expect(screen.getByText("The server is not responding")).toBeTruthy());
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    // The retry must re-run the whole probe, not just clear the error.
    await waitFor(() => expect(screen.getByLabelText("Email")).toBeTruthy());
  });

  it("sends the CSRF header on every request", async () => {
    // The server refuses a cookie-authenticated request without it, so a
    // missing header is a whole UI that silently 403s.
    stubServer({ "GET /v1/me": UNAUTHORIZED });
    render(wrap());

    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
    for (const call of vi.mocked(fetch).mock.calls) {
      const init = call[1] as RequestInit | undefined;
      const headers = (init?.headers ?? {}) as Record<string, string>;
      expect(headers["X-Requested-With"]).toBe("cc-web");
      expect(init?.credentials).toBe("same-origin");
    }
  });
});
