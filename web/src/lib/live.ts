import { useEffect, useState } from "react";
import { raw } from "./api";

/**
 * Being told when a board changes, instead of asking every ten seconds
 * (docs/adr/0022, "Push instead of polling").
 *
 * The server's stream says "changed" and nothing else; this turns each one
 * into a `pulse`, and the screen re-reads through the ordinary endpoint. A
 * burst of changes — an agent ticking five steps — is one pulse.
 *
 * Read with fetch rather than EventSource. EventSource cannot send headers,
 * and every cookie-authenticated request must carry the CSRF header
 * (lib/api.ts); exempting the stream from that would be the one endpoint
 * the defence has to remember. fetch sends it, and a streamed body reads the
 * same events.
 *
 * `live` says whether the stream is open. When it is not — the server
 * refused, a proxy in between does not stream, the tab is hidden — the
 * screen keeps polling as it did before, and this tries again later.
 */
export type Live = { live: boolean; pulse: number };

/** After an open stream ends — normally its lifetime — reconnect soon. */
export const STREAM_RECONNECT_MS = 1_000;
/** After a stream could not be opened, poll for this long before retrying. */
export const STREAM_RETRY_MS = 30_000;
/** Changes closer together than this are one re-read. */
const COALESCE_MS = 250;

export function useLive(path: string): Live {
  const [live, setLive] = useState(false);
  const [pulse, setPulse] = useState(0);

  useEffect(() => {
    if (typeof TextDecoder === "undefined" || typeof document === "undefined") return;
    let stopped = false;
    let controller: AbortController | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let coalesce: ReturnType<typeof setTimeout> | undefined;

    const bump = () => {
      if (coalesce !== undefined) return;
      coalesce = setTimeout(() => {
        coalesce = undefined;
        if (!stopped) setPulse((n) => n + 1);
      }, COALESCE_MS);
    };
    const hidden = () => document.visibilityState === "hidden";

    const connect = async () => {
      if (stopped || hidden() || controller) return;
      const mine = new AbortController();
      controller = mine;
      let opened = false;
      try {
        const res = await raw(path, { signal: mine.signal });
        const type = res.headers.get("Content-Type") ?? "";
        if (!res.body || !type.startsWith("text/event-stream")) {
          throw new Error("not an event stream");
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true }).replace(/\r\n?/g, "\n");
          let end: number;
          while ((end = buffer.indexOf("\n\n")) >= 0) {
            const event = eventName(buffer.slice(0, end));
            buffer = buffer.slice(end + 2);
            if (event === "ready") {
              // Re-read once: anything that changed between the first read
              // and the stream opening was told to nobody.
              opened = true;
              setLive(true);
              bump();
            } else if (event === "changed") {
              bump();
            }
          }
        }
      } catch {
        // Refused, unreachable, not a stream, or aborted: all mean "poll
        // for now".
        mine.abort();
      } finally {
        if (controller === mine) controller = undefined;
        if (!stopped) setLive(false);
      }
      if (stopped || hidden()) return;
      retry = setTimeout(() => void connect(), opened ? STREAM_RECONNECT_MS : STREAM_RETRY_MS);
    };

    // A hidden tab gives its stream back: the server caps streams per
    // credential, and a tab nobody looks at should not hold one.
    const onVisibility = () => {
      clearTimeout(retry);
      if (hidden()) controller?.abort();
      else void connect();
    };

    void connect();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stopped = true;
      clearTimeout(retry);
      clearTimeout(coalesce);
      controller?.abort();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [path]);

  return { live, pulse };
}

/** The `event:` of one Server-Sent Events frame; comments have none. */
export function eventName(frame: string): string | undefined {
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) return line.slice(6).trim();
  }
  return undefined;
}
