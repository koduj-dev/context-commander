/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Conversations from "./Conversations";
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

describe("Conversations", () => {
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

  it("lets a CONTRIBUTOR create a conversation, a session, and append a decoded history object", async () => {
    let objects: unknown[] = [];
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "POST" && url.endsWith("/conversations/")) {
        return jsonResponse({ id: "c1", created_at: "2026-01-01T00:00:00Z" });
      }
      if (method === "POST" && url.endsWith("/conversations/c1/sessions")) {
        return jsonResponse({
          id: "s1",
          conversation_id: "c1",
          provider: "anthropic",
          client: "claude-code",
          provider_session_id: "prov-1",
          relation: "started",
          started_at: "2026-01-01T00:00:00Z",
        });
      }
      if (method === "POST" && url.endsWith("/conversations/c1/objects")) {
        objects = [
          {
            id: "o1",
            external_session_id: "s1",
            type: "raw",
            content_hash: "abc123",
            content: btoa("hello world"),
            created_at: "2026-01-01T00:00:00Z",
          },
        ];
        return jsonResponse(objects[0]);
      }
      if (method === "GET" && url.endsWith("/conversations/c1/objects")) {
        return jsonResponse(objects);
      }
      if (method === "GET" && url.endsWith("/conversations/c1/lineage")) {
        return jsonResponse([]);
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Conversations namespace="ns" slug="sl" role="CONTRIBUTOR" />));

    const newConversationBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "New conversation")!;
    await flush(() => newConversationBtn.click());
    expect(container.textContent).toContain("c1");

    const providerInput = container.querySelector('input[placeholder^="provider ("]') as HTMLInputElement;
    const clientInput = container.querySelector('input[placeholder^="client ("]') as HTMLInputElement;
    const providerSessionInput = container.querySelector('input[placeholder="provider_session_id"]') as HTMLInputElement;
    await flush(() => {
      setInputValue(providerInput, "anthropic");
      setInputValue(clientInput, "claude-code");
      setInputValue(providerSessionInput, "prov-1");
    });

    const createSessionBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Create session")!;
    await flush(() => createSessionBtn.click());
    expect(container.textContent).toContain("anthropic/claude-code");

    const contentArea = container.querySelector('textarea[placeholder="content"]') as HTMLTextAreaElement;
    await flush(() => setInputValue(contentArea, "hello world"));

    const appendBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Append object")!;
    await flush(() => appendBtn.click());

    expect(container.textContent).toContain("hello world");
    expect(container.textContent).not.toContain("No history objects yet.");
  });
});
