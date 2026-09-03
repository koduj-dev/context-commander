import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { ApiError } from "../lib/api";

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [token, setTokenInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(token.trim());
      navigate("/", { replace: true });
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "could not sign in";
      setError(msg);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-cc-bg px-4">
      <div className="w-full max-w-md rounded-lg border border-cc-border bg-cc-surface-1 p-8">
        <h1 className="mb-1 text-xl font-semibold text-cc-text">Context Commander</h1>
        <p className="mb-6 text-sm text-cc-text-muted">
          Sign in with a personal access token. If you don't have one yet, get one on the server with{" "}
          <code className="rounded bg-cc-surface-2 px-1 py-0.5 text-cc-text">ccd bootstrap</code> or{" "}
          <code className="rounded bg-cc-surface-2 px-1 py-0.5 text-cc-text">ccd token create</code>.
        </p>
        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label htmlFor="token" className="mb-1 block text-sm font-medium text-cc-text-secondary">
              Personal access token
            </label>
            <input
              id="token"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={token}
              onChange={(e) => setTokenInput(e.target.value)}
              placeholder="cc_..."
              className="w-full rounded border border-cc-border bg-cc-surface-2 px-3 py-2 text-sm text-cc-text outline-none focus:border-cc-border-strong"
            />
          </div>
          {error && <p className="text-sm text-cc-danger">{error}</p>}
          <button
            type="submit"
            disabled={submitting || token.trim() === ""}
            className="w-full rounded bg-cc-accent-primary px-3 py-2 text-sm font-medium text-cc-void disabled:opacity-50"
          >
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </div>
    </div>
  );
}
