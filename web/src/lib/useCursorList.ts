import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, type Page, type PageOpts } from "./api";

export const PAGE_SIZE = 100;

/**
 * useCursorList drives one paginated list: the first page on mount, then
 * "Load more" appending each next page.
 *
 * hasMore comes from the server's next_cursor, not from whether the last page
 * came back full. The old guess — `page.length >= PAGE_SIZE` — left the button
 * on screen whenever the row count happened to be an exact multiple of the
 * page size, and clicking it returned nothing.
 *
 * fetchPage is held in a ref so callers can pass an inline closure (which is a
 * new function every render) without the effect re-firing on every render.
 * `deps` is what actually decides when to reload — pass the project, filters,
 * or whatever else changes the query.
 */
export function useCursorList<T>(
  fetchPage: (opts: PageOpts) => Promise<Page<T>>,
  deps: unknown[],
  errorMessage: string,
) {
  const [items, setItems] = useState<T[] | null>(null);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchRef = useRef(fetchPage);
  fetchRef.current = fetchPage;

  const refresh = useCallback(async () => {
    setError(null);
    try {
      const page = await fetchRef.current({ limit: PAGE_SIZE });
      setItems(page.items);
      setCursor(page.next_cursor);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : errorMessage);
    }
  }, [errorMessage]);

  const loadMore = useCallback(async () => {
    if (!cursor) return;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await fetchRef.current({ limit: PAGE_SIZE, cursor });
      setItems((prev) => [...(prev ?? []), ...page.items]);
      setCursor(page.next_cursor);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : errorMessage);
    } finally {
      setLoadingMore(false);
    }
  }, [cursor, errorMessage]);

  useEffect(() => {
    void refresh();
    // deps is the caller's declared reload trigger; refresh is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { items, error, setError, hasMore: cursor !== undefined, loadingMore, loadMore, refresh };
}
