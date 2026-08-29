-- Gates — docs/architecture-cookbook.md §13.2/§13.3. In-process registry of
-- Go-implemented providers (internal/gates.Register), with which provider
-- runs on which hook for which scope stored here as data — mirrors
-- skill_bindings' scope composition (migrations/000007).

CREATE TABLE gate_bindings (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  scope      text NOT NULL CHECK (scope IN ('installation', 'organization', 'project')),
  scope_ref  uuid,
  gate_name  text NOT NULL,
  hook       text NOT NULL,
  enabled    boolean NOT NULL DEFAULT true,
  config     jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((scope = 'installation') = (scope_ref IS NULL))
);

CREATE INDEX gate_bindings_hook_idx ON gate_bindings (hook, scope, scope_ref) WHERE enabled;

ALTER TABLE gate_bindings ENABLE ROW LEVEL SECURITY;

-- Same scope-visibility shape as skill_bindings_scoped
-- (migrations/000007) — real authorization is the Service-layer capability
-- check, this is defense in depth.
CREATE POLICY gate_bindings_scoped ON gate_bindings
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

GRANT SELECT, INSERT, UPDATE, DELETE ON gate_bindings TO cc_app;
