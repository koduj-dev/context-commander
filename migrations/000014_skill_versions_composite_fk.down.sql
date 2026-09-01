ALTER TABLE skill_bindings DROP CONSTRAINT skill_bindings_skill_version_id_skill_id_fkey;
ALTER TABLE skill_bindings
  ADD CONSTRAINT skill_bindings_skill_version_id_fkey
  FOREIGN KEY (skill_version_id) REFERENCES skill_versions (id);

ALTER TABLE skill_versions DROP CONSTRAINT skill_versions_id_skill_id_key;
