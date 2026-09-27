-- Installation-wide usage counts, for an operator asking "how much is this
-- actually used".
--
-- The reason this needs a function at all: almost everything worth counting
-- lives behind RLS, and an installation admin is NOT a member of every
-- project. Counting `artifacts` through cc_app returns the admin's own
-- projects, which is a different and misleading number — it would read as
-- "the installation holds 2 artifacts" when the truth is "you can see 2".
-- There is no path by which an installation admin can add themselves to a
-- project (project.add_member requires CapMemberManage on that project), and
-- that boundary is deliberate: the admin role administers the installation,
-- it does not read its content.
--
-- So: SECURITY DEFINER, and the narrowest possible result. This function
-- returns CARDINALITIES AND ONE BYTE TOTAL AND NOTHING ELSE. It cannot
-- return a logical path, a handoff summary, a skill name, a project slug or
-- an email, because it never selects one. Widening it to return any row-level
-- value would be a different decision from this one, and this comment is
-- where that decision should be argued.
--
-- What it does leak, stated plainly: existence. An operator learns that 47
-- artifacts exist in projects they cannot open. That is the minimum an
-- operator needs to answer "is this installation used, and is it growing",
-- and it is the whole of what they gain.
CREATE FUNCTION cc_installation_stats()
RETURNS TABLE (
  projects            bigint,
  organizations       bigint,
  users               bigint,
  service_principals  bigint,
  artifacts           bigint,
  artifact_versions   bigint,
  stored_bytes        bigint,
  skills              bigint,
  skill_versions      bigint,
  skill_bindings      bigint,
  handoffs_open       bigint,
  handoffs_claimed    bigint,
  handoffs_closed     bigint,
  conversations       bigint,
  history_objects     bigint,
  events              bigint,
  audit_records       bigint,
  security_events     bigint,
  secrets             bigint,
  gate_bindings       bigint,
  credentials_active  bigint,
  credentials_revoked bigint,
  credentials_expired bigint,
  active_last_7_days  bigint,
  oldest_project      timestamptz,
  newest_activity     timestamptz
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  SELECT
    (SELECT count(*) FROM projects),
    (SELECT count(*) FROM organizations),
    (SELECT count(*) FROM users),
    (SELECT count(*) FROM service_principals),
    -- Live artifacts only: a deleted one is not storage anyone is using, and
    -- counting it would make the number disagree with every project screen.
    (SELECT count(*) FROM artifacts WHERE deleted_at IS NULL),
    (SELECT count(*) FROM artifact_versions),
    -- The logical size of every version ever written, which is what the
    -- object store holds while nothing prunes it. Deliberately not disk
    -- usage: the database cannot see the filesystem or the bucket, and a
    -- number labelled "disk" that came from SQL would be a guess.
    (SELECT COALESCE(sum(size_bytes), 0) FROM artifact_versions),
    (SELECT count(*) FROM skills),
    (SELECT count(*) FROM skill_versions),
    (SELECT count(*) FROM skill_bindings),
    (SELECT count(*) FROM handoffs WHERE status = 'open'),
    (SELECT count(*) FROM handoffs WHERE status = 'claimed'),
    (SELECT count(*) FROM handoffs WHERE status = 'closed'),
    (SELECT count(*) FROM conversations),
    (SELECT count(*) FROM history_objects),
    (SELECT count(*) FROM domain_events),
    (SELECT count(*) FROM audit_log),
    (SELECT count(*) FROM security_events),
    (SELECT count(*) FROM secret_values),
    (SELECT count(*) FROM gate_bindings),
    -- A credential is "active" only if it is neither revoked nor past its
    -- expiry. Counting un-revoked ones alone would report an expired token as
    -- usable, which is the opposite of what an operator is checking for.
    (SELECT count(*) FROM client_credentials
      WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())),
    (SELECT count(*) FROM client_credentials WHERE revoked_at IS NOT NULL),
    (SELECT count(*) FROM client_credentials
      WHERE revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= now()),
    -- The one number that actually answers "is this used" rather than "what
    -- is stored": credentials that made a request in the last week.
    (SELECT count(*) FROM client_credentials WHERE last_used_at > now() - interval '7 days'),
    (SELECT min(created_at) FROM projects),
    -- The newest thing that happened anywhere. domain_events is the outbox
    -- every mutation writes to, so it is the one honest "last activity"
    -- across all domains.
    (SELECT max(created_at) FROM domain_events);
$$;

REVOKE ALL ON FUNCTION cc_installation_stats() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_installation_stats() TO cc_app;
