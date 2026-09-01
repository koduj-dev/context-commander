/** @vitest-environment happy-dom */
import { describe, expect, it, beforeEach } from "vitest";
import { clearToken, getToken, setToken } from "./auth";

describe("auth token storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("returns null when nothing is stored", () => {
    expect(getToken()).toBeNull();
  });

  it("round-trips a token through set/get", () => {
    setToken("cc_abc_def");
    expect(getToken()).toBe("cc_abc_def");
  });

  it("clears a stored token", () => {
    setToken("cc_abc_def");
    clearToken();
    expect(getToken()).toBeNull();
  });
});
