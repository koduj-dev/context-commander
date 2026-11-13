# 0028. Semantic search of memories: pluggable embeddings, encrypted vectors by default

## Status

Accepted, 2026-11-13.

Refines [ADR 0025](0025-memories.md), which kept embedding search out and
left `memories.Ranker` as the seam for it. Records one opt-in exception to
[ADR 0011](0011-encryption-classification.md) (the `pgvector` store).

## Context

Memories (ADR 0025) are searched by substring. A team of three writing in
Czech and English finds that a search only works when they remember the
words a colleague used: "pomalá databáze" does not find "the list query does
a sequential scan", and neither does "slow list". NEXT.md said to add vector
search only when measurement showed text search to be insufficient. On
2026-11-13 the maintainer decided to add it now, with these constraints:

1. Embeddings are pluggable and local by default (Ollama). An
   OpenAI-compatible API is opt-in. With nothing configured, semantic search
   is off and text search works exactly as before.
2. Where vectors are stored is configuration: `encrypted` (default) or
   `pgvector`.
3. Sending memory text to an external API is a privacy decision, and the
   server says so when it starts.

## Decision

### Providers

`internal/embeddings.Provider` has `Name`, `Model`, `Dimensions`, `Endpoint`
and `Embed(ctx, []string) ([][]float32, error)`.

- **Ollama** (`--embeddings-provider=ollama`): `POST {url}/api/embed`. The
  default model is `bge-m3`: multilingual, good at Czech, runs on a CPU.
- **OpenAI-compatible** (`--embeddings-provider=openai`):
  `POST {url}/v1/embeddings` with a bearer key read from
  `--embeddings-api-key-file`. There is no flag for the key itself, because
  an argument is visible in `ps`. The server logs a warning at startup that
  names the endpoint, because every memory's title and body is sent there.
- Both use a timeout (30 s by default), send at most 16 texts per request,
  cut each text to 8000 characters, and read a response through a 16 MiB
  limit. A response with the wrong number of vectors, ragged or absurd
  dimensions, non-finite values, a bad index (OpenAI) or invalid JSON is
  refused. Errors carry the status code and never the response body, which
  may quote the input. The key is never logged. A provider URL with
  credentials in it is refused, because the endpoint is logged and shown to
  administrators.

### What a vector is, and the two stores

A vector is derived from the text and encodes what it means closely enough
that similar texts can be found from it, and a guessed text can be checked
against it. Under ADR 0011 that makes it **CONTENT PAYLOAD**.

- **`encrypted` (default).** Table `memory_embeddings` (core migration
  000040). One row per memory and model: the vector sealed with the project
  key through `CryptoProvider` (`ResourceType "memory_embedding"`, AAD
  version = the memory version it was made from). The plaintext columns are
  operational metadata: memory id, model, project, memory version,
  dimensions. Similarity is computed in Go over vectors decrypted into an
  in-memory LRU cache that is **bounded by bytes**: 64 MiB by default
  (`--embeddings-cache-mb`), about 16,000 vectors of 1024 dimensions. The
  cache is keyed by memory id. It is only consulted for ids the caller's
  own RLS read returned, and only at the project and version that read
  returned, so it never answers for a memory the caller cannot see. A write
  drops the entry, and the next read reloads it.
- **`pgvector` (opt-in).** Table `memory_vectors`, with a plaintext
  `vector` column, and similarity computed in SQL with `<=>`. **This is an
  exception to ADR 0011.** Anyone who can read the database (an operator, a
  backup, a stolen dump) can compare memories by meaning, cluster them, and
  test guesses against them, although they still cannot read the text. An
  installation chooses it to avoid decrypting vectors into the server's
  memory. The server logs a warning when it starts with it.

Both tables are project-scoped: RLS with the identical
`cc_has_project_access(...)` policy, `SELECT, INSERT, UPDATE, DELETE` for
`cc_app` (a stale vector is replaced, and a vector for a model no longer
configured is deleted), `ON DELETE CASCADE` from `memories`, and a negative
cross-project test as `cc_app`. Vectors are keyed by **memory id and
model**, so changing `--embeddings-model` makes every vector stale and
triggers re-embedding. Storing a new model's vector deletes the old model's,
so a model change does not leave a second copy behind. Vectors do not count
against the storage quota: they are derived data, at most one per memory,
and the person who would hit the limit cannot control them.

### The pgvector schema lives outside the core migration sequence

The core migrations must keep applying on plain `postgres:16-alpine`, which
has no `vector` type. So the pgvector schema is a **separate embedded
migration set**, `migrations/pgvector/`, tracked in its own version table
`schema_migrations_pgvector` (golang-migrate's `x-migrations-table`). It is
applied by `ccd migrate --pgvector` after the core set. The two sequences
number independently, and the core one never learns that the optional one
exists.

`vector` is not a trusted extension, so `cc_owner` cannot create it. A
superuser runs `CREATE EXTENSION vector` once.
`deploy/postgres/init/02-pgvector.sql` does that at cluster creation when
the image has the extension (`pgvector/pgvector:pg16`), and does nothing on
the default image. `ccd migrate --pgvector` checks for the extension first
and says what to do when it is missing. `ccd serve
--embeddings-store=pgvector` reads the version table (migration 1 grants
`cc_app` `SELECT` on it) and refuses to start unless the set is applied,
clean and current. The message names `ccd migrate --pgvector`.

**No approximate index.** HNSW and IVFFlat need a fixed dimension per
column. The model is configuration, so the column has no dimension.
Similarity is computed exactly, over at most one search's candidates (see
below), which is a few thousand floats per memory. When a later change
lifts the candidate bound, it will need an index and a per-model column or
expression to put it on.

### Indexing never blocks a write

`memories.Service` calls `Semantic.Enqueue` after a create or a text edit
commits. `embeddings.Index` keeps a bounded queue (1000) of jobs,
de-duplicated by memory id. `Enqueue` never blocks: a full queue drops the
job and counts it. One worker drains the queue in batches of up to 16, and
retries a failing batch with exponential backoff (1 s, doubling to 2 min,
5 attempts). After that it leaves the batch to the sweep.

The **sweep** runs at startup and every 10 minutes. It lists memories that
have no vector under the current model, or one made from an older version,
and queues them. Restarts, dropped jobs, provider outages and model changes
all heal through it. `ccd embeddings reindex [--all]` does the same work
synchronously, for an operator backfilling after turning semantic search on.

**Whom the indexer acts as.** It is not a request, but it reads and writes
under RLS like one, through `memories.Service.Get`, so authorization and
decryption go through the usual path. After a write it acts as the writer.
For the sweep, the `SECURITY DEFINER` function
`cc_memory_embeddings_pending` (or `cc_memory_vectors_pending`) returns ids
only, never text or vectors. For each memory it also returns a principal
who is currently a member of the project: the author while they are still a
member, otherwise its longest-standing owner or member. That principal can
already read the memory, so the function grants no access. A project with
no members left is skipped.

### Search modes

`memory.search` (MCP), `GET .../memories/search` and
`GET /v1/me/memories/search` take `mode`:

- `text`: the substring match, unchanged.
- `semantic`: candidates ranked by cosine similarity of their vectors to
  the query's vector. Memories below `--embeddings-min-similarity` (default
  0.45, which depends on the model) are left out, and so are memories not
  embedded yet.
- `hybrid`: **reciprocal rank fusion** (k = 60) of the text ranking and the
  semantic ranking. Ranks, not scores, are fused, because a substring match
  has no score. A memory found by text keeps the text hit's match and
  snippet.

The default is `hybrid` when a provider is configured and `text` otherwise.
`semantic` or `hybrid` without a provider is `ErrSemanticOff`, which wraps
`ErrInvalid`: a 400 over HTTP and a tool error over MCP, saying that
semantic search is not configured. If the provider fails during a `hybrid`
search, the search answers by text and sets `degraded`. Hybrid is the
default, and a provider outage must not take search down with it. A
`semantic` search, asked for by name, fails instead.

The candidates are the same as for text search. The metadata filters run in
SQL, the authorization check and the RLS read come first, and the same
bounds apply: 1000 per project, 50 projects × 200 across projects, 100 hits.
Vectors are read per project, under the caller's RLS context, for the
caller's candidates only.

**Related memories.** `GET .../memories/{id}/related?limit=` and the MCP
tool `memory.related` return the active memories closest in meaning to one
memory, at most 20 (5 by default). They use the memory's stored vector, or
embed its text on the spot when it has no current one. Readers may call
them, as they may search.

`Ranker.Rank` now also receives the caller's principal id, because a
ranker that reads stored vectors needs the caller's RLS context. `Hit`
gains `Score`. `SearchResult` gains `Mode` and `Degraded`.

### Surfaces

- `GET .../memories/facets` returns `search_modes` and
  `default_search_mode`. The interface shows its mode switch ("Words /
  Similar meaning / Both") and the "Related memories" panel in the detail
  view only when `search_modes` includes `semantic`.
- `GET /v1/installation/embeddings` (installation administrators only)
  reports whether semantic search is configured, the provider, the model,
  the endpoint, the store, the dimensions, whether the provider was
  reachable at the last attempt, the last success, the last error, the
  queue length and capacity, dropped jobs, pending memories and the cache's
  use. It contains no memory text, no vector and no key.
- Configuration: `--embeddings-provider`, `--embeddings-url`,
  `--embeddings-model`, `--embeddings-api-key-file`,
  `--embeddings-dimensions`, `--embeddings-timeout`, `--embeddings-store`,
  `--embeddings-cache-mb`, `--embeddings-min-similarity`. Each has a
  `CC_EMBEDDINGS_*` environment variable, documented in
  [docs/semantic-search.md](../semantic-search.md). A wrong value stops the
  server from starting.
- `deploy/docker-compose.yml` has an `ollama` service behind
  `--profile embeddings`. The quickstart compose file has it as a commented
  block.

## Consequences

- People find memories by meaning, across Czech and English, and see what
  else was recorded about the same thing. With no provider configured,
  nothing changes.
- The default keeps ADR 0011 intact: a stolen database holds sealed
  vectors. The price is server memory (bounded) and a decryption per vector
  the cache does not hold.
- `pgvector` is available to installations that accept plaintext vectors in
  exchange for computing similarity in the database. It is opt-in in
  configuration, in the migration command and in the Postgres image, and it
  warns every time the server starts.
- With the OpenAI-compatible provider, memory text leaves the installation.
  It is opt-in and stated at startup. The docs say who receives the text.
- Semantic results depend on the model and on `--embeddings-min-similarity`.
  Neither is learned, so an installation that changes models may need to
  tune the threshold.
- A memory is searchable by meaning only after it is embedded, usually
  within seconds. Until then text and hybrid search still find it by words.
