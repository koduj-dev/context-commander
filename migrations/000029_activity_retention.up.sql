-- Who was active on which day is kept for fourteen days, and no longer.
--
-- Asked by the maintainer on 2026-10-27. principal_activity_days
-- (migrations/000028) is the one per-person usage record this installation
-- keeps, and nothing pruned it: a year in, it would be a year of who worked
-- on which days, readable to anyone with the database even though the
-- application cannot read it.
--
-- Pruning it naively would make the Usage screen lie. It shows twelve weeks
-- of "people active", and a week whose day rows are gone would read as a
-- week nobody worked. So a finished week is first reduced to two numbers —
-- people, agents — in activity_weeks, which names nobody, and only then may
-- its days be deleted. Fourteen days leaves a full week of slack after a week
-- ends before any of its days can go.
--
-- There is no scheduler in this server, so the work rides on the write:
-- cc_record_activity() rolls up and prunes whenever it records a principal's
-- first request of the day. That is at most once per principal per day, and
-- on an installation nobody uses, nothing accumulates to prune. Roll-up
-- always precedes the delete in the same call, so a day is never removed
-- before its week is counted.

CREATE TABLE activity_weeks (
  week_start    date PRIMARY KEY,
  active_users  bigint NOT NULL,
  active_agents bigint NOT NULL
);
-- No grant to cc_app: written only by cc_roll_up_activity(), read only by
-- cc_installation_trends().

CREATE FUNCTION cc_roll_up_activity()
RETURNS void
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
  -- Every finished week that still has day rows and is not counted yet.
  -- A week with no rows gets no row here; the trends read that as zero,
  -- which it is.
  INSERT INTO activity_weeks (week_start, active_users, active_agents)
  SELECT date_trunc('week', a.day)::date,
         count(DISTINCT a.principal_id) FILTER (WHERE p.kind = 'user'),
         count(DISTINCT a.principal_id) FILTER (WHERE p.kind = 'service')
  FROM principal_activity_days a JOIN principals p ON p.id = a.principal_id
  WHERE a.day < date_trunc('week', now() AT TIME ZONE 'UTC')::date
  GROUP BY 1
  ON CONFLICT (week_start) DO NOTHING;

  DELETE FROM principal_activity_days
  WHERE day < (now() AT TIME ZONE 'UTC')::date - 14;
$$;

-- The only way the application records activity. It replaces cc_app's
-- direct INSERT, so the roll-up and the retention cannot be skipped by a
-- caller that forgets them.
CREATE FUNCTION cc_record_activity(principal uuid)
RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
  INSERT INTO principal_activity_days (day, principal_id)
  VALUES ((now() AT TIME ZONE 'UTC')::date, principal)
  ON CONFLICT DO NOTHING;
  -- Already recorded today: the roll-up ran then, nothing new to do.
  IF FOUND THEN
    PERFORM cc_roll_up_activity();
  END IF;
END;
$$;

REVOKE INSERT ON principal_activity_days FROM cc_app;
REVOKE ALL ON FUNCTION cc_roll_up_activity() FROM PUBLIC;
REVOKE ALL ON FUNCTION cc_record_activity(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_record_activity(uuid) TO cc_app;

-- A week counts from activity_weeks once it has been rolled up, and from its
-- day rows until then — the current week always, and a finished week nobody
-- has been active since (the roll-up rides on activity, so it has not run).
-- Every finished week is rolled up before any of its days are deleted, so
-- one of the two always holds the whole week. Otherwise unchanged from
-- 000028.
CREATE OR REPLACE FUNCTION cc_installation_trends(weeks int)
RETURNS TABLE (
  week_start             date,
  active_users           bigint,
  active_agents          bigint,
  projects_created       bigint,
  handoffs_created       bigint,
  handoffs_claimed       bigint,
  handoffs_claimed_by_other_client bigint,
  handoffs_closed        bigint,
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
    COALESCE(
      (SELECT aw.active_users FROM activity_weeks aw WHERE aw.week_start = w.week_start),
      (SELECT count(DISTINCT a.principal_id)
         FROM principal_activity_days a JOIN principals p ON p.id = a.principal_id
        WHERE p.kind = 'user' AND a.day >= w.week_start AND a.day < w.week_start + 7)),
    COALESCE(
      (SELECT aw.active_agents FROM activity_weeks aw WHERE aw.week_start = w.week_start),
      (SELECT count(DISTINCT a.principal_id)
         FROM principal_activity_days a JOIN principals p ON p.id = a.principal_id
        WHERE p.kind = 'service' AND a.day >= w.week_start AND a.day < w.week_start + 7)),
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

-- Whatever 000028 recorded so far is rolled up and pruned now, not at the
-- first request after deploy.
SELECT cc_roll_up_activity();
