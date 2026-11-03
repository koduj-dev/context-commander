import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { usePolled } from "./resource";

/** A load whose answers the test hands out, in whatever order it likes. */
function deferredLoads() {
  const pending: ((value: string) => void)[] = [];
  const load = () => new Promise<string>((resolve) => pending.push(resolve));
  return { load, pending };
}

describe("usePolled", () => {
  it("keeps the newest answer when an older one arrives after it", async () => {
    const { load, pending } = deferredLoads();
    const { result, rerender } = renderHook(({ pulse }) => usePolled(load, [], 60_000, pulse), {
      initialProps: { pulse: 0 },
    });
    // The first load is in flight when a pulse starts a second read.
    rerender({ pulse: 1 });
    await waitFor(() => expect(pending.length).toBe(2));

    await act(async () => pending[1]!("new"));
    expect(result.current.data).toBe("new");
    await act(async () => pending[0]!("old"));
    expect(result.current.data).toBe("new");
    expect(result.current.loading).toBe(false);
  });
});
