DROP POLICY IF EXISTS project_scoped ON domain_events;
DROP TABLE IF EXISTS domain_events;
DROP POLICY IF EXISTS project_scoped ON artifact_version_envelopes;
DROP TABLE IF EXISTS artifact_version_envelopes;
DROP POLICY IF EXISTS project_scoped ON artifact_versions;
DROP TABLE IF EXISTS artifact_versions;
DROP POLICY IF EXISTS project_scoped ON artifacts;
DROP TABLE IF EXISTS artifacts;
