DROP INDEX skill_versions_published_in_project_idx;
ALTER TABLE skill_versions DROP COLUMN published_by;
ALTER TABLE skill_versions DROP COLUMN published_in_project;
