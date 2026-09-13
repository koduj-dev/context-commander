import { describe, expect, it } from "vitest";
import { bytes, date, stamp, time, utcStamp } from "./format";

const SAMPLE = "2026-09-13T12:05:22Z";

describe("date", () => {
  it("writes the reader's chosen order regardless of language", () => {
    expect(date(SAMPLE, "en", "ymd", "UTC")).toBe("2026-09-13");
    expect(date(SAMPLE, "de", "ymd", "UTC")).toBe("2026-09-13");
    expect(date(SAMPLE, "en", "dmy", "UTC")).toBe("13-09-2026");
    expect(date(SAMPLE, "en", "mdy", "UTC")).toBe("09-13-2026");
  });

  it("follows the language when no order is chosen", () => {
    // Not asserting the exact string — that is Intl's call and it varies by
    // ICU version. What must hold is that the two languages differ.
    expect(date(SAMPLE, "en", "locale", "UTC")).not.toBe(date(SAMPLE, "de", "locale", "UTC"));
  });

  it("resolves the day in the reader's zone, not the server's", () => {
    // 23:30 UTC is already the next day in Prague.
    expect(date("2026-09-13T23:30:00Z", "en", "ymd", "Europe/Prague")).toBe("2026-09-14");
    expect(date("2026-09-13T23:30:00Z", "en", "ymd", "UTC")).toBe("2026-09-13");
  });
});

describe("time", () => {
  it("honours an explicit clock convention over the language's", () => {
    expect(time(SAMPLE, "en", "h24", "UTC")).toBe("12:05");
    expect(time(SAMPLE, "en", "h12", "UTC")).toMatch(/12:05\s?PM/);
  });
});

describe("stamp", () => {
  it("combines both overrides, which a single Intl call cannot", () => {
    expect(stamp(SAMPLE, "en", "ymd", "h24", "UTC")).toBe("2026-09-13 12:05");
  });
});

describe("bytes", () => {
  it("writes the same integer in whichever system the reader prefers", () => {
    expect(bytes(2_400_000_000, "en", "decimal")).toBe("2.40 GB");
    expect(bytes(2_400_000_000, "en", "binary")).toBe("2.24 GiB");
  });

  it("groups and separates the way the language does", () => {
    expect(bytes(2_400_000_000, "de", "decimal")).toBe("2,40 GB");
  });

  it("never writes a fraction of a byte", () => {
    expect(bytes(512, "en")).toBe("512 B");
  });
});

describe("utcStamp", () => {
  it("is the shared reference an audit reader can correlate on", () => {
    expect(utcStamp(SAMPLE)).toBe("2026-09-13 12:05 UTC");
  });
});
