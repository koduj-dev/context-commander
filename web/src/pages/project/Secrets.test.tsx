/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Secrets from "./Secrets";
import { setToken, clearToken } from "../../lib/auth";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function setInputValue(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

async function flush(fn: () => void) {
  await act(async () => {
    fn();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("Secrets", () => {
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

  it("creates a secret, then requires confirmation before deleting it", async () => {
    let created = false;
    let deleted = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      // Paginated list URLs now carry ?limit=…, so route on the path only.
      const url = String(input).split("?")[0];
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/secrets")) {
        return jsonResponse({ items: created && !deleted ? [{ name: "api-key", reference: "secret://ns/sl/api-key" }] : [] });
      }
      if (method === "POST" && url.endsWith("/secrets")) {
        created = true;
        return jsonResponse({ name: "api-key", reference: "secret://ns/sl/api-key" }, 201);
      }
      if (method === "DELETE" && url.includes("/secrets/")) {
        deleted = true;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Secrets namespace="ns" slug="sl" />));
    expect(container.textContent).toContain("No secrets yet.");

    const nameInput = container.querySelector('input[placeholder="secret name"]') as HTMLInputElement;
    const valueInput = container.querySelector('input[placeholder="value"]') as HTMLInputElement;
    await flush(() => setInputValue(nameInput, "api-key"));
    await flush(() => setInputValue(valueInput, "s3cr3t"));

    const createBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Create secret")!;
    await flush(() => createBtn.click());
    expect(container.textContent).toContain("secret://ns/sl/api-key");

    const deleteBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Delete")!;
    await flush(() => deleteBtn.click());

    // Clicking Delete opens a confirm dialog rather than deleting immediately.
    expect(container.textContent).toContain("secret://ns/sl/api-key");
    expect(deleted).toBe(false);

    const confirmBtn = Array.from(document.querySelectorAll("button")).find(
      (b) => b.textContent === "Delete" && !container.contains(b),
    )!;
    await flush(() => confirmBtn.click());

    expect(deleted).toBe(true);
    expect(container.textContent).toContain("No secrets yet.");
  });
});
