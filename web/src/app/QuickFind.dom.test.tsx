import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n";
import { QuickFind } from "./QuickFind";

type Plans = { status: number; body: unknown };

function stub(plans: Plans) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
      if (url.endsWith("/v1/me/projects"))
        return json([{ namespace: "acme", slug: "rotation", name: "Rotation", role: "OWNER" }]);
      if (url.includes("/v1/skills")) return json({ items: [] });
      if (url.includes("/v1/me/plans/search")) return json(plans.body, plans.status);
      return json({}, 404);
    }),
  );
}

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function wrap() {
  return (
    <I18nProvider>
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route
            path="*"
            element={
              <>
                <QuickFind onClose={() => {}} />
                <Where />
              </>
            }
          />
        </Routes>
      </MemoryRouter>
    </I18nProvider>
  );
}

function type(text: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
}

describe("plans in the quick finder", () => {
  it("goes to the plan and step that matched", async () => {
    stub({
      status: 200,
      body: {
        items: [
          {
            project: "acme/work",
            project_name: "Work",
            plan_id: "plan-1",
            plan_title: "Release",
            plan_status: "abandoned",
            item_id: "it-1",
            text: "Rotate the keys",
            status: "open",
          },
        ],
        truncated: false,
      },
    });
    render(wrap());
    type("rotate");
    fireEvent.click(await waitFor(() => screen.getByText("Rotate the keys")));
    expect(screen.getByTestId("where").textContent).toBe(
      "/p/acme/work/plans?plan=plan-1&item=it-1",
    );
  });

  it("says a bounded search was cut, even with no hits", async () => {
    stub({ status: 200, body: { items: [], truncated: true } });
    render(wrap());
    type("nothing like this");
    expect(await waitFor(() => screen.getByText(/Plan search stopped early/))).toBeTruthy();
    expect(screen.queryByText("Nothing matches that")).toBeNull();
  });

  it("reports a failed plan search on its own and keeps the other results", async () => {
    stub({ status: 500, body: { error: { code: "internal", message: "internal error" } } });
    render(wrap());
    type("rotation");
    expect(
      await waitFor(() =>
        screen.getByText("Plans could not be searched. The other results are shown."),
      ),
    ).toBeTruthy();
    // The project still matches.
    expect(screen.getByText("Rotation")).toBeTruthy();
  });
});
