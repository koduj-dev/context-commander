/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Handoffs from "./Handoffs";
import { setToken, clearToken } from "../../lib/auth";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function setInputValue(input: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const proto = input instanceof HTMLTextAreaElement ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, "value")!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

async function flush(fn: () => void) {
  await act(async () => {
    fn();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("Handoffs", () => {
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

  it("lets a CONTRIBUTOR create a handoff and view its detail", async () => {
    let created = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/handoffs/")) {
        return jsonResponse(created ? [{ id: "h1", status: "open", created_at: "2026-01-01T00:00:00Z" }] : []);
      }
      if (method === "POST" && url.endsWith("/handoffs/")) {
        created = true;
        return jsonResponse({
          id: "h1",
          status: "open",
          created_at: "2026-01-01T00:00:00Z",
          summary: "did the thing",
          completed_work: "wrote the code",
          next_actions: "review it",
        });
      }
      if (method === "GET" && url.endsWith("/handoffs/h1")) {
        return jsonResponse({
          id: "h1",
          status: "open",
          created_at: "2026-01-01T00:00:00Z",
          summary: "did the thing",
          completed_work: "wrote the code",
          next_actions: "review it",
        });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Handoffs namespace="ns" slug="sl" role="CONTRIBUTOR" />));
    expect(container.textContent).toContain("No handoffs yet.");

    const summary = container.querySelector('textarea[placeholder="summary"]') as HTMLTextAreaElement;
    const completed = container.querySelector('textarea[placeholder="completed work"]') as HTMLTextAreaElement;
    const next = container.querySelector('textarea[placeholder="next actions"]') as HTMLTextAreaElement;
    await flush(() => {
      setInputValue(summary, "did the thing");
      setInputValue(completed, "wrote the code");
      setInputValue(next, "review it");
    });

    const submit = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Create handoff")!;
    await flush(() => submit.click());

    expect(container.textContent).not.toContain("No handoffs yet.");
    expect(container.textContent).toContain("did the thing");
    expect(container.textContent).toContain("review it");
  });

  it("hides the create form for a READER", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse([])),
    );

    await flush(() => root.render(<Handoffs namespace="ns" slug="sl" role="READER" />));
    expect(container.querySelectorAll("form").length).toBe(1); // only the status filter form
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Create handoff")).toBe(false);
  });
});
