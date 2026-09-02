/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Gates from "./Gates";
import { setToken, clearToken } from "../../lib/auth";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

async function flush(root: Root, container: HTMLDivElement, element: React.ReactElement) {
  await act(async () => {
    root.render(element);
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return container;
}

describe("Gates", () => {
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

  it("lets an ADMIN bind and unbind a gate", async () => {
    let bound = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/gates/bindings")) {
        return jsonResponse(
          bound ? [{ id: "b1", scope: "project", gate_name: "secret-scanner", hook: "artifact.before_write" }] : [],
        );
      }
      if (method === "GET" && url.endsWith("/")) {
        return jsonResponse({ id: "p1", namespace: "ns", slug: "sl", name: "Proj", visibility: "private" });
      }
      if (method === "POST" && url.endsWith("/gates/bindings")) {
        bound = true;
        return jsonResponse({ id: "b1", scope: "project", gate_name: "secret-scanner", hook: "artifact.before_write" });
      }
      if (method === "DELETE" && url.includes("/gates/bindings")) {
        bound = false;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(root, container, <Gates namespace="ns" slug="sl" role="ADMIN" />);
    expect(container.textContent).toContain("No gates bound.");

    const submit = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Bind gate")!;
    await act(async () => {
      submit.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(container.textContent).toContain("secret-scanner");

    const unbind = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Unbind")!;
    await act(async () => {
      unbind.click();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(container.textContent).toContain("No gates bound.");
  });

  it("hides the bind form and unbind button for a READER", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/gates/bindings")) {
        return jsonResponse([{ id: "b1", scope: "project", gate_name: "secret-scanner", hook: "artifact.before_write" }]);
      }
      if (method === "GET" && url.endsWith("/")) {
        return jsonResponse({ id: "p1", namespace: "ns", slug: "sl", name: "Proj", visibility: "private" });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(root, container, <Gates namespace="ns" slug="sl" role="READER" />);
    expect(container.textContent).toContain("secret-scanner");
    expect(container.querySelector("form")).toBeNull();
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Unbind")).toBe(false);
  });
});
