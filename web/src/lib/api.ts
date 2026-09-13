/**
 * The one place that talks to the server.
 *
 * Two rules hold everywhere below.
 *
 * A failure keeps its shape. The server distinguishes "you may not",
 * "it is not there", "someone changed it first" and "you are going too fast",
 * and each calls for a different thing from the reader — so they arrive here
 * as distinct kinds rather than collapsing into one `Error("request failed")`.
 * Rendering a 403 as an empty list, which is the easiest mistake in this
 * layer, tells someone their project is empty when it is in fact not theirs.
 *
 * Nothing is invented. If the backend cannot do a thing, this file has no
 * function for it — a stub that resolves optimistically would move the lie
 * from the API into the UI, where it is harder to find.
 */

export type ApiErrorKind =
  | "unauthorized"
  | "forbidden"
  | "not_found"
  | "conflict"
  | "rate_limited"
  | "unavailable"
  | "server"
  | "network";

export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly status: number;
  readonly code: string;

  constructor(kind: ApiErrorKind, status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.kind = kind;
    this.status = status;
    this.code = code;
  }
}

function kindFor(status: number, code: string): ApiErrorKind {
  switch (status) {
    case 401:
      return "unauthorized";
    case 403:
      return "forbidden";
    case 404:
      return "not_found";
    case 409:
      // Both an optimistic-concurrency conflict and "another client claimed
      // this first" land here. The code distinguishes them for callers that
      // care; both mean "re-read before you act", which is why they share a
      // kind.
      return "conflict";
    case 429:
      return "rate_limited";
    case 503:
      return "unavailable";
    default:
      return code === "overloaded" ? "unavailable" : "server";
  }
}

const TOKEN_KEY = "cc.token";

export function storedToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function storeToken(token: string | null) {
  try {
    if (token === null) localStorage.removeItem(TOKEN_KEY);
    else localStorage.setItem(TOKEN_KEY, token);
  } catch {
    // A browser with storage blocked can still use the app for one session.
  }
}

type Options = {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  /** Sent as a bearer token; defaults to the stored one. */
  token?: string;
};

export async function request<T>(path: string, opts: Options = {}): Promise<T> {
  const token = opts.token ?? storedToken();
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(`/v1${path}`, {
      method: opts.method ?? "GET",
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError("network", 0, "network", "could not reach the server");
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  if (!res.ok) {
    let code = "";
    let message = res.statusText;
    try {
      const parsed = JSON.parse(text) as { error?: { code?: string; message?: string } };
      code = parsed.error?.code ?? "";
      message = parsed.error?.message ?? message;
    } catch {
      // Some endpoints (the MCP mount, a proxy in front) answer in plain
      // text. The status still carries the meaning.
    }
    throw new ApiError(kindFor(res.status, code), res.status, code, message);
  }

  return text ? (JSON.parse(text) as T) : (undefined as T);
}

/** The `{items, next_cursor}` envelope every growing list returns. */
export type Page<T> = { items: T[]; next_cursor?: string };

export function withQuery(path: string, params: Record<string, string | number | undefined>) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `${path}?${s}` : path;
}

// ---------------------------------------------------------------- resources
//
// Shapes below mirror what the adapters actually return. Where a field is
// optional here it is optional there — `claimed_at` is absent until someone
// claims, and pretending otherwise would put a zero date on every screen.

export type Me = {
  principal_id: string;
  email: string;
  display_name: string;
  is_installation_admin: boolean;
};

export type ProjectSummary = {
  project: { id: string; namespace: string; slug: string; name: string; visibility: string };
  role: string;
};

export type Project = {
  id: string;
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
};

export type Handoff = {
  id: string;
  status: "open" | "claimed" | "closed";
  created_at: string;
  claimed_at?: string;
  claimed_by_principal?: string;
  claimed_by_client?: string;
  closed_at?: string;
  summary?: string;
  completed_work?: string;
  next_actions?: string;
  referenced_artifacts?: string[];
};

export type Member = {
  principal_id: string;
  role: string;
  created_at: string;
  email?: string;
  display_name?: string;
};

export type SkillBinding = {
  id: string;
  scope: "installation" | "organization" | "project";
  scope_ref?: string;
  skill_id: string;
  skill_version_id: string;
  pinned: boolean;
  managed: boolean;
  overridable: boolean;
};

export type CatalogEntry = {
  skill_id: string;
  name: string;
  author?: string;
  origin?: string;
  scope: "installation" | "organization" | "project";
  scope_ref?: string;
  latest_version_id: string;
  latest_version: number;
  latest_checksum: string;
  trust_status: string;
};

export type AuditRecord = {
  id: number;
  timestamp: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  result: string;
  principal_id?: string;
  actor_email?: string;
};

export type Credential = {
  id: string;
  client_type: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
  revoked_at?: string;
};

export const api = {
  // Takes options so sign-in can probe a token before storing it — a bad
  // token must not be left in storage for the next reload to retry.
  me: (opts?: { token?: string }) => request<Me>("/me", opts),
  myProjects: () => request<ProjectSummary[]>("/me/projects"),
  tokens: () => request<Credential[]>("/me/tokens"),
  revokeToken: (id: string) => request<void>(`/me/tokens/${id}`, { method: "DELETE" }),

  project: (ns: string, slug: string) => request<Project>(`/projects/${ns}/${slug}`),
  members: (ns: string, slug: string) => request<Page<Member>>(`/projects/${ns}/${slug}/members`),

  currentHandoff: (ns: string, slug: string) =>
    request<Handoff>(`/projects/${ns}/${slug}/handoffs/current`),
  handoffs: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<Handoff>>(withQuery(`/projects/${ns}/${slug}/handoffs`, { cursor, limit })),
  claimHandoff: (ns: string, slug: string, id: string) =>
    request<Handoff>(`/projects/${ns}/${slug}/handoffs/${id}/claim`, { method: "POST" }),
  closeHandoff: (ns: string, slug: string, id: string) =>
    request<Handoff>(`/projects/${ns}/${slug}/handoffs/${id}/close`, { method: "POST" }),

  effectiveSkills: (ns: string, slug: string) =>
    request<SkillBinding[]>(`/projects/${ns}/${slug}/skills/effective`),
  searchSkills: (query?: string, cursor?: string, limit?: number) =>
    request<Page<CatalogEntry>>(withQuery("/skills", { q: query, cursor, limit })),

  audit: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<AuditRecord>>(withQuery(`/projects/${ns}/${slug}/audit`, { cursor, limit })),
};
