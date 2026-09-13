import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { ApiError, api, storeToken, storedToken, type Me } from "../lib/api";

/**
 * Who is signed in.
 *
 * A bearer PAT, held in this browser and sent on every request. There is no
 * session cookie and no refresh flow, because the server has neither: the
 * same token a CLI or an MCP client uses is the one this UI uses, and
 * inventing a second mechanism here would mean inventing it on the server
 * too.
 *
 * The token is only ever believed after `/v1/me` accepts it. A UI that trusts
 * whatever is in storage renders a whole shell for someone who was revoked an
 * hour ago and only discovers it on the first click.
 */

type AuthState = {
  me: Me | null;
  /** True until the stored token has been checked, so nothing flashes. */
  loading: boolean;
  signIn: (token: string) => Promise<void>;
  signOut: () => void;
};

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const token = storedToken();
    if (!token) {
      setLoading(false);
      return;
    }
    const ac = new AbortController();
    api
      .me()
      .then(setMe)
      .catch((err: unknown) => {
        // Only an outright rejection clears the token. A server that is down
        // is not a revoked credential, and signing someone out because of it
        // loses the token they pasted for no reason.
        if (err instanceof ApiError && err.kind === "unauthorized") storeToken(null);
      })
      .finally(() => setLoading(false));
    return () => ac.abort();
  }, []);

  const signIn = useCallback(async (token: string) => {
    // Passed explicitly rather than stored first: a token that turns out to
    // be bad must not be left behind for the next reload to retry.
    const who = await api.me({ token });
    storeToken(token);
    setMe(who);
  }, []);

  const signOut = useCallback(() => {
    storeToken(null);
    setMe(null);
  }, []);

  const value = useMemo(() => ({ me, loading, signIn, signOut }), [me, loading, signIn, signOut]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
