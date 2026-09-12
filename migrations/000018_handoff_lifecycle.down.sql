DROP INDEX handoffs_project_status_created_idx;

ALTER TABLE handoffs DROP CONSTRAINT handoffs_status_timestamps;
ALTER TABLE handoffs DROP CONSTRAINT handoffs_closed_consistency;
ALTER TABLE handoffs DROP CONSTRAINT handoffs_claimed_consistency;

ALTER TABLE handoffs DROP COLUMN closed_by_client;
ALTER TABLE handoffs DROP COLUMN closed_by_principal;
ALTER TABLE handoffs DROP COLUMN closed_at;
ALTER TABLE handoffs DROP COLUMN claimed_by_client;
ALTER TABLE handoffs DROP COLUMN claimed_by_principal;
ALTER TABLE handoffs DROP COLUMN claimed_at;

-- 'claimed' rows have no representation in the pre-000018 vocabulary. They
-- are folded back to 'open' — the state they would have been in had this
-- migration never run — rather than dropped: losing the handoff itself to
-- satisfy a CHECK constraint would be a far worse trade.
UPDATE handoffs SET status = 'open' WHERE status = 'claimed';

ALTER TABLE handoffs DROP CONSTRAINT handoffs_status_check;
ALTER TABLE handoffs ADD CONSTRAINT handoffs_status_check
  CHECK (status IN ('open', 'closed'));
