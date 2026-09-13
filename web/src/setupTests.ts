/**
 * Test environment setup.
 *
 * `fetch` is deliberately left unstubbed globally: each test that talks to
 * the API stubs it itself with the exact status it is about, so a test that
 * accidentally makes an unexpected request fails loudly rather than getting a
 * blanket 200.
 */
import { afterEach, vi } from "vitest";

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});
