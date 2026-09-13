import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

/**
 * Breadcrumbs, published by a screen and rendered by the shell.
 *
 * They belong in the topbar — that is where the design puts them, and it is
 * the one strip that stays put while the page under it changes. A screen
 * cannot render into the topbar directly, so it says what its trail is and
 * the shell draws it.
 *
 * The alternative, deriving the trail from the URL in the shell, was not
 * taken: the last crumb is usually a name the shell has not loaded (a
 * project's display name, an organization's) and it would have to fetch it a
 * second time to render one line.
 */
type Trail = { path: ReactNode[]; current: ReactNode } | null;

const Ctx = createContext<{ trail: Trail; setTrail: (t: Trail) => void } | null>(null);

export function CrumbsProvider({ children }: { children: ReactNode }) {
  const [trail, setTrail] = useState<Trail>(null);
  const value = useMemo(() => ({ trail, setTrail }), [trail]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

/**
 * The browser tab, from the same trail.
 *
 * A console people keep several tabs of is unusable when every tab reads
 * "Context Commander" — and the back button's history list is the same
 * string repeated. The crumbs already name the screen, so this reuses them
 * rather than asking every page to remember a second title.
 */
export function useDocumentTitle(appName: string) {
  const trail = useCrumbsValue();
  useEffect(() => {
    const here = typeof trail?.current === "string" ? trail.current : undefined;
    document.title = here ? `${here} · ${appName}` : appName;
  }, [trail, appName]);
}

/** What the shell draws. Null while no screen has published a trail. */
export function useCrumbsValue(): Trail {
  return useContext(Ctx)?.trail ?? null;
}

/**
 * Publishes this screen's trail for as long as it is mounted.
 *
 * `key` exists because the crumbs themselves are nodes, which are new objects
 * on every render and would loop an effect that depended on them. The caller
 * gives the strings that actually identify the trail.
 */
export function useCrumbs(key: string, build: () => { path: ReactNode[]; current: ReactNode }) {
  const ctx = useContext(Ctx);
  const setTrail = ctx?.setTrail;
  const buildRef = useCallback(build, [key]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!setTrail) return;
    setTrail(buildRef());
    return () => setTrail(null);
  }, [key, setTrail, buildRef]);
}
