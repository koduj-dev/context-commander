// Thin localStorage wrapper for the caller's PAT. There is no session/login
// backend (docs/adr/0009: PAT issuance is CLI-only) — the UI authenticates
// exactly like the CLI does, by holding a bearer token and sending it on
// every request. See lib/api.ts for where it's attached.
const STORAGE_KEY = "cc_token";

export function getToken(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, token);
  } catch {
    // localStorage can throw (private browsing, quota) — the token just
    // won't survive a reload, which is a degraded-but-safe outcome, not a
    // crash.
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // see setToken
  }
}
