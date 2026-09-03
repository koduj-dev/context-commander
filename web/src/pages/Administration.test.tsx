/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Administration from "./Administration";
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

const TARGET_PRINCIPAL = "11111111-1111-1111-1111-111111111111";

describe("Administration", () => {
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

  it("lists admins, creates a user, registers a service principal, and issues/revokes a token for a principal", async () => {
    let issuedCount = 0;
    let revoked = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/installation/admins")) {
        return jsonResponse([{ user_id: "admin-1", role: "ADMIN", granted_at: "2026-01-01T00:00:00Z" }]);
      }
      if (method === "POST" && url.endsWith("/installation/users")) {
        return jsonResponse({ id: "u1", email: "new@example.test", display_name: "New User", created_at: "2026-01-01T00:00:00Z" }, 201);
      }
      if (method === "POST" && url.endsWith("/installation/service-principals")) {
        return jsonResponse({ id: "sp1", name: "ci-agent", created_at: "2026-01-01T00:00:00Z" }, 201);
      }
      if (method === "GET" && url.endsWith(`/installation/principals/${TARGET_PRINCIPAL}/tokens`)) {
        return jsonResponse(
          issuedCount === 0
            ? []
            : [{ id: "cred1", client_type: "cli", created_at: "2026-01-01T00:00:00Z", revoked_at: revoked ? "2026-01-02T00:00:00Z" : undefined }],
        );
      }
      if (method === "POST" && url.endsWith(`/installation/principals/${TARGET_PRINCIPAL}/tokens`)) {
        issuedCount += 1;
        return jsonResponse({ token: "cc_cred1_secret", credential_id: "cred1", client_type: "cli" }, 201);
      }
      if (method === "DELETE" && url.endsWith("/installation/tokens/cred1")) {
        revoked = true;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Administration />));
    expect(container.textContent).toContain("admin-1");

    // create user
    const emailInput = container.querySelector('input[placeholder="email"]') as HTMLInputElement;
    const nameInput = container.querySelector('input[placeholder="display name"]') as HTMLInputElement;
    await flush(() => {
      setInputValue(emailInput, "new@example.test");
      setInputValue(nameInput, "New User");
    });
    const createUserBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Create user")!;
    await flush(() => createUserBtn.click());
    expect(container.textContent).toContain("new@example.test");

    // register service principal
    const spNameInput = container.querySelector('input[placeholder="name"]') as HTMLInputElement;
    await flush(() => setInputValue(spNameInput, "ci-agent"));
    const registerBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Register")!;
    await flush(() => registerBtn.click());
    expect(container.textContent).toContain("ci-agent");

    // load credentials for a principal
    const lookupInput = container.querySelector('input[placeholder="principal id (UUID)"]') as HTMLInputElement;
    await flush(() => setInputValue(lookupInput, TARGET_PRINCIPAL));
    const loadBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Load")!;
    await flush(() => loadBtn.click());
    expect(container.textContent).toContain("No tokens for this principal.");

    // issue a token
    const clientTypeInput = container.querySelector('input[placeholder^="client_type"]') as HTMLInputElement;
    await flush(() => setInputValue(clientTypeInput, "cli"));
    const issueBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Issue token")!;
    await flush(() => issueBtn.click());
    expect(container.textContent).toContain("cc_cred1_secret");
    expect(container.textContent).not.toContain("No tokens for this principal.");

    // revoke it, through the confirm dialog
    const revokeBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Revoke")!;
    await flush(() => revokeBtn.click());
    expect(revoked).toBe(false);

    const confirmRevokeBtn = Array.from(document.querySelectorAll("button")).find(
      (b) => b.textContent === "Revoke" && !container.contains(b),
    )!;
    await flush(() => confirmRevokeBtn.click());
    expect(revoked).toBe(true);
    expect(container.textContent).toContain("revoked");
  });
});
