import type { MessageKey } from "../i18n/en";

type T = (key: MessageKey) => string;

/**
 * Turning a value the server sent into a word the reader knows.
 *
 * Each of these falls back to the raw token rather than to a missing-key
 * placeholder. The sets are closed in today's schema, but a server newer than
 * this bundle can name a role or a trust state this build has never heard of,
 * and "MAINTAINER" is a usable answer where "role.MAINTAINER" is not.
 */

const ROLES = ["OWNER", "ADMIN", "CONTRIBUTOR", "READER"];
export function roleLabel(t: T, role: string): string {
  return ROLES.includes(role) ? t(`role.${role}` as MessageKey) : role;
}

const SCOPES = ["installation", "organization", "project"];
export function scopeLabel(t: T, scope: string): string {
  return SCOPES.includes(scope) ? t(`scope.${scope}` as MessageKey) : scope;
}

export function trustLabel(t: T, trust: string): string {
  if (trust === "signed") return t("trust.signed");
  if (trust === "approved") return t("trust.approved");
  if (trust === "unverified") return t("trust.unverified");
  return trust;
}

/** Signed and approved versions are trusted (docs/adr/0040); anything else warns. */
export function trustTone(trust: string): "ok" | "warn" {
  return trust === "signed" || trust === "approved" ? "ok" : "warn";
}

/** Up to two initials of a name, for an avatar. */
export function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}
