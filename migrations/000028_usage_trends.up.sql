-- Usage over time, for an installation admin asking "is this used, and is
-- the handoff loop actually working" — the two questions a snapshot of
-- counts (migrations/000024) cannot answer, because a total only grows.
--
-- The same boundary as 000024, and argued the same way: every function here
-- is SECURITY DEFINER because the admin is not a member of every project,
-- and every one returns CARDINALITIES AND DURATIONS AND NOTHING ELSE — never
-- a principal, a project, a path or a summary. Asked by the maintainer on
-- 2026-10-27 for exactly that shape: "aggregated, not per user".
--
-- What aggregation does not hide, stated plainly: on an installation with
-- one person, "1 active person this week" is that person. A count is only as
-- anonymous as the population is large. The figures say how much, never who;
-- on a tiny installation the operator already knows who.

-- ---------------------------------------------------------------------------
-- Which days a principal made an authenticated request.
--
-- The one fact the schema did not keep. client_credentials.last_used_at holds
-- only the latest use, so "how many people used this each week" — the
-- retention question NEXT.md's metrics ask — was unanswerable after the
-- fact. One row per principal per UTC day, written best-effort beside
-- last_used_at (internal/identity/authenticate.go), so a busy agent costs one
-- row a day rather than one per request.
--
-- Deliberately unreadable to the application: cc_app may INSERT and nothing
-- else, so no endpoint, tool or bug in one can list who was active when. The
-- only reader is cc_installation_trends() below, which counts.
--
-- A day, not a timestamp, and no client or request detail: enough to count
-- weekly users, and too coarse to reconstruct anyone's working hours.
CREATE TABLE principal_activity_days (
  day          date NOT NULL,
  -- CASCADE: this is a tally about a principal, not a record anyone must
  -- keep, and it must never be the reason a principal cannot be deleted.
  principal_id uuid NOT NULL REFERENCES principals (id) ON DELETE CASCADE,
  PRIMARY KEY (day, principal_id)
);

GRANT INSERT ON principal_activity_days TO cc_app;

-- ---------------------------------------------------------------------------
-- One row per week, oldest first, ending with the current week.
--
-- Weeks are ISO weeks in UTC, starting Monday. UTC because an installation
-- has no time zone of its own and the admin's would split the same request
-- into different weeks for different readers.
--
-- `weeks` is clamped to 1..52: a year is the most a screen shows, and an
-- unbounded series is an unbounded query.
--
-- Every week in the range is present, including empty ones. A missing row
-- reads as "no data"; a zero reads as "nothing happened", and only the
-- second is true.
CREATE FUNCTION cc_installation_trends(weeks int)
RETURNS TABLE (
  week_start             date,
  active_users           bigint,
  active_agents          bigint,
  projects_created       bigint,
  handoffs_created       bigint,
  handoffs_claimed       bigint,
  -- Claimed through a different client credential than the one that created
  -- it: the cross-client takeover the product exists for (NEXT.md, "Agent B,
  -- on a different client"). A credential is the closest thing to "a client"
  -- the server can see; two tokens on one laptop count as two clients.
  handoffs_claimed_by_other_client bigint,
  handoffs_closed        bigint,
  -- Median of created→claimed over the handoffs claimed that week. NULL when
  -- none were — not zero, which would read as "instant".
  median_seconds_to_claim double precision,
  events                 bigint
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  WITH series AS (
    SELECT s::date AS week_start,
           (s::timestamp AT TIME ZONE 'UTC') AS lo,
           (s::timestamp AT TIME ZONE 'UTC') + interval '7 days' AS hi
    FROM generate_series(
      date_trunc('week', now() AT TIME ZONE 'UTC')
        - (least(greatest(weeks, 1), 52) - 1) * interval '7 days',
      date_trunc('week', now() AT TIME ZONE 'UTC'),
      interval '7 days'
    ) AS s
  )
  SELECT
    w.week_start,
    (SELECT count(DISTINCT a.principal_id)
       FROM principal_activity_days a JOIN principals p ON p.id = a.principal_id
      WHERE p.kind = 'user' AND a.day >= w.week_start AND a.day < w.week_start + 7),
    (SELECT count(DISTINCT a.principal_id)
       FROM principal_activity_days a JOIN principals p ON p.id = a.principal_id
      WHERE p.kind = 'service' AND a.day >= w.week_start AND a.day < w.week_start + 7),
    (SELECT count(*) FROM projects WHERE created_at >= w.lo AND created_at < w.hi),
    (SELECT count(*) FROM handoffs WHERE created_at >= w.lo AND created_at < w.hi),
    (SELECT count(*) FROM handoffs WHERE claimed_at >= w.lo AND claimed_at < w.hi),
    (SELECT count(*) FROM handoffs
      WHERE claimed_at >= w.lo AND claimed_at < w.hi
        AND created_by_client IS NOT NULL AND claimed_by_client IS NOT NULL
        AND claimed_by_client <> created_by_client),
    (SELECT count(*) FROM handoffs WHERE closed_at >= w.lo AND closed_at < w.hi),
    (SELECT percentile_cont(0.5) WITHIN GROUP (
              ORDER BY extract(epoch FROM claimed_at - created_at))
       FROM handoffs WHERE claimed_at >= w.lo AND claimed_at < w.hi),
    (SELECT count(*) FROM domain_events WHERE created_at >= w.lo AND created_at < w.hi)
  FROM series w
  ORDER BY w.week_start;
$$;

-- ---------------------------------------------------------------------------
-- How the handoff loop performs, over the installation's whole life.
--
-- These are NEXT.md's "time to first handoff" and the shape of the loop
-- behind it, measured from what the server already records rather than
-- asked of anyone. They cannot measure "resume success" — whether the second
-- agent understood the handoff — because the server never sees that; a
-- figure claiming to would be invented.
CREATE FUNCTION cc_installation_handoff_flow()
RETURNS TABLE (
  handoffs                     bigint,
  claimed                      bigint,
  claimed_by_other_client      bigint,
  -- Open for more than seven days: work handed off that nobody picked up.
  stale_open                   bigint,
  median_seconds_to_claim      double precision,
  p90_seconds_to_claim         double precision,
  median_seconds_claim_to_close double precision,
  -- Projects in which some handoff has been taken over by another client,
  -- out of all projects — how many reached the loop at all.
  projects                     bigint,
  projects_with_takeover       bigint,
  -- Per project: project created → first takeover by another client; the
  -- median across projects that had one. The closest server-side reading of
  -- "quickstart to takeover by the second client".
  median_seconds_to_first_takeover double precision
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = public
AS $$
  WITH takeover AS (
    SELECT h.project_id, min(h.claimed_at) AS first_at
    FROM handoffs h
    WHERE h.claimed_at IS NOT NULL
      AND h.created_by_client IS NOT NULL AND h.claimed_by_client IS NOT NULL
      AND h.claimed_by_client <> h.created_by_client
    GROUP BY h.project_id
  )
  SELECT
    (SELECT count(*) FROM handoffs),
    (SELECT count(*) FROM handoffs WHERE claimed_at IS NOT NULL),
    (SELECT count(*) FROM handoffs
      WHERE claimed_at IS NOT NULL
        AND created_by_client IS NOT NULL AND claimed_by_client IS NOT NULL
        AND claimed_by_client <> created_by_client),
    (SELECT count(*) FROM handoffs
      WHERE status = 'open' AND created_at < now() - interval '7 days'),
    (SELECT percentile_cont(0.5) WITHIN GROUP (
              ORDER BY extract(epoch FROM claimed_at - created_at))
       FROM handoffs WHERE claimed_at IS NOT NULL),
    (SELECT percentile_cont(0.9) WITHIN GROUP (
              ORDER BY extract(epoch FROM claimed_at - created_at))
       FROM handoffs WHERE claimed_at IS NOT NULL),
    (SELECT percentile_cont(0.5) WITHIN GROUP (
              ORDER BY extract(epoch FROM closed_at - claimed_at))
       FROM handoffs WHERE claimed_at IS NOT NULL AND closed_at IS NOT NULL),
    (SELECT count(*) FROM projects),
    (SELECT count(*) FROM takeover),
    (SELECT percentile_cont(0.5) WITHIN GROUP (
              ORDER BY extract(epoch FROM t.first_at - p.created_at))
       FROM takeover t JOIN projects p ON p.id = t.project_id);
$$;

REVOKE ALL ON FUNCTION cc_installation_trends(int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_installation_trends(int) TO cc_app;
REVOKE ALL ON FUNCTION cc_installation_handoff_flow() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_installation_handoff_flow() TO cc_app;
