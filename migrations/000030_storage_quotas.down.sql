DROP TRIGGER IF EXISTS projects_namespace_storage ON projects;
DROP TRIGGER IF EXISTS skills_scope_storage ON skills;
DROP TRIGGER IF EXISTS skill_versions_storage ON skill_versions;
DROP TRIGGER IF EXISTS history_objects_storage ON history_objects;
DROP TRIGGER IF EXISTS artifacts_deleted_storage ON artifacts;
DROP TRIGGER IF EXISTS artifact_versions_storage ON artifact_versions;

DROP FUNCTION IF EXISTS cc_project_namespace_storage();
DROP FUNCTION IF EXISTS cc_skill_scope_storage();
DROP FUNCTION IF EXISTS cc_skill_version_storage();
DROP FUNCTION IF EXISTS cc_history_object_storage();
DROP FUNCTION IF EXISTS cc_artifact_deleted();
DROP FUNCTION IF EXISTS cc_artifact_version_storage();

DROP FUNCTION IF EXISTS cc_set_namespace_quota(uuid, bigint, boolean, uuid);
DROP FUNCTION IF EXISTS cc_my_storage();
DROP FUNCTION IF EXISTS cc_namespace_storage_page(timestamptz, uuid, int);
DROP FUNCTION IF EXISTS cc_check_skill_storage(text, uuid, bigint);
DROP FUNCTION IF EXISTS cc_check_project_storage(uuid, bigint);
DROP FUNCTION IF EXISTS cc_skill_scope_namespace(text, uuid);
DROP FUNCTION IF EXISTS cc_project_namespace(uuid);
DROP FUNCTION IF EXISTS cc_check_storage(uuid, bigint);
DROP FUNCTION IF EXISTS cc_charge_storage(uuid, bigint);
DROP FUNCTION IF EXISTS cc_storage_quota_exceeded();
DROP FUNCTION IF EXISTS cc_storage_effective_limit(boolean, bigint);
DROP FUNCTION IF EXISTS cc_storage_default();

ALTER TABLE skill_versions DROP COLUMN IF EXISTS size_bytes;
DROP TABLE IF EXISTS namespace_storage;
