-- Security events — the durable record of things that were REFUSED.
--
-- Why this is not audit_log: audit_log is project-scoped and its RLS policy
-- is "USING (cc_has_project_access(...))" with no separate WITH CHECK, so
-- Postgres reuses that expression as the write check too. A row recording
-- "this principal was refused access to project X" therefore cannot be
-- inserted by that principal — precisely the case worth recording. And an
-- authentication failure has no principal and no project at all, so it has
-- nowhere to go in a table whose every row is scoped to one project.
--
-- Two independent drivers, one table:
--   * incident analysis — reconstructing what was attempted, by which
--     credential, in what order, after something goes wrong;
--   * CRA Annex I (2)(l), which asks a product to "provide security related
--     information by recording and monitoring relevant internal activity,
--     including the access to or modification of data, services or
--     functions, with an opt-out mechanism for the user". The opt-out is
--     `ccd serve --security-log=off`; see internal/security.
--
-- No RLS, deliberately — the same trust model as installation_admins
-- (migrations/000017) and for the same reason: this table is never queried
-- directly by adapter code, only through internal/security.Service, which
-- gates reads on installation-admin rights (docs/adr/0017). Making it
-- project-scoped would recreate exactly the hole described above.
CREATE TABLE security_events (
  id   bigserial PRIMARY KEY,
  ts   timestamptz NOT NULL DEFAULT now(),
  kind text NOT NULL CHECK (kind IN ('authentication_failed', 'authorization_denied', 'rate_limited')),

  -- Deliberately NOT foreign keys. The whole point of this table is to
  -- record identifiers that were presented and turned out to be wrong: a
  -- credential id parsed from a bearer token that matches no credential is
  -- the single most interesting row here, and an FK would reject it. The
  -- ids are therefore evidence of what was claimed, never proof that the
  -- thing exists.
  principal_id  uuid,
  credential_id uuid,
  project_id    uuid,

  -- For authorization_denied: the capability that was refused
  -- (internal/auth.Capability). Null for the other kinds.
  capability text,
  -- Short, fixed, non-user-controlled reason. Never a request body, never
  -- a token, never project content — this table is readable by every
  -- installation admin and must stay free of anything confidential.
  reason     text NOT NULL,
  request_id text
);

-- The two queries an investigation actually runs: "what happened, newest
-- first" and "what did this principal do".
CREATE INDEX security_events_ts_idx ON security_events (ts DESC, id DESC);
CREATE INDEX security_events_principal_ts_idx ON security_events (principal_id, ts DESC);

-- No UPDATE, no DELETE: append-only for cc_app, the same technique
-- artifact_versions and skill_versions use to make "just tweak the record"
-- fail at the database rather than at review. Retention/purge is an
-- operator concern (a future migration or an owner-privileged job), not
-- something the running server may do to its own security record.
GRANT SELECT, INSERT ON security_events TO cc_app;
GRANT USAGE, SELECT ON SEQUENCE security_events_id_seq TO cc_app;
