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
  | "quota_exceeded"
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
    // The namespace is full (docs/adr/0021). Its own kind because the
    // remedy is the reader's — delete something, or ask for room — and a
    // generic server error would tell them to retry, which cannot help.
    case 507:
      return "quota_exceeded";
    case 503:
      return "unavailable";
    default:
      return code === "overloaded" ? "unavailable" : "server";
  }
}

/**
 * The header that makes cookie authentication safe (docs/adr/0019).
 *
 * A cross-site <form> cannot set a custom header, and a cross-origin fetch
 * that tries is stopped by the preflight — no CORS middleware answers one,
 * because the SPA is served from the same origin as the API by the same
 * binary. The server refuses any cookie-authenticated request without it, so
 * it is sent on every request rather than remembered per call site.
 */
const CSRF_HEADER = "X-Requested-With";
const CSRF_VALUE = "cc-web";

/**
 * There is no token in browser storage, and that is the point.
 *
 * The session lives in an HttpOnly cookie the page cannot read, so an XSS in
 * this SPA cannot lift a credential out of it. What this module used to do —
 * keep the pasted PAT in localStorage and attach it to every request — meant
 * one injection handed over a long-lived and often non-expiring token, with
 * nothing to tell its owner it had happened.
 *
 * A PAT is still a way to sign IN: it is posted once to /v1/auth/token,
 * exchanged for a session, and never stored here.
 */

type Options = {
  method?: string;
  /** Serialized as JSON. */
  body?: unknown;
  /**
   * Sent verbatim, with no Content-Type of ours. Artifact content is bytes
   * the caller already has; wrapping it in JSON would change what is stored.
   */
  rawBody?: string;
  signal?: AbortSignal;
};

/**
 * One request, with the session cookie attached and failures classified.
 *
 * Returns the raw Response so callers that are not reading JSON — artifact
 * content is application/octet-stream with its version in a header — get the
 * same auth, the same error classification and the same network handling
 * instead of reaching for `fetch` themselves.
 */
export async function raw(path: string, opts: Options = {}): Promise<Response> {
  const headers: Record<string, string> = { [CSRF_HEADER]: CSRF_VALUE };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(`/v1${path}`, {
      method: opts.method ?? "GET",
      // Explicit rather than relying on the default: this is the only thing
      // carrying the caller's identity, and a default is a thing that gets
      // changed by someone who does not know that.
      credentials: "same-origin",
      headers,
      body: opts.rawBody ?? (opts.body === undefined ? undefined : JSON.stringify(opts.body)),
      signal: opts.signal,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError("network", 0, "network", "could not reach the server");
  }

  if (!res.ok) throw await toApiError(res);
  return res;
}

/** Reads the server's error envelope, falling back to the status alone. */
async function toApiError(res: Response): Promise<ApiError> {
  let code = "";
  let message = res.statusText;
  const text = await res.text();
  try {
    const parsed = JSON.parse(text) as { error?: { code?: string; message?: string } };
    code = parsed.error?.code ?? "";
    message = parsed.error?.message ?? message;
  } catch {
    // Some endpoints (the MCP mount, a proxy in front) answer in plain
    // text. The status still carries the meaning.
  }
  return new ApiError(kindFor(res.status, code), res.status, code, message);
}

export async function request<T>(path: string, opts: Options = {}): Promise<T> {
  const headers: Record<string, string> = { [CSRF_HEADER]: CSRF_VALUE };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";

  let res: Response;
  try {
    res = await fetch(`/v1${path}`, {
      method: opts.method ?? "GET",
      // Explicit rather than relying on the default: this is the only thing
      // carrying the caller's identity, and a default is a thing that gets
      // changed by someone who does not know that.
      credentials: "same-origin",
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    throw new ApiError("network", 0, "network", "could not reach the server");
  }

  if (!res.ok) throw await toApiError(res);
  if (res.status === 204) return undefined as T;

  const text = await res.text();
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
// Every shape below was read off the handler that produces it
// (adapters/http/*.go), not inferred from the domain types. They differ: a
// handler flattens `namespace/slug` onto the project, omits a field rather
// than sending null, and returns a bare array where the domain paginates.
//
// Which lists are paginated is also a property of the handler, not of the
// concept. `writeList` in adapters/http/paging.go marks the paged ones;
// everything else returns an array because it is bounded by something other
// than how much data the system holds — how many projects a person was added
// to, how many roles a project has.

/**
 * A way to sign in that this installation actually offers.
 *
 * A list rather than a set of booleans because directory providers append to
 * it: each configured one arrives with its own id and display name. The
 * sign-in screen renders exactly what is here, which is the UI's standing
 * rule — never offer a control for a capability the backend lacks — applied
 * to the one screen that was breaking it.
 */
export type AuthMethod = {
  kind: "password" | "token" | "ldap" | "oidc";
  id?: string;
  label?: string;
};

/**
 * One row of "is anything waiting on me", across every project.
 *
 * Shaped by the handler (adapters/http/me.go), not by the domain type: the
 * project arrives as a `namespace/slug` **path** rather than an object, and
 * the summary is already resolved into the row — `handoffs/current` returns a
 * body and this read does the same work server-side so the browser does not
 * have to ask twice per project.
 *
 * There is no project *name* here, which is deliberate on the server's part:
 * a cross-project read should not join a table it does not need. The screen
 * joins it against /me/projects, which it already loads.
 */
export type WaitingHandoff = {
  /** "namespace/slug". */
  project: string;
  role: string;
  id: string;
  status: string;
  created_at: string;
  claimed_at?: string;
  summary: string;
};

export type Session = {
  id: string;
  created_at: string;
  last_used_at: string | null;
  expires_at: string | null;
  /** The session making this request, so the screen can say "this browser". */
  current: boolean;
};

export type Me = {
  principal_id: string;
  kind: "user" | "service";
  /** Set for a user principal. */
  email?: string;
  display_name?: string;
  /** Set for a service principal instead of email/display_name. */
  name?: string;
  is_installation_admin: boolean;
  /**
   * Whether this account can sign in with a password.
   *
   * It separates two states that look identical in a form: changing a
   * password, which must ask for the current one, and setting a first one,
   * which cannot ask for something that does not exist.
   */
  has_password: boolean;
};

/** What to call a principal on screen, whichever kind it is. */
export function principalName(me: Me | null | undefined): string {
  return me?.display_name || me?.name || me?.email || "";
}

export type MyProject = {
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
  role: string;
};

export type MyOrganization = { id: string; name: string; role: string };

export type Project = {
  id: string;
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
};

export type Member = {
  principal_id: string;
  role: string;
  created_at: string;
  email?: string;
  display_name?: string;
};

export type OrganizationMember = {
  user_id: string;
  role: string;
  created_at: string;
  /** Joined from the users row, so a membership can be shown as a person. */
  email?: string;
  display_name?: string;
};

export type Handoff = {
  id: string;
  source_session_id?: string;
  status: string;
  created_at: string;
  claimed_at?: string;
  claimed_by_principal?: string;
  claimed_by_client?: string;
  closed_at?: string;
  closed_by_principal?: string;
  closed_by_client?: string;
};

/** The body is only present on a single-handoff read, never in a list. */
export type HandoffDetail = Handoff & {
  summary: string;
  completed_work: string;
  next_actions: string;
  referenced_artifacts?: string[];
};

/**
 * The aggregated bootstrap read model — what an agent is handed when it
 * starts on a project.
 *
 * The server's own read model is the wire format here: the Go type carries
 * the JSON tags and the adapter writes it straight out, so a second shape
 * mirroring it field for field would only be a place for the two to drift.
 */
export type ContextPack = {
  project: { id: string; path: string; name: string };
  role: string;
  capabilities: string[];
  handoff?: {
    id: string;
    status: string;
    created_at: string;
    claimed_at?: string;
    summary: string;
    completed_work: string;
    next_actions: string;
  };
  artifacts: {
    logical_path: string;
    version: number;
    content_hash: string;
    size_bytes: number;
    updated_at: string;
    content: string;
  }[];
  skills: {
    binding_id: string;
    skill_id: string;
    skill_version_id: string;
    name: string;
    version: number;
    scope: string;
    trust_status: string;
    content: string;
  }[];
  events: {
    recent: { type: string; resource_type: string; resource_id?: string }[];
    next_cursor?: string;
  };
  /** What the pack cost, and — the point of it — what it left out. */
  budget: {
    limit_bytes: number;
    used_bytes: number;
    truncated: { kind: string; ref: string; size_bytes?: number; reason: string }[];
  };
  assembled_at: string;
};

export type Artifact = {
  logical_path: string;
  content_type: string;
  current_version: number;
};

export type ArtifactVersion = {
  version: number;
  content_hash: string;
  size_bytes: number;
};

export type SecretReference = { reference: string; name: string };

export type GateBinding = {
  id: string;
  scope: string;
  scope_ref?: string;
  gate_name: string;
  hook: string;
  enabled: boolean;
  config: unknown;
};

export type SkillBinding = {
  id: string;
  scope: string;
  scope_ref?: string;
  skill_id: string;
  skill_version_id: string;
  pinned: boolean;
  managed: boolean;
  overridable: boolean;
};

export type SkillVersion = {
  id: string;
  skill_id: string;
  version: number;
  checksum: string;
  declared_capabilities: string[];
  trust_status: string;
};

export type CatalogEntry = {
  skill_id: string;
  name: string;
  author?: string;
  origin?: string;
  scope: string;
  scope_ref?: string;
  latest_version_id: string;
  latest_version: number;
  latest_checksum: string;
  trust_status: string;
};

/**
 * A version plus its bytes.
 *
 * Extends SkillVersion because the Go response embeds skillVersionResponse,
 * and embedding flattens into the same JSON object — `checksum` and
 * `declared_capabilities` arrive alongside `content`, not nested under it.
 */
export type SkillContent = SkillVersion & {
  skill_name: string;
  skill_author?: string;
  skill_origin?: string;
  content: string;
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

export type EventRecord = {
  seq: number;
  event_id: string;
  type: string;
  resource_type: string;
  resource_id?: string;
  payload_kind: string;
};

export type SecurityEvent = {
  id: number;
  ts: string;
  kind: string;
  principal_id?: string;
  credential_id?: string;
  project_id?: string;
  capability?: string;
  reason: string;
  request_id?: string;
};

export type Conversation = { id: string; created_by_principal: string; created_at: string };

export type ExternalSession = {
  id: string;
  conversation_id: string;
  provider: string;
  client: string;
  provider_session_id: string;
  parent_external_session_id?: string;
  relation: string;
  started_at: string;
};

export type HistoryObject = {
  id: string;
  external_session_id: string;
  type: string;
  derived_from?: string;
  content_hash: string;
  /** Base64. History bodies are opaque bytes on the wire — see decodeHistory. */
  content: string;
  created_at: string;
};

/**
 * History content travels base64-encoded, because a history object is
 * whatever a client recorded and need not be text at all. Decoding can
 * therefore fail on something that is not UTF-8, and that is a fact about
 * the object rather than an error to throw at the reader.
 */
export function decodeHistory(base64: string): string | null {
  try {
    const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return null;
  }
}

export type LineageEdge = { from_id: string; to_id: string; kind: string };

export type Credential = {
  id: string;
  client_type: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
  revoked_at?: string;
};

/** The raw token is in this response and nowhere else, ever again. */
export type IssuedToken = {
  token: string;
  credential_id: string;
  client_type: string;
  expires_at?: string;
};

export type InstallationUser = {
  id: string;
  email: string;
  display_name: string;
  created_at: string;
  /**
   * Who this user ACTS AS, which is a different id from `id`.
   *
   * An organization membership is keyed by the user; a project membership by
   * the principal. Sending one where the other is expected produces a
   * foreign-key error nobody can read, so both travel together. Absent on
   * the response to creating a user.
   */
  principal_id?: string;
};

export type ServicePrincipal = {
  id: string;
  name: string;
  description?: string;
  created_at: string;
  /** The principal this agent acts as — see InstallationUser.principal_id. */
  principal_id?: string;
  /**
   * Who registered this agent, already resolved to a person.
   *
   * Absent when nobody is recorded: an agent registered before the column
   * existed, or one whose registrar has since been deleted. The screen says
   * so rather than printing a blank — and it must not say "owner", because
   * this grants the agent nothing. An agent's access comes from its own
   * project memberships.
   */
  created_by?: string;
  created_by_email?: string;
  created_by_name?: string;
};

/**
 * What the installation holds, in cardinalities.
 *
 * Counts and one byte total, never a row-level value. That is what makes it
 * available at all: an installation admin is not a member of every project
 * and cannot read project content, so a field naming a path or a summary
 * would widen that boundary rather than report on it
 * (migrations/000024).
 */
export type InstallationStats = {
  scale: { projects: number; organizations: number; users: number; service_principals: number };
  content: {
    artifacts: number;
    artifact_versions: number;
    /** Logical size of every version written — not disk usage. */
    stored_bytes: number;
    secrets: number;
    conversations: number;
    history_objects: number;
  };
  coordination: {
    handoffs_open: number;
    handoffs_claimed: number;
    handoffs_closed: number;
    skills: number;
    skill_versions: number;
    skill_bindings: number;
    gate_bindings: number;
  };
  activity: {
    events: number;
    audit_records: number;
    security_events: number;
    newest_activity?: string;
    oldest_project?: string;
  };
  credentials: { active: number; revoked: number; expired: number; used_last_7_days: number };
};

/**
 * Usage per UTC week (oldest first, empty weeks included) and the handoff
 * loop over the installation's life — counts and durations only, never who
 * (migrations/000028). A duration is absent when there was nothing to take
 * a median of, which is not the same as zero.
 */
/**
 * Plans, phases and presence (adapters/http/plans.go, docs/adr/0022). People
 * travel as ids plus a `people` map on the reads that name them.
 */
export type ItemStatus = "open" | "in_progress" | "done" | "blocked" | "skipped";
export type ItemKind = "task" | "check";
export type PlanStatus = "draft" | "active" | "done" | "abandoned";
export type PhaseStatus = "planned" | "active" | "done";

export type Effort = { seconds: number; tokens_in: number; tokens_out: number };
export type Counts = {
  open: number;
  in_progress: number;
  done: number;
  blocked: number;
  skipped: number;
};

export type PlanItem = {
  id: string;
  plan_id: string;
  position: number;
  kind: ItemKind;
  status: ItemStatus;
  text: string;
  note?: string;
  assignee?: string;
  updated_by?: string;
  updated_at: string;
  completed_at?: string;
  effort: Effort;
};

export type Plan = {
  id: string;
  phase_id?: string;
  status: PlanStatus;
  version: number;
  title: string;
  body: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  items: PlanItem[];
};

export type Phase = {
  id: string;
  position: number;
  status: PhaseStatus;
  title: string;
  goal: string;
  counts: Counts;
  effort: Effort;
};

export type PlanSummary = {
  id: string;
  phase_id?: string;
  status: PlanStatus;
  title: string;
  created_by: string;
  updated_at: string;
  counts: Counts;
  effort: Effort;
};

export type Presence = {
  client_id: string;
  client_type: string;
  principal_id: string;
  you: boolean;
  project: string;
  plan_id?: string;
  plan_title?: string;
  item_id?: string;
  item_text?: string;
  activity?: string;
  since: string;
  last_seen: string;
  idle: boolean;
};

export type Person = { kind: "user" | "service"; name: string };

export type Board = {
  phases: Phase[];
  plans: PlanSummary[];
  in_progress: { plan_title: string; item: PlanItem }[];
  blocked: { plan_title: string; item: PlanItem }[];
  presence: Presence[];
  people: Record<string, Person>;
};

export type PlanHit = {
  plan_id: string;
  plan_title: string;
  plan_status: PlanStatus;
  item_id?: string;
  text: string;
  status: string;
};

export type ItemChange = {
  status?: ItemStatus;
  text?: string;
  note?: string;
  /** null unassigns; absent leaves the assignee alone. */
  assignee?: string | null;
};

/**
 * One namespace's storage (adapters/http/storage.go). limit_bytes is what is
 * enforced and is absent when nothing limits the namespace. override and
 * created_at come only with the admin listing.
 */
export type NamespaceStorage = {
  namespace_id: string;
  slug: string;
  kind: "personal" | "organization";
  display_name: string;
  used_bytes: number;
  limit_bytes?: number;
  created_at?: string;
  override?: "default" | "custom" | "unlimited";
};

export type StorageLimit =
  { kind: "default" } | { kind: "unlimited" } | { kind: "custom"; bytes: number };

export type InstallationTrends = {
  weeks: {
    /** A date (YYYY-MM-DD), the week's Monday in UTC — not a timestamp. */
    week_start: string;
    active_users: number;
    active_agents: number;
    projects_created: number;
    handoffs_created: number;
    handoffs_claimed: number;
    handoffs_claimed_by_other_client: number;
    handoffs_closed: number;
    median_seconds_to_claim?: number;
    events: number;
  }[];
  handoff_flow: {
    handoffs: number;
    claimed: number;
    claimed_by_other_client: number;
    stale_open: number;
    median_seconds_to_claim?: number;
    p90_seconds_to_claim?: number;
    median_seconds_claim_to_close?: number;
    projects: number;
    projects_with_takeover: number;
    median_seconds_to_first_takeover?: number;
  };
};

export type InstallationAdmin = {
  user_id: string;
  role: string;
  granted_by?: string;
  granted_at: string;
};

const project = (ns: string, slug: string) =>
  `/projects/${encodeURIComponent(ns)}/${encodeURIComponent(slug)}`;

export const api = {
  // ---- authentication (docs/adr/0019) ------------------------------------
  //
  // Every one of these but `me` is reachable without a credential, because
  // the screen that calls them is the screen someone has before they have
  // one.
  authMethods: () => request<{ methods: AuthMethod[] }>("/auth/methods"),
  setupStatus: () => request<{ needs_setup: boolean }>("/setup/status"),
  completeSetup: (code: string, email: string, displayName: string, password: string) =>
    request<{ expires_at: string }>("/setup", {
      method: "POST",
      body: { code, email, display_name: displayName, password },
    }),
  signInWithPassword: (email: string, password: string) =>
    request<{ expires_at: string }>("/auth/password", {
      method: "POST",
      body: { email, password },
    }),
  // The PAT is sent once and forgotten; what comes back is a session cookie.
  signInWithToken: (token: string) =>
    request<{ expires_at: string }>("/auth/token", { method: "POST", body: { token } }),
  signOut: () => request<void>("/auth/session", { method: "DELETE" }),

  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>("/me/password", {
      method: "PUT",
      body: { current_password: currentPassword, new_password: newPassword },
    }),
  sessions: () => request<Session[]>("/me/sessions"),
  // The caller's own namespace and their organizations'. An envelope with no
  // cursor: it is bounded by how many organizations someone is in.
  myStorage: () => request<Page<NamespaceStorage>>("/me/storage"),
  revokeSession: (id: string) => request<void>(`/me/sessions/${id}`, { method: "DELETE" }),

  me: () => request<Me>("/me"),
  myProjects: () => request<MyProject[]>("/me/projects"),
  myOrganizations: () => request<MyOrganization[]>("/me/organizations"),
  // One request for every project, instead of one request per project.
  myHandoffs: () => request<WaitingHandoff[]>("/me/handoffs"),

  tokens: () => request<Credential[]>("/me/tokens"),
  issueToken: (clientType: string, expiresInSeconds?: number) =>
    request<IssuedToken>("/me/tokens", {
      method: "POST",
      body: { client_type: clientType, expires_in_seconds: expiresInSeconds ?? null },
    }),
  revokeToken: (id: string) => request<void>(`/me/tokens/${id}`, { method: "DELETE" }),

  createProject: (path: string, name: string) =>
    request<Project>("/projects", { method: "POST", body: { project: path, name } }),
  project: (ns: string, slug: string) => request<Project>(project(ns, slug)),

  members: (ns: string, slug: string) => request<Member[]>(`${project(ns, slug)}/members`),
  addMember: (ns: string, slug: string, principalID: string, role: string) =>
    request<Member>(`${project(ns, slug)}/members`, {
      method: "POST",
      body: { principal_id: principalID, role },
    }),

  contextPack: (ns: string, slug: string, opts: { budget?: number; eventLimit?: number } = {}) =>
    request<ContextPack>(
      withQuery(`${project(ns, slug)}/context`, {
        budget: opts.budget,
        event_limit: opts.eventLimit,
      }),
    ),

  artifacts: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<Artifact>>(withQuery(`${project(ns, slug)}/artifacts`, { cursor, limit })),
  /**
   * Writes a new version.
   *
   * `expectedVersion` is the version the editor was opened on, and 0 for a
   * path that does not exist yet. The server refuses with a 409 when someone
   * else wrote in between — that refusal is the whole point: without it a
   * save would silently discard their work.
   *
   * The body is raw bytes, not JSON, so it does not go through request<T>.
   */
  writeArtifact: async (
    ns: string,
    slug: string,
    path: string,
    content: string,
    contentType: string,
    expectedVersion: number,
  ) => {
    const res = await raw(
      withQuery(`${project(ns, slug)}/artifacts/content`, {
        path,
        content_type: contentType,
        expected_version: expectedVersion,
      }),
      { method: "PUT", rawBody: content },
    );
    return (await res.json()) as ArtifactVersion;
  },
  deleteArtifact: (ns: string, slug: string, path: string) =>
    request<void>(withQuery(`${project(ns, slug)}/artifacts/content`, { path }), {
      method: "DELETE",
    }),
  artifactHistory: (ns: string, slug: string, path: string) =>
    request<ArtifactVersion[]>(withQuery(`${project(ns, slug)}/artifacts/history`, { path })),
  artifactStat: (ns: string, slug: string, path: string) =>
    request<Artifact>(withQuery(`${project(ns, slug)}/artifacts/stat`, { path })),
  // Content is served as application/octet-stream with the version in a
  // header, so it does not go through request<T>'s JSON path.
  artifactContent: async (ns: string, slug: string, path: string, version?: number) => {
    const res = await raw(withQuery(`${project(ns, slug)}/artifacts/content`, { path, version }));
    return {
      version: Number(res.headers.get("X-Artifact-Version") ?? 0),
      hash: res.headers.get("X-Content-Hash") ?? "",
      text: await res.text(),
    };
  },

  handoffs: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<Handoff>>(withQuery(`${project(ns, slug)}/handoffs`, { cursor, limit })),
  currentHandoff: (ns: string, slug: string) =>
    request<HandoffDetail>(`${project(ns, slug)}/handoffs/current`),
  handoff: (ns: string, slug: string, id: string) =>
    request<HandoffDetail>(`${project(ns, slug)}/handoffs/${id}`),
  createHandoff: (
    ns: string,
    slug: string,
    body: { summary: string; completed_work: string; next_actions: string },
  ) => request<Handoff>(`${project(ns, slug)}/handoffs`, { method: "POST", body }),
  claimHandoff: (ns: string, slug: string, id: string) =>
    request<Handoff>(`${project(ns, slug)}/handoffs/${id}/claim`, { method: "POST" }),
  closeHandoff: (ns: string, slug: string, id: string) =>
    request<Handoff>(`${project(ns, slug)}/handoffs/${id}/close`, { method: "POST" }),

  secrets: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<SecretReference>>(withQuery(`${project(ns, slug)}/secrets`, { cursor, limit })),
  createSecret: (ns: string, slug: string, name: string, value: string) =>
    request<SecretReference>(`${project(ns, slug)}/secrets`, {
      method: "POST",
      body: { name, value },
    }),
  deleteSecret: (ns: string, slug: string, name: string) =>
    request<void>(`${project(ns, slug)}/secrets/${encodeURIComponent(name)}`, { method: "DELETE" }),

  planBoard: (ns: string, slug: string) => request<Board>(`${project(ns, slug)}/plans/board`),
  plan: (ns: string, slug: string, id: string) => request<Plan>(`${project(ns, slug)}/plans/${id}`),
  createPlan: (
    ns: string,
    slug: string,
    plan: {
      title: string;
      body?: string;
      phase_id?: string;
      items: { text: string; kind?: ItemKind }[];
    },
  ) => request<Plan>(`${project(ns, slug)}/plans/`, { method: "POST", body: plan }),
  updatePlan: (
    ns: string,
    slug: string,
    id: string,
    change: {
      status?: PlanStatus;
      phase_id?: string | null;
      title?: string;
      body?: string;
      expected_version?: number;
    },
  ) => request<Plan>(`${project(ns, slug)}/plans/${id}`, { method: "PATCH", body: change }),
  deletePlan: (ns: string, slug: string, id: string) =>
    request<void>(`${project(ns, slug)}/plans/${id}`, { method: "DELETE" }),
  addPlanItems: (
    ns: string,
    slug: string,
    id: string,
    items: { text: string; kind?: ItemKind }[],
  ) => request<Plan>(`${project(ns, slug)}/plans/${id}/items`, { method: "POST", body: { items } }),
  updatePlanItem: (ns: string, slug: string, itemID: string, change: ItemChange) =>
    request<PlanItem>(`${project(ns, slug)}/plans/items/${itemID}`, {
      method: "PATCH",
      body: change,
    }),
  // Bare arrays: search is capped on the server, history at 20 per item.
  searchPlans: (ns: string, slug: string, q: string, status?: ItemStatus) =>
    request<PlanHit[]>(withQuery(`${project(ns, slug)}/plans/search`, { q, status })),
  createPhase: (ns: string, slug: string, title: string, goal?: string) =>
    request<Phase>(`${project(ns, slug)}/phases/`, { method: "POST", body: { title, goal } }),
  updatePhase: (
    ns: string,
    slug: string,
    id: string,
    change: { status?: PhaseStatus; title?: string; goal?: string },
  ) => request<Phase>(`${project(ns, slug)}/phases/${id}`, { method: "PATCH", body: change }),
  deletePhase: (ns: string, slug: string, id: string) =>
    request<void>(`${project(ns, slug)}/phases/${id}`, { method: "DELETE" }),
  // The browser session is a client too: marking an item as yours puts you
  // on the board like any agent.
  reportPresence: (ns: string, slug: string, itemID?: string, activity?: string) =>
    request<Presence>(`${project(ns, slug)}/plans/presence`, {
      method: "POST",
      body: { item_id: itemID, activity },
    }),
  clearPresence: (ns: string, slug: string) =>
    request<void>(`${project(ns, slug)}/plans/presence`, { method: "DELETE" }),
  myPresence: () => request<{ items: Presence[]; people: Record<string, Person> }>("/me/presence"),

  gateBindings: (ns: string, slug: string) =>
    request<GateBinding[]>(`${project(ns, slug)}/gates/bindings`),
  bindGate: (
    ns: string,
    slug: string,
    body: { scope: string; scope_ref?: string; gate_name: string; hook: string; config?: unknown },
  ) =>
    request<GateBinding>(`${project(ns, slug)}/gates/bindings`, {
      method: "POST",
      body: { ...body, config: body.config ?? null },
    }),
  unbindGate: (ns: string, slug: string, bindingID: string) =>
    request<void>(withQuery(`${project(ns, slug)}/gates/bindings`, { id: bindingID }), {
      method: "DELETE",
    }),

  effectiveSkills: (ns: string, slug: string) =>
    request<SkillBinding[]>(`${project(ns, slug)}/skills/effective`),
  // Install is mounted under a project because that is what authorizes it;
  // `scope`/`scope_ref` say where the binding applies, which can be wider.
  installSkill: (
    ns: string,
    slug: string,
    body: {
      scope: string;
      scope_ref?: string;
      skill_id: string;
      skill_version_id: string;
      managed?: boolean;
      overridable?: boolean;
    },
  ) => request<SkillBinding>(`${project(ns, slug)}/skills/install`, { method: "POST", body }),
  // 204, not the updated binding — re-read the effective set afterwards.
  // Only a binding made at this project's own scope can be pinned; the
  // server refuses an inherited one with a 404 rather than silently doing
  // nothing (internal/skills.Service.setPinned).
  setSkillPinned: (ns: string, slug: string, bindingID: string, pinned: boolean) =>
    request<void>(`${project(ns, slug)}/skills/${pinned ? "pin" : "unpin"}`, {
      method: "POST",
      body: { binding_id: bindingID },
    }),
  // Publishing is mounted at the top level, not under a project: a skill can
  // be published straight onto the installation shelf, and requiring a
  // project in the path would exclude exactly that caller.
  publishSkill: (body: {
    scope: string;
    scope_ref?: string;
    skill_id?: string;
    name: string;
    author: string;
    origin: string;
    content: string;
    declared_capabilities?: string[];
  }) => request<SkillVersion>("/skills", { method: "POST", body }),
  overrideSkill: (
    ns: string,
    slug: string,
    body: {
      skill_id: string;
      skill_version_id: string;
      managed?: boolean;
      overridable?: boolean;
    },
  ) => request<SkillBinding>(`${project(ns, slug)}/skills/override`, { method: "POST", body }),
  promoteSkill: (skillID: string, scope: string, scopeRef?: string) =>
    request<{ skill_id: string; scope: string; scope_ref?: string }>("/skills/promote", {
      method: "POST",
      body: { skill_id: skillID, scope, scope_ref: scopeRef ?? null },
    }),

  conversations: (ns: string, slug: string, cursor?: string) =>
    request<Page<Conversation>>(withQuery(`${project(ns, slug)}/conversations`, { cursor })),
  historyObjects: (ns: string, slug: string, conversationID: string, cursor?: string) =>
    request<Page<HistoryObject>>(
      withQuery(`${project(ns, slug)}/conversations/${conversationID}/objects`, { cursor }),
    ),
  // A bare array: lineage is bounded by the conversation, not by how much
  // data the system holds.
  lineage: (ns: string, slug: string, conversationID: string) =>
    request<LineageEdge[]>(`${project(ns, slug)}/conversations/${conversationID}/lineage`),

  searchSkills: (query?: string, cursor?: string, limit?: number) =>
    request<Page<CatalogEntry>>(withQuery("/skills", { q: query, cursor, limit })),
  skillVersions: (skillID: string, cursor?: string) =>
    request<Page<SkillVersion>>(withQuery(`/skills/${skillID}/versions`, { cursor })),
  skillVersion: (skillVersionID: string) =>
    request<SkillContent>(`/skills/versions/${skillVersionID}`),

  audit: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<AuditRecord>>(withQuery(`${project(ns, slug)}/audit`, { cursor, limit })),
  events: (ns: string, slug: string, cursor?: string, limit?: number) =>
    request<Page<EventRecord>>(withQuery(`${project(ns, slug)}/events`, { cursor, limit })),

  createOrganization: (name: string) =>
    request<{ id: string; name: string; created_at: string }>("/organizations", {
      method: "POST",
      body: { name },
    }),
  addOrganizationMember: (id: string, userID: string, role: string) =>
    request<{ organization_id: string; user_id: string; role: string }>(
      `/organizations/${id}/members`,
      { method: "POST", body: { user_id: userID, role } },
    ),
  organizationMembers: (id: string) =>
    request<OrganizationMember[]>(`/organizations/${id}/members`),
  organizationAudit: (id: string, cursor?: string, limit?: number) =>
    request<Page<AuditRecord>>(withQuery(`/organizations/${id}/audit`, { cursor, limit })),

  installationAdmins: () => request<InstallationAdmin[]>("/installation/admins"),
  installationStats: () => request<InstallationStats>("/installation/stats"),
  installationTrends: (weeks?: number) =>
    request<InstallationTrends>(withQuery("/installation/trends", { weeks })),
  // Every namespace's storage, oldest first. Installation-admin only.
  installationStorage: (cursor?: string, limit?: number) =>
    request<Page<NamespaceStorage>>(withQuery("/installation/storage", { cursor, limit })),
  // {} puts the namespace back on the server default.
  setStorageLimit: (namespaceID: string, limit: StorageLimit) =>
    request<void>(`/installation/storage/${namespaceID}`, {
      method: "PUT",
      body:
        limit.kind === "custom"
          ? { limit_bytes: limit.bytes }
          : limit.kind === "unlimited"
            ? { unlimited: true }
            : {},
    }),
  // The directory. Installation-admin only, which is why every screen that
  // uses it falls back to a plain id field when the caller is not one.
  users: (cursor?: string, limit?: number) =>
    request<Page<InstallationUser>>(withQuery("/installation/users", { cursor, limit })),
  servicePrincipals: (cursor?: string, limit?: number) =>
    request<Page<ServicePrincipal>>(
      withQuery("/installation/service-principals", { cursor, limit }),
    ),
  securityEvents: (cursor?: string, limit?: number) =>
    request<Page<SecurityEvent>>(withQuery("/installation/security-events", { cursor, limit })),
  credentialsFor: (principalID: string) =>
    request<Credential[]>(`/installation/principals/${principalID}/tokens`),
  // Revoking someone else's credential. Mounted on the credential rather
  // than on the principal because a credential id is unique on its own.
  revokeCredentialFor: (credentialID: string) =>
    request<void>(`/installation/tokens/${credentialID}`, { method: "DELETE" }),
  // password is optional: given, the new person can sign in straight away;
  // omitted, the field is left out of the body entirely rather than sent
  // empty, because "" is a password the server would have to reject.
  createUser: (email: string, displayName: string, password?: string) =>
    request<InstallationUser>("/installation/users", {
      method: "POST",
      body: password
        ? { email, display_name: displayName, password }
        : { email, display_name: displayName },
    }),
  setUserPassword: (userID: string, password: string) =>
    request<void>(`/installation/users/${userID}/password`, {
      method: "PUT",
      body: { password },
    }),
  // The raw token comes back here and nowhere else, exactly like /me/tokens.
  issueTokenFor: (principalID: string, clientType: string, expiresInSeconds?: number) =>
    request<IssuedToken>(`/installation/principals/${principalID}/tokens`, {
      method: "POST",
      body: { client_type: clientType, expires_in_seconds: expiresInSeconds ?? null },
    }),
  createServicePrincipal: (name: string, description: string) =>
    request<ServicePrincipal>("/installation/service-principals", {
      method: "POST",
      body: { name, description },
    }),
};
