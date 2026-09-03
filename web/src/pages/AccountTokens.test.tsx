/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import AccountTokens from "./AccountTokens";
import { setToken, clearToken } from "../lib/auth";

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

describe("AccountTokens", () => {
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

  it("issues a token, reveals it once, lists it, and revokes it", async () => {
    let issuedCount = 0;
    let revoked = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/me/tokens")) {
        return jsonResponse(
          issuedCount === 0
            ? []
            : [{ id: "cred1", client_type: "cli", created_at: "2026-01-01T00:00:00Z", revoked_at: revoked ? "2026-01-02T00:00:00Z" : undefined }],
        );
      }
      if (method === "POST" && url.endsWith("/me/tokens")) {
        issuedCount += 1;
        return jsonResponse({ token: "cc_cred1_secret", credential_id: "cred1", client_type: "cli" }, 201);
      }
      if (method === "DELETE" && url.endsWith("/me/tokens/cred1")) {
        revoked = true;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("navigator", { clipboard: { writeText: vi.fn(async () => {}) } });

    await flush(() => root.render(<AccountTokens />));
    expect(container.textContent).toContain("No tokens yet.");

    const clientTypeInput = container.querySelector('input[placeholder^="client_type"]') as HTMLInputElement;
    await flush(() => setInputValue(clientTypeInput, "cli"));

    const issueBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Issue token")!;
    await flush(() => issueBtn.click());

    expect(container.textContent).toContain("cc_cred1_secret");
    expect(container.textContent).toContain("Copy this token now");
    expect(container.textContent).not.toContain("No tokens yet.");

    const revokeBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Revoke")!;
    await flush(() => revokeBtn.click());
    expect(revoked).toBe(false);

    const confirmBtn = Array.from(document.querySelectorAll("button")).find(
      (b) => b.textContent === "Revoke" && !container.contains(b),
    )!;
    await flush(() => confirmBtn.click());

    expect(container.textContent).toContain("revoked");
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Revoke")).toBe(false);
  });
});
