import { afterEach, describe, expect, it, vi } from "vitest";
import { modifierLabel } from "./platform";

function pretendPlatform(platform: string) {
  vi.spyOn(navigator, "platform", "get").mockReturnValue(platform);
}

afterEach(() => vi.restoreAllMocks());

describe("modifierLabel", () => {
  it("names the Command key on Apple keyboards", () => {
    pretendPlatform("MacIntel");
    expect(modifierLabel("K")).toBe("⌘K");
  });

  // The bug this exists for: a hint reading ⌘K tells someone on Windows to
  // press a key their keyboard does not have.
  it("names Ctrl everywhere else", () => {
    pretendPlatform("Win32");
    expect(modifierLabel("K")).toBe("Ctrl K");
    pretendPlatform("Linux x86_64");
    expect(modifierLabel("K")).toBe("Ctrl K");
  });

  it("falls back to Ctrl when the platform is unknown", () => {
    pretendPlatform("");
    expect(modifierLabel("K")).toBe("Ctrl K");
  });

  it("prefers userAgentData when the browser provides it", () => {
    // A Chromium browser freezes navigator.platform for privacy and reports
    // the real one here instead.
    pretendPlatform("Win32");
    Object.defineProperty(navigator, "userAgentData", {
      value: { platform: "macOS" },
      configurable: true,
    });
    expect(modifierLabel("K")).toBe("⌘K");
    Reflect.deleteProperty(navigator, "userAgentData");
  });
});
