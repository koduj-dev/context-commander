// Mirrors the subset of internal/auth's roleCapabilities table
// (internal/auth/capability.go) the UI needs client-side, purely to decide
// what to render — the server re-checks every capability itself on every
// request, so a UI mistake here is a display bug, never an authorization
// bypass.
export function canReadAudit(role: string): boolean {
  return role === "ADMIN" || role === "OWNER";
}

export function canManageMembers(role: string): boolean {
  return role === "ADMIN" || role === "OWNER";
}
