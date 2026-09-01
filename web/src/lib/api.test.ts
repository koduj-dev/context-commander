/** @vitest-environment happy-dom */
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { ApiError, api } from "./api";
import { clearToken, setToken } from "./auth";

describe("api error mapping", () => {
  beforeEach(() => {
    clearToken();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("throws ApiError with the server's code/message on a non-2xx JSON response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ error: { code: "forbidden", message: "nope" } }), {
          status: 403,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(api.me()).rejects.toMatchObject({ status: 403, code: "forbidden", message: "nope" });
    await expect(api.me()).rejects.toBeInstanceOf(ApiError);
  });

  it("clears the stored token on a 401", async () => {
    setToken("cc_something");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ error: { code: "unauthorized", message: "invalid token" } }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(api.me()).rejects.toBeInstanceOf(ApiError);
    expect(localStorage.getItem("cc_token")).toBeNull();
  });

  it("attaches the stored token as a bearer header", async () => {
    setToken("cc_mytoken");
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    await api.me();

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer cc_mytoken");
  });
});
