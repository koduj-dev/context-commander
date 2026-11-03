-- Tokens per model beside the totals — docs/adr/0022, "Time and tokens".
--
-- The maintainer asked for tokens to judge whether the API would cost less
-- than a subscription, and prices differ by model, so a count that does not
-- say which model it was for cannot be priced. plan_item_effort keeps the
-- totals and the time; this keeps the same four counts split by the model the
-- client named. model '' is "not stated": every token in the totals is in
-- exactly one row here, so the breakdown always adds up to the totals.
--
-- The model is a label the client reports, not a foreign key: an unknown
-- model is allowed and is simply shown without a price.
CREATE TABLE plan_item_effort_models (
  item_id            uuid NOT NULL REFERENCES plan_items (id) ON DELETE CASCADE,
  principal_id       uuid NOT NULL REFERENCES principals (id),
  model              text NOT NULL CHECK (length(model) <= 128),
  project_id         uuid NOT NULL REFERENCES projects (id),
  tokens_in          bigint NOT NULL DEFAULT 0 CHECK (tokens_in >= 0),
  tokens_out         bigint NOT NULL DEFAULT 0 CHECK (tokens_out >= 0),
  cache_read_tokens  bigint NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
  cache_write_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
  PRIMARY KEY (item_id, principal_id, model)
);
CREATE INDEX plan_item_effort_models_project_idx ON plan_item_effort_models (project_id);

-- What was reported before models were: not stated.
INSERT INTO plan_item_effort_models (item_id, principal_id, model, project_id, tokens_in, tokens_out, cache_read_tokens, cache_write_tokens)
SELECT item_id, principal_id, '', project_id, tokens_in, tokens_out, cache_read_tokens, cache_write_tokens
FROM plan_item_effort
WHERE tokens_in > 0 OR tokens_out > 0 OR cache_read_tokens > 0 OR cache_write_tokens > 0;

ALTER TABLE plan_item_effort_models ENABLE ROW LEVEL SECURITY;
CREATE POLICY project_scoped ON plan_item_effort_models
  USING (cc_has_project_access(current_setting('app.current_principal_id', true), project_id));
GRANT SELECT, INSERT, UPDATE ON plan_item_effort_models TO cc_app;
