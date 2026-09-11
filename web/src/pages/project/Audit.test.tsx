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

  // The button follows next_cursor, so a short first page still offers it —
  // and the cursor the UI sends back is the server's own opaque token, never
  // something the client derives from a row.
  it("shows Load more while the server offers a cursor, and echoes it back verbatim", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      const cursor = url.searchParams.get("cursor");
      if (!cursor) {
        return jsonResponse({ items: [record(200), record(199)], next_cursor: "opaque-cursor-1" });
      }
      return jsonResponse({ items: [record(50), record(49)] });
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
    expect(calledUrl.searchParams.get("cursor")).toBe("opaque-cursor-1");
  });

  it("does not show Load more when the server sends no cursor", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ items: [record(1)] })),
    );

    await flush(() => root.render(<Audit namespace="ns" slug="sl" />));
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Load more")).toBe(false);
  });
});
