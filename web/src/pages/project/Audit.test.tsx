/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Audit from "./Audit";
import { setToken, clearToken } from "../../lib/auth";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

async function flush(fn: () => void) {
  await act(async () => {
    fn();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function record(id: number) {
  return { id, timestamp: "2026-01-01T00:00:00Z", action: `action-${id}`, resource_type: "artifact", result: "allow" };
}

describe("Audit", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    setToken("cc_test");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    clearToken();
    vi.unstubAllGlobals();
  });

  it("shows Load more only when a full page came back, and appends the next page", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      const beforeID = url.searchParams.get("before_id");
      if (!beforeID) {
        // first page: exactly PAGE_SIZE (100) rows, so "Load more" should appear
        return jsonResponse(Array.from({ length: 100 }, (_, i) => record(200 - i)));
      }
      // second page: fewer than PAGE_SIZE, so "Load more" should then disappear
      return jsonResponse([record(50), record(49)]);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Audit namespace="ns" slug="sl" />));

    let loadMore = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load more");
    expect(loadMore).toBeTruthy();

    await flush(() => loadMore!.click());

    expect(container.textContent).toContain("action-49");
    loadMore = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load more");
    expect(loadMore).toBeUndefined();

    const lastCall = fetchMock.mock.calls[fetchMock.mock.calls.length - 1];
    const calledUrl = new URL(String(lastCall[0]), "http://localhost");
    expect(calledUrl.searchParams.get("before_id")).toBe("101");
  });

  it("does not show Load more when the first page is short", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse([record(1)])),
    );

    await flush(() => root.render(<Audit namespace="ns" slug="sl" />));
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Load more")).toBe(false);
  });
});
