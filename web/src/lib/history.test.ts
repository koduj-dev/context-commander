import { describe, expect, it } from "vitest";
import { decodeHistory } from "./api";

describe("decodeHistory", () => {
  it("decodes what the server actually sends", () => {
    // The exact bytes /conversations/{id}/objects returned for a recorded
    // session, base64 as the handler encodes them.
    expect(decodeHistory("QWdlbnQgQTogcmVpbmRleCBzdGFydGVk")).toBe("Agent A: reindex started");
  });

  it("keeps multi-byte characters intact", () => {
    const text = "Reindex hotový — 74 %";
    const b64 = btoa(String.fromCharCode(...new TextEncoder().encode(text)));
    expect(decodeHistory(b64)).toBe(text);
  });

  // A history object is whatever a client recorded and need not be text.
  // That is a fact about the object, not an error to throw at the reader.
  it("answers null for bytes that are not UTF-8", () => {
    expect(decodeHistory(btoa("\xff\xfe\xfd"))).toBeNull();
  });

  it("answers null for something that is not base64 at all", () => {
    expect(decodeHistory("not base64 !!")).toBeNull();
  });
});
