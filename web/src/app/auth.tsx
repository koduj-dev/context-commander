import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { ApiError, api, type AuthMethod, type Me } from "../lib/api";

/**
 * Who is signed in, and how anyone can become signed in.
 *
 * The credential is a session in an HttpOnly cookie (docs/adr/0019), so this
 * module cannot read it and does not try. "Am I signed in" is answered by
 * asking the server — `/v1/me` succeeds or it does not — rather than by
 * checking storage for a token, which was the old shape and which rendered a
 * whole shell for someone revoked an hour ago.
 *
 * It also answers the question that comes before signing in: a fresh
 * installation has no users, and the only way to create the first one used to
 * be a shell on the server. `needsSetup` routes to the screen that fixes
 * that.
 */

type AuthState = {
  me: Me | null;
  /** True until the server has been asked, so nothing flashes. */
  loading: boolean;
  /** This installation has no users yet: show first-run setup, not sign-in. */
  needsSetup: boolean;
  /** What this installation offers. The sign-in screen renders only these. */
  methods: AuthMethod[];
  /**
   * The server could not be asked who this is — not a refusal.
   *
   * Kept distinct from "not signed in" because the two deserve different
   * screens. A 500 or a dead socket means nobody knows yet; rendering a
   * login form for it asks someone to re-type a password that was never the
   * problem, and if they do, it fails again for a reason the form cannot
   * explain.
   */
  unreachable: boolean;
  retry: () => void;
  signInWithPassword: (email: string, password: string) => Promise<void>;
  signInWithToken: (token: string) => Promise<void>;
  completeSetup: (
    code: string,
    email: string,
    displayName: string,
    password: string,
  ) => Promise<void>;
  signOut: () => Promise<void>;
};

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [methods, setMethods] = useState<AuthMethod[]>([]);
  const [unreachable, setUnreachable] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    (async () => {
      setLoading(true);
      setUnreachable(false);
      try {
        const who = await api.me();
        if (live) setMe(who);
        return;
      } catch (err) {
        // Only an outright rejection means "not signed in".
        if (!(err instanceof ApiError) || err.kind !== "unauthorized") {
          if (live) setUnreachable(true);
          return;
        }
      }
      // Not signed in: find out what to offer. Both are unauthenticated
      // reads, and both are allowed to fail without breaking the screen —
      // worst case the sign-in form falls back to its defaults.
      const [status, offered] = await Promise.allSettled([api.setupStatus(), api.authMethods()]);
      if (!live) return;
      if (status.status === "fulfilled") setNeedsSetup(status.value.needs_setup);
      if (offered.status === "fulfilled") setMethods(offered.value.methods);
    })().finally(() => {
      if (live) setLoading(false);
    });
    return () => {
      live = false;
    };
  }, [attempt]);

  // Re-running the effect rather than duplicating its body: the retry has to
  // do exactly what the first attempt did, and two copies of that would
  // eventually disagree.
  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  // Each of these ends with /v1/me rather than with the sign-in response:
  // the cookie is set by the server and unreadable here, so the only honest
  // confirmation that it works is a request that needed it.
  const afterCredential = useCallback(async () => {
    setMe(await api.me());
    setNeedsSetup(false);
  }, []);

  const signInWithPassword = useCallback(
    async (email: string, password: string) => {
      await api.signInWithPassword(email, password);
      await afterCredential();
    },
    [afterCredential],
  );

  const signInWithToken = useCallback(
    async (token: string) => {
      await api.signInWithToken(token);
      await afterCredential();
    },
    [afterCredential],
  );

  const completeSetup = useCallback(
    async (code: string, email: string, displayName: string, password: string) => {
      await api.completeSetup(code, email, displayName, password);
      await afterCredential();
    },
    [afterCredential],
  );

  const signOut = useCallback(async () => {
    // The local state is cleared whatever the server says. A failed sign-out
    // that leaves someone looking at a signed-in shell is worse than one that
    // leaves a revocable session behind, and the server's own endpoint is
    // forgiving about being called again.
    try {
      await api.signOut();
    } finally {
      setMe(null);
      const offered = await api.authMethods().catch(() => null);
      if (offered) setMethods(offered.methods);
    }
  }, []);

  const value = useMemo(
    () => ({
      me,
      loading,
      needsSetup,
      methods,
      unreachable,
      retry,
      signInWithPassword,
      signInWithToken,
      completeSetup,
      signOut,
    }),
    [
      me,
      loading,
      needsSetup,
      methods,
      unreachable,
      retry,
      signInWithPassword,
      signInWithToken,
      completeSetup,
      signOut,
    ],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
