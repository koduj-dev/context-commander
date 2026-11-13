import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "../i18n";
import { PrefsProvider } from "../app/prefs";
import { EmbeddingsBody } from "./AdminEmbeddings";
import type { EmbeddingsStatus } from "../lib/api";

function show(s: EmbeddingsStatus) {
  return render(
    <I18nProvider>
      <PrefsProvider>
        <EmbeddingsBody s={s} />
      </PrefsProvider>
    </I18nProvider>,
  );
}

describe("the semantic search card", () => {
  it("says search is by words only when no provider is configured", () => {
    show({ configured: false, queue: 0, dropped: 0, pending: 0 });
    expect(screen.getByText(/Semantic search is off/)).toBeTruthy();
  });

  it("names where the text goes, the store and the health", () => {
    show({
      configured: true,
      provider: "ollama",
      model: "bge-m3",
      endpoint: "http://ollama:11434",
      store: "encrypted",
      dimensions: 1024,
      reachable: false,
      last_error: "connection refused",
      queue: 3,
      queue_capacity: 256,
      dropped: 2,
      pending: -1,
    });
    expect(screen.getByText("http://ollama:11434")).toBeTruthy();
    expect(screen.getByText("Encrypted with each project's key")).toBeTruthy();
    expect(screen.getByText("Unreachable")).toBeTruthy();
    expect(screen.getByText("(1,024 dimensions)", { exact: false })).toBeTruthy();
    expect(screen.getByText("Last error: connection refused")).toBeTruthy();
    expect(screen.getByText("Could not be counted")).toBeTruthy();
    expect(screen.getByText("3 of 256")).toBeTruthy();
    expect(screen.getByText(/2 memories did not fit/)).toBeTruthy();
  });
});
