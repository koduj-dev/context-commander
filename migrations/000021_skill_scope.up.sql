-- Where a skill LIVES, as opposed to where it is applied.
--
-- docs/adr/0012 gave SkillBinding a scope (installation/organization/
-- project) and gave the package none, so "who may see this skill" had no
-- answer in the model — only the accident that `skills`/`skill_versions`
-- carried no RLS. docs/adr/0018 answers it: a package lives at a scope, the
-- installation scope is the instance-wide marketplace shelf, and the
-- registry is browsable by whoever can see the entry.
--
-- The vocabulary is deliberately identical to skill_bindings' (and
-- gate_bindings'): scopes compose the same way, and a reader who has
-- understood one has understood all three.
ALTER TABLE skills ADD COLUMN scope     text;
ALTER TABLE skills ADD COLUMN scope_ref uuid;

-- Backfill from the provenance migration 000020 recorded. A skill whose
-- versions all predate that migration has no project to attribute it to and
-- becomes installation-scoped: that is the visibility it was published
-- under, and silently narrowing an existing skill would break whoever is
-- already using it.
UPDATE skills s SET scope = 'project', scope_ref = v.published_in_project
FROM (
  SELECT DISTINCT ON (skill_id) skill_id, published_in_project
  FROM skill_versions WHERE published_in_project IS NOT NULL
  ORDER BY skill_id, version ASC
) v
WHERE v.skill_id = s.id;

UPDATE skills SET scope = 'installation', scope_ref = NULL WHERE scope IS NULL;

ALTER TABLE skills ALTER COLUMN scope SET NOT NULL;
ALTER TABLE skills ALTER COLUMN scope SET DEFAULT 'project';
ALTER TABLE skills ADD CONSTRAINT skills_scope_check CHECK (scope IN ('installation', 'organization', 'project'));
-- Same shape as skill_bindings: a scope_ref for the narrower scopes, none
-- for installation.
ALTER TABLE skills ADD CONSTRAINT skills_scope_ref_check CHECK ((scope = 'installation') = (scope_ref IS NULL));

CREATE INDEX skills_scope_idx ON skills (scope, scope_ref);
-- Search is by name, case-insensitively, across whatever the caller can
-- see. Without this every catalog query is a sequential scan behind an RLS
-- filter.
CREATE INDEX skills_name_lower_idx ON skills (lower(name));

-- Visibility, mirroring skill_bindings' policy exactly (migrations/000007,
-- tightened by 000013). Real authorization is still the Service-layer check
-- (docs/adr/0014); this is the database saying the same thing (docs/adr/0002).
ALTER TABLE skills ENABLE ROW LEVEL SECURITY;
CREATE POLICY skills_scoped ON skills
  USING (
    scope = 'installation'
    OR (scope = 'organization' AND cc_has_org_access(current_setting('app.current_principal_id', true), scope_ref))
    OR (scope = 'project' AND cc_has_project_access(current_setting('app.current_principal_id', true), scope_ref))
  );

-- skill_versions has no scope of its own: a version goes wherever its skill
-- goes, so it inherits through the FK rather than denormalizing a column
-- that could then disagree with its parent. Same technique
-- encrypted_audit_payload uses to inherit audit_log's scoping
-- (migrations/000006).
ALTER TABLE skill_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY skill_versions_scoped ON skill_versions
  USING (EXISTS (SELECT 1 FROM skills s WHERE s.id = skill_versions.skill_id));

-- Promotion moves a skill up a scope, so the catalog row is now mutable in
-- exactly one respect. No DELETE: a published skill is not unpublished.
GRANT UPDATE (scope, scope_ref) ON skills TO cc_app;
