-- Usage ledger — docs/adr/0027-usage-ledger-by-branch-and-ticket.md.
--
-- plan_item_effort answers "what did this step cost". The team also asks
-- "what did this week, this person, this branch or this ticket cost", and
-- some of that work happens on no step at all. The ledger keeps one
-- aggregated row per (project, UTC day, principal, model, branch, ticket):
-- counts are added, never logged, so it grows with distinct days and
-- dimensions, not with reports.
--
-- branch and ticket_key are OPERATIONAL METADATA (docs/adr/0011), allowlisted
-- in ADR 0027: grouping by them is the point, and the database cannot group
-- what it cannot read. '' means "not stated" and keeps the unique key simple.
-- Seconds sit on the model = '' row, because time is not spent per model.

ALTER TABLE work_presence
  ADD COLUMN branch text NOT NULL DEFAULT '' CHECK (length(branch) <= 200),
  ADD COLUMN ticket_key text NOT NULL DEFAULT '' CHECK (length(ticket_key) <= 100);

CREATE TABLE usage_ledger (
  project_id         uuid NOT NULL REFERENCES projects (id),
  day                date NOT NULL,
  principal_id       uuid NOT NULL REFERENCES principals (id),
  model              text NOT NULL DEFAULT '',
  branch             text NOT NULL DEFAULT '' CHECK (length(branch) <= 200),
  ticket_key         text NOT NULL DEFAULT '' CHECK (length(ticket_key) <= 100),
  seconds            bigint NOT NULL DEFAULT 0 CHECK (seconds >= 0),
  tokens_in          bigint NOT NULL DEFAULT 0 CHECK (tokens_in >= 0),
  tokens_out         bigint NOT NULL DEFAULT 0 CHECK (tokens_out >= 0),
  cache_read_tokens  bigint NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
  cache_write_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
  -- Reports that carried tokens: roughly one per model turn.
  turns              bigint NOT NULL DEFAULT 0 CHECK (turns >= 0),
  PRIMARY KEY (project_id, day, principal_id, model, branch, ticket_key)
);
CREATE INDEX usage_ledger_day_idx ON usage_ledger (project_id, day DESC);

ALTER TABLE usage_ledger ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON usage_ledger
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
-- No DELETE: a counted cost is a fact. Purging belongs to retention, later.
GRANT SELECT, INSERT, UPDATE ON usage_ledger TO cc_app;
