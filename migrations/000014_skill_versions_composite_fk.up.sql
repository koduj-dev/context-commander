-- skill_bindings had two independent foreign keys — skill_id -> skills(id)
-- and skill_version_id -> skill_versions(id) — with nothing tying them
-- together. Install/Override accepted both UUIDs independently, so a
-- binding could claim skill A's identity while pinning a SkillVersion that
-- actually belongs to skill B. ListEffective groups/shadows by the claimed
-- skill_id, so a consumer resolving skill A got skill B's content without
-- either its identity or its policy (docs/adr/0012 supply-chain guarantee)
-- ever being evaluated.
--
-- A composite (id, skill_id) key on skill_versions lets skill_bindings
-- reference (skill_version_id, skill_id) together, so the database itself
-- rejects a mismatched pair.

ALTER TABLE skill_versions ADD CONSTRAINT skill_versions_id_skill_id_key UNIQUE (id, skill_id);

ALTER TABLE skill_bindings DROP CONSTRAINT skill_bindings_skill_version_id_fkey;
ALTER TABLE skill_bindings
  ADD CONSTRAINT skill_bindings_skill_version_id_skill_id_fkey
  FOREIGN KEY (skill_version_id, skill_id) REFERENCES skill_versions (id, skill_id);
