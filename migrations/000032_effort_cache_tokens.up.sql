-- Cache tokens beside input and output — docs/adr/0022, "Time and tokens".
--
-- For an agent like Claude Code nearly all input is read from the prompt
-- cache, which the API bills at a fraction of fresh input, while writing to
-- the cache costs more than fresh input. A count of "tokens in" that lumps
-- the three together cannot answer what the maintainer asked tokens for —
-- whether the API would cost more or less than a subscription — so they are
-- kept apart. Measured in a real Claude Code session on 2026-10-27: ~1k fresh
-- input tokens against ~127M read from cache.
ALTER TABLE plan_item_effort ADD COLUMN cache_read_tokens  bigint NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0);
ALTER TABLE plan_item_effort ADD COLUMN cache_write_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0);
