import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Page } from "./api";

/**
 * Loading one thing from the server.
 *
 * Three states, never two: loading, failed, and loaded. Collapsing "failed"
 * into "loaded with nothing" is how a refusal ends up rendered as an empty
 * table — see lib/api.ts.
 */
export type Resource<T> = {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  reload: () => void;
};

export function useResource<T>(load: () => Promise<T>, deps: unknown[]): Resource<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [nonce, setNonce] = useState(0);

  // Guards against a slow first request resolving after a fast second one and
  // overwriting it — the classic way a list shows the previous project's rows.
  const generation = useRef(0);

  useEffect(() => {
    const mine = ++generation.current;
    setLoading(true);
    setError(undefined);
    load()
      .then((value) => {
        if (generation.current === mine) {
          setData(value);
          setError(undefined);
        }
      })
      .catch((err: unknown) => {
        if (generation.current === mine) {
          setError(err);
          setData(undefined);
        }
      })
      .finally(() => {
        if (generation.current === mine) setLoading(false);
      });
    // `load` is a fresh closure each render; the caller's deps say when it
    // actually changed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { data, error, loading, reload };
}

/**
 * A keyset-paginated list.
 *
 * The server returns an opaque cursor and does not know how many pages exist,
 * so this exposes "is there more" and nothing else. There is deliberately no
 * page count, no total and no jump-to-page: those would be numbers the API
 * cannot produce, and inventing them means either a COUNT(*) on every request
 * or a lie.
 */
export type List<T> = {
  items: T[];
  error: unknown;
  /** The first page is still in flight. */
  loading: boolean;
  /** A `Load more` is in flight; the rows already shown stay put. */
  loadingMore: boolean;
  hasMore: boolean;
  loadMore: () => void;
  reload: () => void;
};

export function useList<T>(
  fetchPage: (cursor?: string) => Promise<Page<T>>,
  deps: unknown[],
): List<T> {
  const [items, setItems] = useState<T[]>([]);
  const [cursor, setCursor] = useState<string>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [nonce, setNonce] = useState(0);

  const generation = useRef(0);

  useEffect(() => {
    const mine = ++generation.current;
    setLoading(true);
    setError(undefined);
    setItems([]);
    setCursor(undefined);
    fetchPage()
      .then((page) => {
        if (generation.current !== mine) return;
        setItems(page.items ?? []);
        setCursor(page.next_cursor);
      })
      .catch((err: unknown) => {
        if (generation.current === mine) setError(err);
      })
      .finally(() => {
        if (generation.current === mine) setLoading(false);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);

  const loadMore = useCallback(() => {
    if (!cursor || loadingMore) return;
    const mine = generation.current;
    setLoadingMore(true);
    fetchPage(cursor)
      .then((page) => {
        if (generation.current !== mine) return;
        setItems((prev) => [...prev, ...(page.items ?? [])]);
        setCursor(page.next_cursor);
      })
      .catch((err: unknown) => {
        // A failed "load more" must not blank the rows already on screen.
        if (generation.current === mine) setError(err);
      })
      .finally(() => {
        if (generation.current === mine) setLoadingMore(false);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cursor, loadingMore, ...deps]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  return useMemo(
    () => ({
      items,
      error,
      loading,
      loadingMore,
      hasMore: Boolean(cursor),
      loadMore,
      reload,
    }),
    [items, error, loading, loadingMore, cursor, loadMore, reload],
  );
}

/** Debounces a value — for search boxes that would otherwise fire per keystroke. */
export function useDebounced<T>(value: T, ms = 250): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return debounced;
}

/**
 * A read that keeps itself current: loaded once, then again every
 * `intervalMs` while the page is visible.
 *
 * A refresh never goes back to "loading" — the rows on screen stay put while
 * the next answer is in flight, or a board someone is watching would blink
 * every ten seconds. A refresh that fails keeps the last good answer and
 * reports `stale`; only a first load that fails is an error.
 */
export type Polled<T> = Resource<T> & { stale: boolean };

export function usePolled<T>(
  load: () => Promise<T>,
  deps: unknown[],
  intervalMs: number,
): Polled<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [stale, setStale] = useState(false);
  const generation = useRef(0);
  const hasData = useRef(false);
  // reload() refreshes in place, like a tick: after a change someone made
  // on this screen, the board should update, not disappear and come back.
  const refresh = useRef<() => void>(() => {});

  useEffect(() => {
    const mine = ++generation.current;
    hasData.current = false;
    setLoading(true);
    setError(undefined);
    setData(undefined);

    const fetchOnce = () => {
      load()
        .then((value) => {
          if (generation.current !== mine) return;
          hasData.current = true;
          setData(value);
          setError(undefined);
          setStale(false);
        })
        .catch((err: unknown) => {
          if (generation.current !== mine) return;
          if (hasData.current) setStale(true);
          else setError(err);
        })
        .finally(() => {
          if (generation.current === mine) setLoading(false);
        });
    };
    refresh.current = fetchOnce;

    fetchOnce();
    const timer = setInterval(() => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
      fetchOnce();
    }, intervalMs);
    return () => clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, intervalMs]);

  const reload = useCallback(() => refresh.current(), []);
  return { data, error, loading, reload, stale };
}
