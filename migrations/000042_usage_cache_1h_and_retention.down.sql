DROP FUNCTION IF EXISTS cc_purge_usage_ledger(int);
ALTER TABLE usage_ledger DROP COLUMN IF EXISTS cache_write_1h_tokens;
ALTER TABLE plan_item_effort_models DROP COLUMN IF EXISTS cache_write_1h_tokens;
ALTER TABLE plan_item_effort DROP COLUMN IF EXISTS cache_write_1h_tokens;
