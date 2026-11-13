-- One-hour cache writes, and retention for the usage ledger —
-- docs/adr/0031-one-hour-cache-writes-and-ledger-retention.md.
--
-- Anthropic bills a five-minute cache write at 1.25x input and a one-hour
-- write at 2x. Until now a cache write was one count, so every one-hour
-- write was priced as a five-minute one. cache_write_1h_tokens is the part
-- of cache_write_tokens that was written for an hour: a subset, not a fifth
-- kind. The four counts keep adding up to every token reported, an old row
-- or an old client simply has no one-hour part, and the CHECK keeps the
-- subset from ever exceeding the whole.

ALTER TABLE plan_item_effort
  ADD COLUMN cache_write_1h_tokens bigint NOT NULL DEFAULT 0,
  ADD CONSTRAINT plan_item_effort_cache_write_1h_check
    CHECK (cache_write_1h_tokens >= 0 AND cache_write_1h_tokens <= cache_write_tokens);

ALTER TABLE plan_item_effort_models
  ADD COLUMN cache_write_1h_tokens bigint NOT NULL DEFAULT 0,
  ADD CONSTRAINT plan_item_effort_models_cache_write_1h_check
    CHECK (cache_write_1h_tokens >= 0 AND cache_write_1h_tokens <= cache_write_tokens);

ALTER TABLE usage_ledger
  ADD COLUMN cache_write_1h_tokens bigint NOT NULL DEFAULT 0,
  ADD CONSTRAINT usage_ledger_cache_write_1h_check
    CHECK (cache_write_1h_tokens >= 0 AND cache_write_1h_tokens <= cache_write_tokens);

-- Retention. cc_app still has no DELETE on usage_ledger: a counted cost is
-- a fact, and no request may remove one. The server's retention job
-- (ccd serve --usage-ledger-retention) runs with no principal, so RLS would
-- show it nothing; this function, owned by the table owner, deletes exactly
-- the rows older than the retention and nothing else. A retention below one
-- day is refused rather than read as "delete everything".
CREATE FUNCTION cc_purge_usage_ledger(p_keep_days int)
RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
  n bigint;
BEGIN
  IF p_keep_days IS NULL OR p_keep_days < 1 THEN
    RAISE EXCEPTION 'usage ledger retention must be at least one day, not %', p_keep_days;
  END IF;
  DELETE FROM usage_ledger
  WHERE day < (now() AT TIME ZONE 'UTC')::date - p_keep_days;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END;
$$;

REVOKE ALL ON FUNCTION cc_purge_usage_ledger(int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cc_purge_usage_ledger(int) TO cc_app;
