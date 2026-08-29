-- Skill Registry — docs/adr/0012-skill-registry-supply-chain.md,
-- docs/architecture-cookbook.md §12.

-- Stable identity across versions. Global, installation-wide catalog — no
-- project_id, no RLS: every authenticated principal in this installation
-- can see what skills exist (the trust decision is per-binding, via
-- skill_bindings, not per-catalog-entry).
CREATE TABLE skills (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL,
  author     text,
  origin     text,
  created_at timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT ON skills TO cc_app;

-- Immutable once inserted (ADR 0012's "skill.publish creates a new
-- SkillVersion; skill.update never touches published content"): no
-- UPDATE/DELETE grant, so a future "just tweak the published content" code
-- path fails at the database, not just at review — same technique as
-- artifact_versions (migrations/000003).
CREATE TABLE skill_versions (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  skill_id              uuid NOT NULL REFERENCES skills (id),
  version               int NOT NULL,
  checksum              text NOT NULL,
  -- ObjectStore key holding the packaged skill content. Deliberately
  -- unencrypted in v1: internal/crypto.Provider's key hierarchy is
  -- root -> PROJECT key -> DEK (docs/adr/0005) and an
  -- installation/organization-scoped skill has no project to anchor a key
  -- to. See docs/gotchas.md.
  storage_key           text NOT NULL,
  files                 jsonb NOT NULL DEFAULT '[]',
  declared_capabilities text[] NOT NULL DEFAULT '{}',
  trust_status          text NOT NULL DEFAULT 'unverified',
  signature             bytea,
  created_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (skill_id, version)
);

CREATE INDEX skill_versions_skill_id_idx ON skill_versions (skill_id);

GRANT SELECT, INSERT ON skill_versions TO cc_app;

-- "Project X uses skill Y" — mutable, scoped, always pinned to one concrete
-- SkillVersion in v1 (no version-range resolver yet).
CREATE TABLE skill_bindings (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  scope            text NOT NULL CHECK (scope IN ('installation', 'organization', 'project')),
  scope_ref        uuid,
  skill_id         uuid NOT NULL REFERENCES skills (id),
  skill_version_id uuid NOT NULL REFERENCES skill_versions (id),
  pinned           boolean NOT NULL DEFAULT true,
  managed          boolean NOT NULL DEFAULT false,
  overridable      boolean NOT NULL DEFAULT true,
  installed_by     uuid REFERENCES principals (id),
  installed_at     timestamptz NOT NULL DEFAULT now(),
  CHECK ((scope = 'installation') = (scope_ref IS NULL))
);

CREATE INDEX skill_bindings_scope_idx ON skill_bindings (scope, scope_ref);
CREATE INDEX skill_bindings_skill_id_idx ON skill_bindings (skill_id);

ALTER TABLE skill_bindings ENABLE ROW LEVEL SECURITY;

-- installation scope: visible to every authenticated principal (one
-- installation, everyone in it). organization scope: org members only
-- (cc_has_org_access, migrations/000005). project scope: project members
-- only (cc_has_project_access, migrations/000001). No WITH CHECK given, so
-- for ALL commands Postgres reuses this USING expression as the write
-- check too — safe here because, unlike organizations' bootstrap problem,
-- scope_ref always names an *existing* project/org the caller must already
-- belong to before creating a binding into it. Real authorization is the
-- Service-layer capability check (docs/adr/0014); this is defense in depth.
CREATE POLICY skill_bindings_scoped ON skill_bindings
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

GRANT SELECT, INSERT, UPDATE, DELETE ON skill_bindings TO cc_app;
