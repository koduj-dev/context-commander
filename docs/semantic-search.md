# Semantic search of memories

Memories are searched by their words out of the box. Semantic search also
finds memories that are **similar in meaning**: in another language, or with
none of the words you typed. A search for "pomalá databáze" finds "the list
query does a sequential scan". It also adds a **Related memories** panel to
each memory in the interface, and the MCP tool `memory.related`.

It is **off until an operator configures an embedding provider.** With
nothing configured, everything works exactly as before. The design is in
[ADR 0028](adr/0028-semantic-search.md).

## Quick start with a local model (recommended)

Ollama runs the model on your own machine, so memory text never leaves it.
The default model, `bge-m3`, is multilingual and handles Czech and English
well. It needs about 1.2 GB of disk and runs on a CPU.

Development compose file:

```bash
docker compose -f deploy/docker-compose.yml --profile embeddings up -d
docker compose -f deploy/docker-compose.yml exec ollama ollama pull bge-m3

ccd serve --embeddings-provider=ollama --embeddings-url=http://localhost:11434
```

Quickstart compose file: uncomment the `ollama` service and the
`cc_ollama_models` volume in `deploy/quickstart/docker-compose.yml`, add the
two `CC_EMBEDDINGS_*` lines from the comment there to `app`'s environment,
then:

```bash
docker compose up -d
docker compose exec ollama ollama pull bge-m3
```

Memories written before semantic search was on are embedded by the server
in the background, within about ten minutes of startup. To do it now:

```bash
ccd embeddings reindex --embeddings-provider=ollama --embeddings-url=http://localhost:11434
```

`reindex` connects like the server: `CC_DATABASE_URL` as `cc_app`, and the
same `CC_ROOT_KEY`. Use the same `--embeddings-*` settings as the server.
`--all` re-embeds every memory, not only the missing and stale ones.

## Configuration

Every flag of `ccd serve` (and of `ccd embeddings reindex`) has an
environment variable, so a `.env` file next to the compose file configures
it:

```dotenv
# Turn it on. Empty or absent: text search only.
CC_EMBEDDINGS_PROVIDER=ollama            # ollama | openai
CC_EMBEDDINGS_URL=http://ollama:11434    # default http://localhost:11434 (ollama), https://api.openai.com (openai)
CC_EMBEDDINGS_MODEL=bge-m3               # default bge-m3 (ollama), text-embedding-3-small (openai)

# Optional
CC_EMBEDDINGS_STORE=encrypted            # encrypted (default) | pgvector — see below
CC_EMBEDDINGS_CACHE_MB=64                # decrypted-vector cache, encrypted store only
CC_EMBEDDINGS_MIN_SIMILARITY=0.45        # below this a memory is not a semantic hit; model-dependent
CC_EMBEDDINGS_TIMEOUT=30s                # one request to the provider
CC_EMBEDDINGS_DIMENSIONS=0               # 0 accepts the model's length; openai text-embedding-3 can shorten
CC_EMBEDDINGS_API_KEY_FILE=/run/secrets/embeddings_key   # openai only
```

| Flag | Variable | Default |
|---|---|---|
| `--embeddings-provider` | `CC_EMBEDDINGS_PROVIDER` | empty: off |
| `--embeddings-url` | `CC_EMBEDDINGS_URL` | per provider |
| `--embeddings-model` | `CC_EMBEDDINGS_MODEL` | `bge-m3` / `text-embedding-3-small` |
| `--embeddings-api-key-file` | `CC_EMBEDDINGS_API_KEY_FILE` | none |
| `--embeddings-dimensions` | `CC_EMBEDDINGS_DIMENSIONS` | 0 (whatever the model returns) |
| `--embeddings-timeout` | `CC_EMBEDDINGS_TIMEOUT` | 30s |
| `--embeddings-store` | `CC_EMBEDDINGS_STORE` | `encrypted` |
| `--embeddings-cache-mb` | `CC_EMBEDDINGS_CACHE_MB` | 64 |
| `--embeddings-min-similarity` | `CC_EMBEDDINGS_MIN_SIMILARITY` | 0.45 |

A wrong value stops the server from starting. A provider that does not
answer does not stop it: the server logs a warning, hybrid search falls back
to words (and says so), and the background sweep catches up when the
provider comes back.

**Changing the model** makes every stored vector stale. The server
re-embeds in the background, or you can run `ccd embeddings reindex`.
Different models score similarity differently, so you may need to adjust
`--embeddings-min-similarity`.

## An external API: who receives your text

`--embeddings-provider=openai` works with any OpenAI-compatible
`/v1/embeddings` endpoint: OpenAI, Azure OpenAI behind a compatible proxy,
or a self-hosted server such as vLLM or LiteLLM.

**The title and body of every memory, and every search query, are sent to
that endpoint in plaintext.** Whoever runs it receives them, under their own
retention and training terms. That is a decision for whoever is responsible
for the data in this installation. The server logs a warning naming the
endpoint at every start. The API key is read from a file. There is
deliberately no flag for the key itself, because an argument is visible in
`ps`.

```bash
ccd serve --embeddings-provider=openai --embeddings-api-key-file=/run/secrets/openai_key
```

## Where vectors are stored

A vector says what a memory means closely enough that similar memories can
be found from it, and a guessed text can be checked against it. Context
Commander treats vectors as content.

- **`encrypted` (default).** Each vector is sealed with the project's key,
  like the memory's text. Similarity is computed in the server over vectors
  decrypted into an in-memory cache that never holds more than
  `--embeddings-cache-mb` (64 MiB by default, about 16,000 vectors of 1024
  dimensions). Nothing to install.
- **`pgvector` (opt-in).** Vectors are stored **in plaintext** in a pgvector
  column, and similarity is computed in the database. Anyone who can read
  the database (an operator, a backup, a stolen dump) can compare your
  memories by meaning, although not read them. Choose it only if that is
  acceptable. It needs:
  1. a Postgres with the extension, for example the `pgvector/pgvector:pg16`
     image instead of `postgres:16-alpine`;
  2. a superuser to run `CREATE EXTENSION vector;` in the database once.
     `deploy/postgres/init/02-pgvector.sql` does it when a new cluster is
     created from that image. For an existing cluster, run it by hand;
  3. the optional migrations, as `cc_owner`:
     `ccd migrate --pgvector`. They are separate from the core migrations
     and tracked in their own table, `schema_migrations_pgvector`;
  4. `--embeddings-store=pgvector` on the server. It refuses to start until
     step 3 is done.

## Checking that it works

An installation administrator can read `GET /v1/installation/embeddings`:

```json
{
  "configured": true, "provider": "ollama", "model": "bge-m3",
  "endpoint": "http://ollama:11434", "store": "encrypted", "dimensions": 1024,
  "reachable": true, "last_success": "2026-11-13T10:00:00Z",
  "queue": 0, "queue_capacity": 1000, "dropped": 0, "pending": 0,
  "cache_bytes": 1310720, "cache_limit_bytes": 67108864
}
```

`pending` is how many memories are not embedded yet under the current model.
`last_error` says why the provider last failed. It never contains memory
text.

In the interface, the project's **Memories** tab shows a "Search by: Words /
Similar meaning / Both" switch, and opening a memory shows **Related
memories**. Both appear only when the server has semantic search.

Over MCP, `memory.search` takes `mode` (`text`, `semantic`, `hybrid`), and
`memory.related` takes a `memory_id`.
