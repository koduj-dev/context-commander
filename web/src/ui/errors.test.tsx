import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "../i18n";
import { ApiError } from "../lib/api";
import { ErrorState } from "./index";

/**
 * Rendered as static markup rather than through a testing library: the
 * assertions here are about what reaches the reader, and that needs no DOM,
 * no user events and no extra dependency.
 */
function html(node: React.ReactNode) {
  return renderToStaticMarkup(<I18nProvider>{node}</I18nProvider>);
}

describe("ErrorState", () => {
  // The mistake this exists to prevent: rendering a refusal as an empty list,
  // which tells someone their project is empty when the truth is that it is
  // not theirs.
  it("says a refusal is a refusal", () => {
    const out = html(<ErrorState error={new ApiError("forbidden", 403, "forbidden", "nope")} />);
    expect(out).toContain("You do not have access to this");
    expect(out).not.toContain("Nothing matches that");
  });

  it("distinguishes not-found from forbidden", () => {
    const out = html(<ErrorState error={new ApiError("not_found", 404, "not_found", "nope")} />);
    expect(out).toContain("Not found");
    expect(out).not.toContain("You do not have access");
  });

  it("tells a 409 to merge rather than to retry blindly", () => {
    const out = html(<ErrorState error={new ApiError("conflict", 409, "conflict", "stale")} />);
    expect(out).toContain("changed while you were reading it");
    expect(out).toContain("merge");
  });

  it("says nothing was changed when the server is unreachable", () => {
    const out = html(<ErrorState error={new ApiError("network", 0, "network", "down")} />);
    expect(out).toContain("Nothing was changed");
  });

  it("falls back to a generic title for an error it has no words for", () => {
    const out = html(<ErrorState error={new Error("boom")} />);
    expect(out).toContain("Something went wrong");
  });
});
