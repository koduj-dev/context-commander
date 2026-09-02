// Thin typed wrapper over fetch. Auth is a bearer PAT (lib/auth.ts), sent as
// Authorization: Bearer <token> on every request — there is no session
// cookie to rely on (docs/adr/0009: CC has no login backend, only PATs).
import { clearToken, getToken } from "./auth";

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    if (typeof body === "string") {
      payload = body;
    } else {
      headers["Content-Type"] = "application/json";
      payload = JSON.stringify(body);
    }
  }

  const resp = await fetch(path, { method, headers, body: payload });

  if (resp.status === 401) {
    clearToken();
  }

  if (!resp.ok) {
    let code = "unknown";
    let message = `request failed with status ${resp.status}`;
    try {
      const errBody = (await resp.json()) as { error?: { code?: string; message?: string } };
      if (errBody.error) {
        code = errBody.error.code ?? code;
        message = errBody.error.message ?? message;
      }
    } catch {
      // non-JSON error body — keep the generic message
    }
    throw new ApiError(resp.status, code, message);
  }

  if (resp.status === 204) return undefined as T;
  const contentType = resp.headers.get("Content-Type") ?? "";
  if (contentType.includes("application/json")) {
    return (await resp.json()) as T;
  }
  return (await resp.text()) as unknown as T;
}

export interface Me {
  principal_id: string;
  kind: "user" | "service";
  email?: string;
  display_name?: string;
  name?: string;
}

export interface MyProject {
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
  role: string;
}

export interface MyOrganization {
  id: string;
  name: string;
  role: string;
}

export interface Project {
  id: string;
  namespace: string;
  slug: string;
  name: string;
  visibility: string;
}

export interface Artifact {
  logical_path: string;
  content_type: string;
  current_version: number;
}

export interface ArtifactVersion {
  version: number;
  content_hash: string;
  size_bytes: number;
}

export interface SecretRef {
  name: string;
  reference: string;
}

export interface GateBinding {
  id: string;
  scope: string;
  scope_ref?: string;
  gate_name: string;
  hook: string;
  config?: string;
}

export interface SkillVersion {
  id: string;
  skill_id: string;
  version: number;
  checksum: string;
  declared_capabilities: string[];
  trust_status: string;
}

export interface SkillBinding {
  id: string;
  scope: string;
  scope_ref?: string;
  skill_id: string;
  skill_version_id: string;
  pinned: boolean;
  managed: boolean;
  overridable: boolean;
}

export interface AuditRecord {
  id: number;
  timestamp: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  result: string;
  principal_id?: string;
  actor_email?: string;
}

export interface Membership {
  organization_id?: string;
  user_id?: string;
  role: string;
}

export interface Member {
  principal_id: string;
  role: string;
  created_at: string;
  email?: string;
  display_name?: string;
}

export interface IssuedToken {
  token: string;
  credential_id: string;
  client_type: string;
  expires_at?: string;
}

export interface Credential {
  id: string;
  client_type: string;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
  revoked_at?: string;
}

export const api = {
  me: () => req<Me>("GET", "/v1/me"),
  myProjects: () => req<MyProject[]>("GET", "/v1/me/projects"),
  myOrganizations: () => req<MyOrganization[]>("GET", "/v1/me/organizations"),

  issueToken: (clientType: string, expiresInSeconds?: number) =>
    req<IssuedToken>("POST", "/v1/me/tokens", { client_type: clientType, expires_in_seconds: expiresInSeconds }),
  listTokens: () => req<Credential[]>("GET", "/v1/me/tokens"),
  revokeToken: (id: string) => req<void>("DELETE", `/v1/me/tokens/${id}`),

  createProject: (project: string, name: string) =>
    req<Project>("POST", "/v1/projects", { project, name }),
  getProject: (namespace: string, slug: string) =>
    req<Project>("GET", `/v1/projects/${namespace}/${slug}/`),
  addMember: (namespace: string, slug: string, principalId: string, role: string) =>
    req<void>("POST", `/v1/projects/${namespace}/${slug}/members`, { principal_id: principalId, role }),
  listMembers: (namespace: string, slug: string) =>
    req<Member[]>("GET", `/v1/projects/${namespace}/${slug}/members`),

  listArtifacts: (namespace: string, slug: string) =>
    req<Artifact[]>("GET", `/v1/projects/${namespace}/${slug}/artifacts`),
  artifactHistory: (namespace: string, slug: string, path: string) =>
    req<ArtifactVersion[]>("GET", `/v1/projects/${namespace}/${slug}/artifacts/history?path=${encodeURIComponent(path)}`),
  readArtifact: (namespace: string, slug: string, path: string) =>
    req<string>("GET", `/v1/projects/${namespace}/${slug}/artifacts/content?path=${encodeURIComponent(path)}`),

  listSecrets: (namespace: string, slug: string) =>
    req<SecretRef[]>("GET", `/v1/projects/${namespace}/${slug}/secrets`),
  createSecret: (namespace: string, slug: string, name: string, value: string) =>
    req<SecretRef>("POST", `/v1/projects/${namespace}/${slug}/secrets`, { name, value }),
  deleteSecret: (namespace: string, slug: string, name: string) =>
    req<void>("DELETE", `/v1/projects/${namespace}/${slug}/secrets/${encodeURIComponent(name)}`),

  listEffectiveSkills: (namespace: string, slug: string) =>
    req<SkillBinding[]>("GET", `/v1/projects/${namespace}/${slug}/skills/effective`),
  publishSkill: (
    namespace: string,
    slug: string,
    body: { skill_id?: string; name: string; author: string; origin: string; content: string; declared_capabilities: string[] },
  ) => req<SkillVersion>("POST", `/v1/projects/${namespace}/${slug}/skills`, body),
  installSkill: (
    namespace: string,
    slug: string,
    body: { scope: string; scope_ref?: string; skill_id: string; skill_version_id: string; managed: boolean; overridable: boolean },
  ) => req<SkillBinding>("POST", `/v1/projects/${namespace}/${slug}/skills/install`, body),
  overrideSkill: (
    namespace: string,
    slug: string,
    body: { skill_id: string; skill_version_id: string; managed: boolean; overridable: boolean },
  ) => req<SkillBinding>("POST", `/v1/projects/${namespace}/${slug}/skills/override`, body),
  pinSkillBinding: (namespace: string, slug: string, bindingId: string) =>
    req<void>("POST", `/v1/projects/${namespace}/${slug}/skills/pin`, { binding_id: bindingId }),
  unpinSkillBinding: (namespace: string, slug: string, bindingId: string) =>
    req<void>("POST", `/v1/projects/${namespace}/${slug}/skills/unpin`, { binding_id: bindingId }),

  listGateBindings: (namespace: string, slug: string) =>
    req<GateBinding[]>("GET", `/v1/projects/${namespace}/${slug}/gates/bindings`),
  bindGate: (
    namespace: string,
    slug: string,
    body: { scope: string; scope_ref?: string; gate_name: string; hook: string; config: unknown },
  ) => req<GateBinding>("POST", `/v1/projects/${namespace}/${slug}/gates/bindings`, body),
  unbindGate: (namespace: string, slug: string, id: string) =>
    req<void>("DELETE", `/v1/projects/${namespace}/${slug}/gates/bindings?id=${encodeURIComponent(id)}`),

  queryAudit: (
    namespace: string,
    slug: string,
    opts: { action?: string; resource_type?: string; since?: string; until?: string; limit?: number } = {},
  ) => {
    const params = new URLSearchParams();
    if (opts.action) params.set("action", opts.action);
    if (opts.resource_type) params.set("resource_type", opts.resource_type);
    if (opts.since) params.set("since", opts.since);
    if (opts.until) params.set("until", opts.until);
    if (opts.limit) params.set("limit", String(opts.limit));
    const qs = params.toString();
    return req<AuditRecord[]>("GET", `/v1/projects/${namespace}/${slug}/audit${qs ? `?${qs}` : ""}`);
  },

  createOrganization: (name: string) => req<MyOrganization>("POST", "/v1/organizations", { name }),
  addOrganizationMember: (orgId: string, userId: string, role: string) =>
    req<Membership>("POST", `/v1/organizations/${orgId}/members`, { user_id: userId, role }),
};
