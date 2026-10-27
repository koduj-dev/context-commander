-- An agent's name is unique among the agents one person registered.
--
-- Decided by the maintainer on 2026-10-27, after migrations/000001 left
-- service_principals.name unconstrained while users.email is unique: a name
-- two agents share is a poor thing to tell them apart by, but an
-- installation-wide rule would make one person's "codex" block everyone
-- else's. The scope is therefore the registrar, `created_by`
-- (migrations/000026) — the only namespace an agent has.
--
-- Case-insensitive, like users_email_lower_key: "Codex" and "codex" in the
-- same list are the same confusion as two "codex".
--
-- Agents with no recorded registrar (created_by IS NULL) fall outside the
-- rule, and that is on purpose rather than an accident of NULL semantics.
-- Two kinds of agent have none: those registered before 000026, and those an
-- operator registers with `ccd installation create-service-principal`, which
-- runs against the database with no signed-in person to name. Neither has a
-- namespace to be unique in, and inventing one — "the operator", "nobody" —
-- would make the first such agent's name unusable for every later one. The
-- same applies when a registrar's account is deleted (ON DELETE SET NULL):
-- their agents keep running, keep their names, and stop belonging to a
-- namespace, which is the loss 000026 already chose.
--
-- Existing duplicates within one registrar are renamed, not refused. A
-- migration that fails on existing data stops the server from starting,
-- which turns a cosmetic clash into an outage. The oldest agent keeps the
-- name; each later one gets its id's first eight characters appended, which
-- is unique by construction where a counter ("codex (2)") could collide
-- with a name someone already chose. Nothing looks an agent up by name —
-- tokens, memberships and audit rows all reference the id — so a rename
-- changes what a screen shows and nothing that works.
UPDATE service_principals sp
SET name = sp.name || ' (' || left(sp.id::text, 8) || ')'
FROM (
  SELECT id,
         row_number() OVER (
           PARTITION BY created_by, lower(name) ORDER BY created_at, id
         ) AS n
  FROM service_principals
  WHERE created_by IS NOT NULL
) dup
WHERE sp.id = dup.id AND dup.n > 1;

CREATE UNIQUE INDEX service_principals_registrar_name_key
  ON service_principals (created_by, lower(name))
  WHERE created_by IS NOT NULL;
