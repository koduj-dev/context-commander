import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { AuthProvider } from "../app/auth";
import { CrumbsProvider } from "../app/crumbs";
import { PrefsProvider } from "../app/prefs";
import { ThemeProvider } from "../app/theme";
import type { AuthMethod } from "../lib/api";
import { SignIn } from "./SignIn";

/**
 * Single sign-on on the sign-in screen.
 *
 * The provider buttons come from /v1/auth/methods and nowhere else, and the
 * callback reports a failure by landing here with `?sign_in_error=`. Both
 * halves are only visible running, so this renders the screen against a
 * stubbed server rather than asserting on the component's props.
 */
function stubServer(methods: AuthMethod[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/v1/me")) {
        return new Response(JSON.stringify({ error: "unauthorized", message: "no credential" }), {
          status: 401,
        });
      }
      if (url.endsWith("/v1/auth/methods")) {
        return new Response(JSON.stringify({ methods }), { status: 200 });
      }
      if (url.endsWith("/v1/setup/status")) {
        return new Response(JSON.stringify({ needs_setup: false }), { status: 200 });
      }
      return new Response("{}", { status: 404 });
    }),
  );
}

function renderSignIn() {
  return render(
    <I18nProvider>
      <PrefsProvider>
        <ThemeProvider>
          <AuthProvider>
            <MemoryRouter>
              <CrumbsProvider>
                <SignIn />
              </CrumbsProvider>
            </MemoryRouter>
          </AuthProvider>
        </ThemeProvider>
      </PrefsProvider>
    </I18nProvider>,
  );
}

const withKeycloak: AuthMethod[] = [
  { kind: "password" },
  { kind: "token" },
  { kind: "oidc", id: "keycloak", label: "Keycloak" },
];

describe("single sign-on on the sign-in screen", () => {
  beforeEach(() => window.history.replaceState(null, "", "/"));
  afterEach(() => vi.restoreAllMocks());

  it("offers each configured provider and sends the browser to it", async () => {
    stubServer(withKeycloak);
    const assign = vi.spyOn(window.location, "assign").mockImplementation(() => {});
    const user = userEvent.setup();
    renderSignIn();

    await user.click(await screen.findByRole("button", { name: "Sign in with Keycloak" }));
    expect(assign).toHaveBeenCalledWith("/v1/auth/oidc/keycloak/start");
  });

  it("offers no provider when none is configured", async () => {
    stubServer([{ kind: "password" }, { kind: "token" }]);
    renderSignIn();
    // The form renders once the methods have arrived; by then a provider
    // button would have too.
    await screen.findByRole("button", { name: "Sign in" });
    expect(screen.queryByRole("button", { name: /Sign in with/ })).toBeNull();
  });

  it("says why the provider's sign-in failed, once", async () => {
    window.history.replaceState(null, "", "/?sign_in_error=oidc_email_unverified");
    stubServer(withKeycloak);
    renderSignIn();

    expect(
      await screen.findByText(/Your email address at the sign-in provider is not verified/),
    ).toBeTruthy();
    // Gone from the address bar, so a reload does not repeat a failure from
    // an attempt long finished.
    expect(window.location.search).toBe("");
  });

  it("still reports a failure whose code it does not know", async () => {
    window.history.replaceState(null, "", "/?sign_in_error=something_new");
    stubServer(withKeycloak);
    renderSignIn();
    expect(
      await screen.findByText("Sign-in through the provider did not succeed. Start again."),
    ).toBeTruthy();
  });
});
