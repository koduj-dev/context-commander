/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Login from "./Login";

const login = vi.fn();
vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({ login, loading: false, user: null, logout: vi.fn() }),
}));

// React tracks a controlled input's value via a property setter it installs
// on the DOM node itself, so a plain `input.value = "x"` is invisible to it
// — the change never reaches onChange. Going through the native
// HTMLInputElement setter first (as React Testing Library's fireEvent does
// internally) is what actually triggers React's change detection.
function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("Login", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    login.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it("renders a token field and a disabled submit button until something is typed", () => {
    act(() => {
      root.render(
        <MemoryRouter>
          <Login />
        </MemoryRouter>,
      );
    });

    const input = container.querySelector("#token") as HTMLInputElement;
    const button = container.querySelector("button[type=submit]") as HTMLButtonElement;
    expect(input).toBeTruthy();
    expect(button.disabled).toBe(true);

    act(() => {
      setInputValue(input, "cc_test_token");
    });

    expect(button.disabled).toBe(false);
  });

  it("calls login() with the trimmed token on submit", async () => {
    login.mockResolvedValue(undefined);
    act(() => {
      root.render(
        <MemoryRouter>
          <Login />
        </MemoryRouter>,
      );
    });

    const input = container.querySelector("#token") as HTMLInputElement;
    act(() => {
      setInputValue(input, "  cc_test_token  ");
    });

    const form = container.querySelector("form") as HTMLFormElement;
    await act(async () => {
      form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });

    expect(login).toHaveBeenCalledWith("cc_test_token");
  });
});
