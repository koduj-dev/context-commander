-- Who registered an agent.
--
-- `service_principals` has recorded a name, a description and a timestamp
-- since migrations/000001 and nothing about where it came from. Raised by the
-- maintainer on 2026-10-06: an agent "has to be created with some
-- authorization from a user", and that user was nowhere on the screen — so a
-- directory of agents was a list of names with no way to ask whose they are.
--
-- The value was never missing, only discarded: installation.Service's
-- CapServicePrincipalCreate check already resolves the calling user, and the
-- insert simply did not keep it.
--
-- NULLABLE, and left NULL for every existing row. Backfilling it with the
-- first admin, or with whoever runs this migration, would turn "nobody
-- recorded this" into a false statement about a specific person — and the
-- screen reading it cannot tell the difference. An agent registered before
-- this column existed is honestly unattributed.
--
-- Deliberately NOT named `owner_id`, and deliberately granting nothing. An
-- agent is its own principal (docs/adr/0004): its access comes from its own
-- project memberships, not from a user's. A column that looked like
-- ownership would invite the next reader to treat it as delegated authority,
-- which would quietly make every agent as powerful as whoever created it.
-- Ownership that can be transferred and that means something is a separate
-- decision, and it needs an ADR before it needs a column.
--
-- ON DELETE SET NULL rather than CASCADE: deleting a user must never delete
-- the agents they registered — those are running, hold credentials and have
-- project memberships of their own. Losing the attribution is the correct
-- loss here.
ALTER TABLE service_principals
  ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;

-- "Which agents did this person register" — asked when someone leaves, which
-- is exactly when it matters and exactly when nobody wants to scan.
CREATE INDEX service_principals_created_by_idx ON service_principals (created_by);
