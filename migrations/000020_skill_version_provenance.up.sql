-- Where a SkillVersion came from.
--
-- skills.Service.Publish has always taken a projectID, checked
-- CapSkillPublish on it, and then thrown that fact away — so "which
-- project does this package belong to" had no answer, and
-- skills.Service.GetVersion (added in Phase 1) consequently had nothing to
-- authorize a read against except an anchor project the caller had already
-- picked for themselves. Any principal who knew a version UUID could read
-- content published inside a project they were not a member of.
--
-- This is deliberately NOT a decision about what the skill registry is —
-- whether packages are instance-wide, organization-scoped, or promoted
-- explicitly is a product question (docs/adr/0012 scopes the *binding*, not
-- the package, and NEXT.md keeps a public marketplace behind trust and
-- signing). It records the provenance that was already known and discarded,
-- which is enough to make reads answerable today and is a prerequisite for
-- any of those answers later.
--
-- published_by is the same fact for supply chain purposes: ADR 0012 frames
-- the registry as a distribution/trust platform, and "who published this"
-- is trust metadata that was missing from a table full of trust metadata.
ALTER TABLE skill_versions ADD COLUMN published_in_project uuid REFERENCES projects (id);
ALTER TABLE skill_versions ADD COLUMN published_by         uuid REFERENCES principals (id);

CREATE INDEX skill_versions_published_in_project_idx ON skill_versions (published_in_project);

-- Nullable, and null means "provenance unknown". Publish sets both columns
-- on every row it writes from here on, so only rows predating this
-- migration can be null; internal/skills reads those as installation-wide,
-- the behaviour they were published under. Making the column NOT NULL would
-- mean a migration that fails on any existing row, which is a worse trade
-- than one honest null.
COMMENT ON COLUMN skill_versions.published_in_project IS
  'Project the version was published from; null only for rows predating migration 000020, which are treated as installation-wide.';
