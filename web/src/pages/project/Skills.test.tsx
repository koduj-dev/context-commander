/** @vitest-environment happy-dom */
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import Skills from "./Skills";
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

describe("Skills", () => {
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

  it("lets a CONTRIBUTOR publish a skill and pre-fills the install form from it", async () => {
    let installed = false;
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/skills/effective")) {
        return jsonResponse(
          installed
            ? [
                {
                  id: "bind1",
                  scope: "project",
                  skill_id: "skill-123",
                  skill_version_id: "ver-456",
                  pinned: false,
                  managed: false,
                  overridable: true,
                },
              ]
            : [],
        );
      }
      if (method === "GET" && url.endsWith("/")) {
        return jsonResponse({ id: "p1", namespace: "ns", slug: "sl", name: "Proj", visibility: "private" });
      }
      if (method === "POST" && url.endsWith("/skills")) {
        return jsonResponse({
          id: "ver-456",
          skill_id: "skill-123",
          version: 1,
          checksum: "abc",
          declared_capabilities: [],
          trust_status: "trusted",
        });
      }
      if (method === "POST" && url.endsWith("/skills/install")) {
        installed = true;
        return jsonResponse({
          id: "bind1",
          scope: "project",
          skill_id: "skill-123",
          skill_version_id: "ver-456",
          pinned: false,
          managed: false,
          overridable: true,
        });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Skills namespace="ns" slug="sl" role="CONTRIBUTOR" />));
    expect(container.textContent).toContain("No skills installed.");

    const nameInput = container.querySelector('input[placeholder="name"]') as HTMLInputElement;
    const authorInput = container.querySelector('input[placeholder="author"]') as HTMLInputElement;
    const contentArea = container.querySelector('textarea[placeholder="skill content"]') as HTMLTextAreaElement;
    await flush(() => {
      setInputValue(nameInput, "my-skill");
      setInputValue(authorInput, "alice");
      setInputValue(contentArea, "do the thing");
    });

    const publishBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Publish")!;
    expect(publishBtn.hasAttribute("disabled")).toBe(false);
    await flush(() => publishBtn.click());

    const skillIdInput = container.querySelector('input[placeholder="skill_id"]') as HTMLInputElement;
    const skillVersionInput = container.querySelector('input[placeholder="skill_version_id"]') as HTMLInputElement;
    expect(skillIdInput.value).toBe("skill-123");
    expect(skillVersionInput.value).toBe("ver-456");

    const installBtn = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Install")!;
    await flush(() => installBtn.click());
    expect(container.textContent).not.toContain("No skills installed.");
    expect(Array.from(container.querySelectorAll("button")).some((b) => b.textContent === "Pin")).toBe(true);
  });

  it("hides the publish/install forms for a READER", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "GET" && url.endsWith("/skills/effective")) return jsonResponse([]);
      if (method === "GET" && url.endsWith("/")) {
        return jsonResponse({ id: "p1", namespace: "ns", slug: "sl", name: "Proj", visibility: "private" });
      }
      throw new Error(`unexpected fetch ${method} ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    await flush(() => root.render(<Skills namespace="ns" slug="sl" role="READER" />));
    expect(container.querySelectorAll("form").length).toBe(0);
  });
});
