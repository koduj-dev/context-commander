/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Events from "./Events";
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

function event(seq: number) {
  return { seq, event_id: `e${seq}`, type: "artifact.written", resource_type: "artifact", payload_kind: "metadata_only" };
}

describe("Events", () => {
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

  // "Load more" is driven by next_cursor, not by whether a page came back
  // full — so the first page here is deliberately NOT full (2 rows) yet still
  // offers a cursor. Under the old page-size guess this case showed no button
  // and the remaining rows were unreachable.
  it("shows Load more while the server offers a cursor, and appends the next page", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      const cursor = url.searchParams.get("cursor");
      if (cursor === null) {
        return jsonResponse({ items: [event(1), event(2)], next_cursor: "cursor-page-2" });
      }
      return jsonResponse({ items: [event(101), event(102)] });
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Events namespace="ns" slug="sl" />));

    let loadMore = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load more");
    expect(loadMore).toBeTruthy();

    await flush(() => loadMore!.click());

    expect(container.textContent).toContain("102");
    loadMore = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load more");
    expect(loadMore).toBeUndefined();
  });

  // The mirror case: a page that happens to be exactly the page size but has
  // nothing behind it must not offer the button. That is what the old
  // `page.length >= PAGE_SIZE` check got wrong.
  it("hides Load more on a full page with no cursor", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ items: Array.from({ length: 100 }, (_, i) => event(i + 1)) })),
    );

    await flush(() => root.render(<Events namespace="ns" slug="sl" />));
    const loadMore = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load more");
    expect(loadMore).toBeUndefined();
  });

  it("shows an empty state with no events", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ items: [] })),
    );

    await flush(() => root.render(<Events namespace="ns" slug="sl" />));
    expect(container.textContent).toContain("No events yet.");
  });
});
