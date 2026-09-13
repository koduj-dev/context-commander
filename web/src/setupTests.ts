/**
 * Test environment setup.
 *
 * `fetch` is deliberately left unstubbed globally: each test that talks to
 * the API stubs it itself with the exact status it is about, so a test that
 * accidentally makes an unexpected request fails loudly rather than getting a
 * blanket 200.
 */
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(() => {
  // Testing Library only registers its own cleanup when vitest runs with
  // `globals: true`, which this project does not. Without it every rendered
  // tree stays in the document and the next test's queries find elements
  // from the previous one — which is exactly how a passing screen was made
  // to look broken.
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  localStorage.clear();
});
