-- Handoff lifecycle — NEXT.md Phase 1: "Define open → claimed/acknowledged →
-- closed, plus a 'current handoff' query."
--
-- Before this, migrations/000010 allowed only ('open', 'closed') and
-- internal/handoffs.Service had no transition at all: every row was created
-- 'open' and stayed there forever, which is why the web UI's Handoffs tab
-- could only show and create. A handoff that cannot be claimed cannot
-- express "someone is already continuing this work" — the one fact the
-- second agent in a cross-client handoff most needs before it starts.
--
-- 'claimed' sits between the two rather than replacing 'closed': claiming
-- is taking responsibility, closing is declaring the work done, and the
-- pair are distinct events an audit trail must be able to tell apart.
ALTER TABLE handoffs DROP CONSTRAINT handoffs_status_check;
ALTER TABLE handoffs ADD CONSTRAINT handoffs_status_check
  CHECK (status IN ('open', 'claimed', 'closed'));

-- Who took it and when. claimed_by_client is the client credential, not
-- just the principal: the whole point of the handoff domain is that agent A
-- and agent B are different *clients*, frequently sharing neither principal
-- nor vendor, so "which client claimed this" is the load-bearing half.
ALTER TABLE handoffs ADD COLUMN claimed_at            timestamptz;
ALTER TABLE handoffs ADD COLUMN claimed_by_principal  uuid REFERENCES principals (id);
ALTER TABLE handoffs ADD COLUMN claimed_by_client     uuid REFERENCES client_credentials (id);
ALTER TABLE handoffs ADD COLUMN closed_at             timestamptz;
ALTER TABLE handoffs ADD COLUMN closed_by_principal   uuid REFERENCES principals (id);
ALTER TABLE handoffs ADD COLUMN closed_by_client      uuid REFERENCES client_credentials (id);

-- The timestamp columns are derived state, not independent input: a row may
-- only carry claimed_*/closed_* consistently with the status it is in.
-- Service.Claim/Close write both together in one UPDATE, so a row that
-- violates this means a code path bypassed the service — exactly what a
-- CHECK is for.
ALTER TABLE handoffs ADD CONSTRAINT handoffs_claimed_consistency
  CHECK ((claimed_at IS NULL) = (claimed_by_principal IS NULL));
ALTER TABLE handoffs ADD CONSTRAINT handoffs_closed_consistency
  CHECK ((closed_at IS NULL) = (closed_by_principal IS NULL));
ALTER TABLE handoffs ADD CONSTRAINT handoffs_status_timestamps
  CHECK (
    (status = 'open'    AND claimed_at IS NULL AND closed_at IS NULL)
    OR (status = 'claimed' AND claimed_at IS NOT NULL AND closed_at IS NULL)
    OR (status = 'closed'  AND closed_at IS NOT NULL)
  );

-- Service.Current asks for "the newest handoff still in play" — status IN
-- ('open','claimed') ordered by created_at DESC. The existing
-- handoffs_project_id_idx can't serve that ordering without a sort of every
-- handoff the project ever had; this one can.
CREATE INDEX handoffs_project_status_created_idx
  ON handoffs (project_id, status, created_at DESC, id DESC);
