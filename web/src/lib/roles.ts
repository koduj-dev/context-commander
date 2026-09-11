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

// CapMemberGrantOwner / CapOrgGrantOwner are OWNER-only, deliberately
// narrower than manage-members: an ADMIN who could hand out OWNER could
// promote an account they control and inherit ownership that way. The same
// predicate covers both scopes — "OWNER" means OWNER in each.
export function canGrantOwnerRole(role: string): boolean {
  return role === "OWNER";
}

// CapGateManage (internal/auth/capability.go) is ADMIN/OWNER-only.
export function canManageGates(role: string): boolean {
  return role === "ADMIN" || role === "OWNER";
}

// CapSkillInstall/CapSkillPublish are granted to CONTRIBUTOR and up.
export function canManageSkills(role: string): boolean {
  return role === "CONTRIBUTOR" || role === "ADMIN" || role === "OWNER";
}

// CapSkillOverride (shadowing an org/installation binding) is ADMIN/OWNER-only.
export function canOverrideSkills(role: string): boolean {
  return role === "ADMIN" || role === "OWNER";
}

// CapHandoffWrite is granted to CONTRIBUTOR and up.
export function canWriteHandoffs(role: string): boolean {
  return role === "CONTRIBUTOR" || role === "ADMIN" || role === "OWNER";
}

// CapHistoryWrite is granted to CONTRIBUTOR and up.
export function canWriteHistory(role: string): boolean {
  return role === "CONTRIBUTOR" || role === "ADMIN" || role === "OWNER";
}
