/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Shell from "./Shell";

const logout = vi.fn();
vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({ user: { email: "shell-test@example.test" }, logout, loading: false, login: vi.fn() }),
}));
vi.mock("../theme/ThemeContext", () => ({
  useTheme: () => ({ theme: "dark", setTheme: vi.fn(), resolvedTheme: "dark" }),
}));

describe("Shell", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    logout.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it("renders the signed-in user's email and nested route content", () => {
    act(() => {
      root.render(
        <MemoryRouter initialEntries={["/"]}>
          <Routes>
            <Route element={<Shell />}>
              <Route path="/" element={<div data-testid="page">page content</div>} />
            </Route>
          </Routes>
        </MemoryRouter>,
      );
    });

    expect(container.textContent).toContain("shell-test@example.test");
    expect(container.textContent).toContain("page content");
  });

  it("calls logout() when the sign-out button is clicked", () => {
    act(() => {
      root.render(
        <MemoryRouter initialEntries={["/"]}>
          <Routes>
            <Route element={<Shell />}>
              <Route path="/" element={<div>page content</div>} />
            </Route>
          </Routes>
        </MemoryRouter>,
      );
    });

    const button = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Sign out")!;
    act(() => {
      button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(logout).toHaveBeenCalled();
  });
});
