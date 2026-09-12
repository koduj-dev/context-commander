-- One binding per (scope, scope_ref, skill_id), and installing again moves
-- it to the new version.
--
-- Without this, "project X uses skill Y" could be true twice at once with
-- two different versions, and internal/skills.ListEffective picked between
-- them by whichever row the database happened to return first — its
-- composition rule only compares scope rank, so two bindings of equal rank
-- left the winner undefined. The practical consequence was that a project
-- had NO WAY to move to a newer version of a skill: skill.install added a
-- second row, ADR 0012 forbids skill.publish from touching a pinned
-- binding, and no other operation existed. Found while testing
-- `ccd sync-skills`, which correctly reported "unchanged" for a skill whose
-- new version had just been installed.
--
-- Deduplicate first, keeping the most recently installed row per group —
-- the one whoever installed last meant to be in effect.
DELETE FROM skill_bindings b
USING skill_bindings newer
WHERE b.skill_id = newer.skill_id
  AND b.scope = newer.scope
  AND b.scope_ref IS NOT DISTINCT FROM newer.scope_ref
  AND (newer.installed_at, newer.id) > (b.installed_at, b.id);

-- NULLS NOT DISTINCT because scope_ref is NULL for installation scope
-- (migrations/000007's CHECK), and the default SQL rule — every NULL
-- distinct from every other — would exempt exactly the installation-wide
-- bindings from the constraint. Postgres 15+; the project targets 16.
ALTER TABLE skill_bindings
  ADD CONSTRAINT skill_bindings_one_per_scope UNIQUE NULLS NOT DISTINCT (scope, scope_ref, skill_id);
