import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, LOCALES, LOCALE_NAMES } from "../i18n";
import { AuthProvider } from "./auth";
import { CrumbsProvider } from "./crumbs";
import { PrefsProvider } from "./prefs";
import { ThemeProvider } from "./theme";
import { Shell } from "./Shell";
import { SignIn } from "../pages/SignIn";
import { Preferences } from "../pages/Preferences";

/**
 * Every locale that exists must be reachable.
 *
 * Written because it was not: the shell's language switcher listed three
 * locales written out by hand, and three more were added to LOCALES without
 * it changing. Nothing failed — the catalogues were complete, the checks were
 * green, the code compiled — and the languages simply were not in the
 * interface. A list duplicated from a constant is invisible to every other
 * kind of check, so this one renders the controls and counts.
 */
function stubMe() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/me/projects")) return new Response("[]", { status: 200 });
      if (url.endsWith("/v1/me")) {
        return new Response(
          JSON.stringify({
            principal_id: "p-1",
            kind: "user",
            email: "someone@example.test",
            display_name: "Someone",
            is_installation_admin: false,
          }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 404 });
    }),
  );
}

function wrap(children: React.ReactNode) {
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

/** Every language named in itself, as the switchers show them. */
const nativeNames = LOCALES.map((l) => LOCALE_NAMES[l]);

/**
 * Opened, not inspected.
 *
 * The control is a listbox rather than a native `<select>`, so its options do
 * not exist in the document until it is opened — which makes this the
 * stronger test: it proves the thing a person does actually works, rather
 * than that some markup is present.
 */
async function openedOptions(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByLabelText("Interface language"));
  const list = await screen.findByRole("listbox", { name: "Interface language" });
  return [...list.querySelectorAll('[role="option"]')].map((o) => o.textContent);
}

describe("language switchers offer every locale", () => {
  beforeEach(stubMe);

  it("in the application shell", async () => {
    const user = userEvent.setup();
    render(wrap(<Shell />));
    expect(await openedOptions(user)).toEqual(nativeNames);
  });

  it("on the sign-in screen", async () => {
    const user = userEvent.setup();
    render(wrap(<SignIn />));
    expect(await openedOptions(user)).toEqual(nativeNames);
  });

  it("on the preferences screen", async () => {
    const user = userEvent.setup();
    render(wrap(<Preferences />));
    expect(await openedOptions(user)).toEqual(nativeNames);
  });

  it("switches the interface when one is chosen", async () => {
    const user = userEvent.setup();
    render(wrap(<SignIn />));

    // English to begin with.
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();

    await user.click(screen.getByLabelText("Interface language"));
    await user.click(await screen.findByRole("option", { name: "Français" }));

    // The catalogue is loaded on demand, so the switch is asynchronous.
    expect(await screen.findByRole("button", { name: "Se connecter" })).toBeTruthy();
    expect(document.documentElement.lang).toBe("fr");
  });
});
