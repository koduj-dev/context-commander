-- Phase 1: identity, namespace, organization, project membership, RBAC, RLS.
-- Run as cc_owner (see docs/adr/0002-non-recursive-rls.md). Role creation
-- itself (cc_owner/cc_app) is cluster-level provisioning, not a migration —
-- see deploy/postgres/init/01-roles.sql for local dev.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- ---------------------------------------------------------------------------
-- Identity — docs/adr/0004-principal-model.md
-- ---------------------------------------------------------------------------

CREATE TABLE users (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email        text UNIQUE NOT NULL,
  display_name text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE service_principals (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  description text,
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- Principal = User XOR ServicePrincipal, never both, never neither.
CREATE TABLE principals (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind                  text NOT NULL CHECK (kind IN ('user', 'service')),
  user_id               uuid REFERENCES users (id),
  service_principal_id  uuid REFERENCES service_principals (id),
  created_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT principals_kind_matches_ref CHECK (
    (kind = 'user'    AND user_id IS NOT NULL AND service_principal_id IS NULL) OR
    (kind = 'service' AND service_principal_id IS NOT NULL AND user_id IS NULL)
  ),
  CONSTRAINT principals_user_unique UNIQUE (user_id),
  CONSTRAINT principals_service_principal_unique UNIQUE (service_principal_id)
);

-- PAT format cc_<credential-id>_<random-secret> — docs/adr/0009-pat-format-and-bootstrap.md.
-- credential-id (this row's id) is the public lookup key; token_hash hashes
-- only the random-secret part. Never store or compare the raw secret.
CREATE TABLE client_credentials (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  principal_id uuid NOT NULL REFERENCES principals (id),
  client_type  text NOT NULL,
  token_hash   text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  expires_at   timestamptz,
  revoked_at   timestamptz
);

CREATE INDEX client_credentials_principal_id_idx ON client_credentials (principal_id);

-- ---------------------------------------------------------------------------
-- Organization — docs/adr/0006-organization-vs-project-authorization.md
-- ---------------------------------------------------------------------------

CREATE TABLE organizations (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organization_members (
  organization_id uuid NOT NULL REFERENCES organizations (id),
  user_id         uuid NOT NULL REFERENCES users (id),
  role            text NOT NULL CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, user_id)
);

-- ---------------------------------------------------------------------------
-- Namespace & Project — docs/adr/0001-project-namespace-not-global-slug.md
-- ---------------------------------------------------------------------------

CREATE TABLE namespaces (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind            text NOT NULL CHECK (kind IN ('personal', 'organization')),
  slug            text NOT NULL UNIQUE,
  owner_user_id   uuid REFERENCES users (id),
  organization_id uuid REFERENCES organizations (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT namespaces_kind_matches_ref CHECK (
    (kind = 'personal'     AND owner_user_id IS NOT NULL AND organization_id IS NULL) OR
    (kind = 'organization' AND organization_id IS NOT NULL AND owner_user_id IS NULL)
  ),
  CONSTRAINT namespaces_owner_user_unique UNIQUE (owner_user_id),
  CONSTRAINT namespaces_organization_unique UNIQUE (organization_id)
);

CREATE TABLE projects (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  namespace_id uuid NOT NULL REFERENCES namespaces (id),
  slug         text NOT NULL,
  name         text NOT NULL,
  visibility   text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'organization', 'public')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (namespace_id, slug)
);
-- Reminder (docs/adr/0006-*): `visibility` is discovery metadata only.
-- AuthzService/RLS must never read it as an authorization signal.

CREATE TABLE project_members (
  project_id   uuid NOT NULL REFERENCES projects (id),
  principal_id uuid NOT NULL REFERENCES principals (id),
  role         text NOT NULL CHECK (role IN ('OWNER', 'ADMIN', 'CONTRIBUTOR', 'READER')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, principal_id)
);

-- ---------------------------------------------------------------------------
-- Non-recursive RLS — docs/adr/0002-non-recursive-rls.md
-- ---------------------------------------------------------------------------

-- Takes principal id as TEXT, not uuid, and casts internally: a custom GUC
-- like app.current_principal_id reads as NULL via current_setting(..., true)
-- only the *first* time it's touched on a backend. Once anything has called
-- set_config() on it (i.e. after the first WithPrincipal transaction on a
-- pooled connection), current_setting(..., true) returns '' — not NULL —
-- for every later transaction that doesn't set it again. USING(... ::uuid)
-- at the policy level would then hard-error ("invalid input syntax for type
-- uuid") on ANY query that reaches this table outside WithPrincipal, on a
-- connection that has ever had a principal set. Guarding + casting inside
-- the function turns that into a safe DENY instead of a runtime error.
CREATE FUNCTION cc_has_project_access(p_principal_id_text text, p_project_id uuid)
RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  p_principal_id uuid;
BEGIN
  IF p_principal_id_text IS NULL OR p_principal_id_text = '' THEN
    RETURN false;
  END IF;

  BEGIN
    p_principal_id := p_principal_id_text::uuid;
  EXCEPTION WHEN invalid_text_representation THEN
    RETURN false;
  END;

  RETURN EXISTS (
    SELECT 1 FROM project_members
    WHERE principal_id = p_principal_id AND project_id = p_project_id
  );
END;
$$;

REVOKE ALL ON FUNCTION cc_has_project_access(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_has_project_access(text, uuid) TO cc_app;

ALTER TABLE project_members ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON project_members
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));

-- ---------------------------------------------------------------------------
-- Audit — docs/adr/0011-encryption-classification.md (allowlisted columns only)
-- ---------------------------------------------------------------------------

CREATE TABLE audit_log (
  id            bigserial PRIMARY KEY,
  ts            timestamptz NOT NULL DEFAULT now(),
  project_id    uuid REFERENCES projects (id),
  principal_id  uuid REFERENCES principals (id),
  client_id     uuid REFERENCES client_credentials (id),
  action        text NOT NULL,
  resource_type text NOT NULL,
  resource_id   uuid,
  result        text NOT NULL CHECK (result IN ('allow', 'deny', 'error')),
  request_id    text,
  event_id      uuid
  -- No free-form jsonb/content column here — see encrypted_audit_payload below
  -- for any payload that must accompany an audit row.
);

CREATE INDEX audit_log_project_id_ts_idx ON audit_log (project_id, ts);

CREATE TABLE encrypted_audit_payload (
  audit_log_id bigint NOT NULL REFERENCES audit_log (id),
  ciphertext   bytea NOT NULL,
  nonce        bytea NOT NULL,
  wrapped_dek  bytea NOT NULL
);

-- ---------------------------------------------------------------------------
-- Grants for cc_app — narrow, per-table, issued as tables are created.
-- ---------------------------------------------------------------------------

GRANT SELECT, INSERT, UPDATE ON users, service_principals, principals, client_credentials TO cc_app;
GRANT SELECT, INSERT, UPDATE ON organizations, organization_members TO cc_app;
GRANT SELECT, INSERT, UPDATE ON namespaces, projects, project_members TO cc_app;
GRANT SELECT, INSERT ON audit_log, encrypted_audit_payload TO cc_app;
GRANT USAGE, SELECT ON audit_log_id_seq TO cc_app;
