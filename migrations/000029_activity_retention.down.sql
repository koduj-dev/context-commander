-- Restores 000028's shape. Pruned days are gone and stay gone; the trends
-- go back to counting from whatever day rows remain.
GRANT INSERT ON principal_activity_days TO cc_app;
DROP FUNCTION IF EXISTS cc_record_activity(uuid);
DROP FUNCTION IF EXISTS cc_roll_up_activity();

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

DROP TABLE IF EXISTS activity_weeks;
