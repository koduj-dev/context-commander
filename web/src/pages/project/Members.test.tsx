/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Members from "./Members";
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

const MEMBERS = [
  { principal_id: "p-owner", role: "OWNER", created_at: "2026-01-01T00:00:00Z", email: "owner@example.test" },
  { principal_id: "p-reader", role: "READER", created_at: "2026-01-02T00:00:00Z", email: "reader@example.test" },
];

describe("Members", () => {
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

  it("shows the member list to a READER, without an add form", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(MEMBERS)),
    );

    await flush(() => root.render(<Members namespace="ns" slug="sl" role="READER" />));

    expect(container.textContent).toContain("owner@example.test");
    expect(container.textContent).toContain("reader@example.test");
    expect(container.querySelector("form")).toBeNull();
  });

  it("does not offer the OWNER role to an ADMIN, but does to an OWNER", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse(MEMBERS)),
    );

    // Granting OWNER requires OWNER server-side (internal/members
    // Service.Add); offering the option to an ADMIN would only yield a 403.
    await flush(() => root.render(<Members namespace="ns" slug="sl" role="ADMIN" />));
    const adminRoles = Array.from(container.querySelectorAll("option")).map((o) => o.value);
    expect(adminRoles).toEqual(["ADMIN", "CONTRIBUTOR", "READER"]);

    await flush(() => root.render(<Members namespace="ns" slug="sl" role="OWNER" />));
    const ownerRoles = Array.from(container.querySelectorAll("option")).map((o) => o.value);
    expect(ownerRoles).toContain("OWNER");
  });

  it("shows the add form to an ADMIN and refreshes the list after adding", async () => {
    let added = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/members")) {
        return jsonResponse(added ? [...MEMBERS, { principal_id: "p-new", role: "READER", created_at: "2026-01-03T00:00:00Z" }] : MEMBERS);
      }
      if (method === "POST" && url.endsWith("/members")) {
        added = true;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Members namespace="ns" slug="sl" role="ADMIN" />));
    expect(container.querySelector("form")).not.toBeNull();

    const input = container.querySelector('input[placeholder="principal id (UUID)"]') as HTMLInputElement;
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!;
    await flush(() => {
      setter.call(input, "p-new");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });

    const submit = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Add member")!;
    await flush(() => submit.click());

    expect(container.textContent).toContain("p-new");
  });
});
