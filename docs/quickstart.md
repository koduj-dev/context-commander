# Quickstart

Two agents on two different clients, sharing project state through a server
you run yourself. Target: under ten minutes from nothing to the second
client picking up the first one's work.

You need Docker and Docker Compose. Nothing else — no Go toolchain, no Node,
no manual SQL.

## 1. Start the server

```bash
git clone https://github.com/koduj-dev/context-commander.git
cd context-commander/deploy/quickstart

cp .env.example .env
printf 'CC_ROOT_KEY=%s\n' "$(openssl rand -base64 32)" > .env

docker compose up -d --build
```

That builds the image, starts Postgres, applies every migration as
`cc_owner`, and then starts the server as `cc_app`. The REST API is at
<http://127.0.0.1:8490/v1>, the MCP endpoint at
<http://127.0.0.1:8490/mcp>, and the web UI at <http://127.0.0.1:8490>.

> **`CC_ROOT_KEY` is not recoverable.** It encrypts every project key, and
> through them every artifact, handoff, secret and history object. Back it up
> before you put anything into the server — losing it loses the data.

Check it came up:

```bash
curl -fsS http://127.0.0.1:8490/healthz && echo " ok"
```

## 2. Create the first user and token

```bash
docker compose run --rm app bootstrap \
  --email you@example.com --name "Your Name"
```

This prints a Personal Access Token exactly once — only its hash is stored.
Keep it:

```bash
export CC_TOKEN="cc_..."      # paste the token from the command above
```

The bootstrap user is also the first installation admin, so there is no
second escalation step and no manual SQL (docs/adr/0017).

## 3. Create a project

```bash
curl -fsS -X POST http://127.0.0.1:8490/v1/projects \
  -H "Authorization: Bearer $CC_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"project": "you/first-project", "name": "First Project"}'
```

The path is `namespace/slug` (docs/adr/0001): `you` is your personal
namespace, `first-project` the project in it.

## 4. Connect your agents

Both supported clients connect over Streamable HTTP with the token as a
bearer credential. The configurations are committed in
[`deploy/mcp-clients/`](../deploy/mcp-clients/) and covered by a smoke test,
so they are guaranteed to match this server's surface — see that directory's
[README](../deploy/mcp-clients/README.md) for both in full.

**Claude Code:**

```bash
cd /your/repo
claude mcp add --transport http context-commander http://127.0.0.1:8490/mcp \
  --header "Authorization: Bearer $CC_TOKEN"
```

**Codex CLI** — append to `~/.codex/config.toml`:

```toml
[mcp_servers.context_commander]
url = "http://127.0.0.1:8490/mcp"
bearer_token_env_var = "CC_TOKEN"
```

## 5. The handoff, end to end

This is the loop the whole product exists for. Agent A does some work and
leaves structured state; agent B, on a different client, continues from it
without seeing A's conversation.

**Agent A**, at the end of its session, tells its client:

> Store what we did in Context Commander: write the design notes to the
> project `you/first-project`, then create a handoff summarizing what is
> done and what comes next.

The client calls `artifact.write` and then `handoff.create`. Nothing is
copied by hand.

**Agent B**, in a fresh session on the other client:

> Pick up the work on `you/first-project`.

The client calls `context.bootstrap`, which returns in one response: the
project and B's role in it, the open handoff with its summary and next
actions, the content of the project's effective skills, the artifacts the
project says to read, and an event cursor to follow along from. B then calls
`handoff.claim` before starting, so a third agent arriving in the meantime
can see the work is already being continued, and `handoff.close` when it is
done.

To watch the same thing from the outside:

```bash
curl -fsS "http://127.0.0.1:8490/v1/projects/you/first-project/context" \
  -H "Authorization: Bearer $CC_TOKEN" | jq
```

Or from the CLI, which prints the same read model in a form built for
reading:

```bash
docker compose run --rm -e CC_TOKEN="$CC_TOKEN" app context --project you/first-project
```

(`-e CC_TOKEN=...` because `docker compose run` starts a fresh container
that does not inherit your shell's environment — the compose file only
carries the database URL and the root key.)

## 6. A second credential for the second agent

Step 5 works with one token in two clients, which is fine for trying it out.
For anything beyond that, give each agent its own credential — revoking one
should never take the other down:

```bash
curl -fsS -X POST http://127.0.0.1:8490/v1/me/tokens \
  -H "Authorization: Bearer $CC_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"client_type": "agent-b"}'
```

List and revoke at any time; revocation takes effect on the next request:

```bash
curl -fsS http://127.0.0.1:8490/v1/me/tokens -H "Authorization: Bearer $CC_TOKEN"
curl -fsS -X DELETE http://127.0.0.1:8490/v1/me/tokens/<id> -H "Authorization: Bearer $CC_TOKEN"
```

To let a colleague's agent into the project instead, create them a user and
add their principal as a member:

```bash
docker compose run --rm app user create --email them@example.com --name "Them"
docker compose run --rm app token create --user them@example.com --client claude-code

curl -fsS -X POST http://127.0.0.1:8490/v1/projects/you/first-project/members \
  -H "Authorization: Bearer $CC_TOKEN" -H 'Content-Type: application/json' \
  -d '{"principal_id": "<their principal id>", "role": "CONTRIBUTOR"}'
```

## Backup and restore

Two things hold state, and both must be captured together — an artifact's
row in Postgres and its bytes in storage are useless apart:

```bash
# Back up
docker compose exec -T postgres pg_dump -U postgres context_commander | gzip > cc-db.sql.gz
docker run --rm -v context-commander-quickstart_cc_app_storage:/data -v "$PWD:/backup" \
  alpine tar czf /backup/cc-storage.tar.gz -C /data .

# Restore into a fresh stack (with the SAME CC_ROOT_KEY — without it the
# backup is unreadable ciphertext)
docker compose up -d postgres
gunzip -c cc-db.sql.gz | docker compose exec -T postgres psql -U postgres context_commander
docker run --rm -v context-commander-quickstart_cc_app_storage:/data -v "$PWD:/backup" \
  alpine tar xzf /backup/cc-storage.tar.gz -C /data
docker compose up -d
```

Store `CC_ROOT_KEY` somewhere other than alongside the backup.

## What this quickstart is and isn't

- **Storage is the filesystem backend**, on the `cc_app_storage` volume.
  `internal/storage/s3` exists but no code path in `ccd serve` constructs it,
  so S3/MinIO is not wired into the running server — treat it as
  experimental and unsupported until it is. This is why there is no MinIO
  container here.
- **Cleartext on loopback.** The server is published to `127.0.0.1` only.
  Reaching it from another machine means putting TLS in front of it first
  (`--tls-cert`/`--tls-key`, or a TLS-terminating reverse proxy) — a bearer
  token and decrypted project content must not cross a shared network in the
  clear, and `ccd serve` refuses to bind a non-loopback address without
  either TLS or an explicit override.
- **The database roles use the built-in development passwords.** That is
  acceptable here only because the Postgres port is not published: nothing
  outside the compose network can reach it. A deployment where the database
  is reachable from anywhere else must change them.

## Where to go next

- [`deploy/mcp-clients/README.md`](../deploy/mcp-clients/README.md) — the two supported clients in detail
- [`docs/architecture-cookbook.md`](architecture-cookbook.md) — why the system is shaped this way
- [`docs/dev-environment.md`](dev-environment.md) — running from source instead

## Security event recording

The server records what it **refused** — failed authentications, denied
capabilities, and rate-limit refusals — to the `security_events` table and to
its structured log. Reads are installation-admin-only:

```bash
curl -fsS http://127.0.0.1:8490/v1/installation/security-events \
  -H "Authorization: Bearer $CC_TOKEN" | jq
```

Each row carries the kind, the principal / credential / project that was
refused, the capability where one applies, a fixed reason string, and the
request id — never a token, a request body, or project content.

**What is recorded**

| Kind | When |
|---|---|
| `authentication_failed` | A bearer token did not resolve to an active credential (unknown, revoked, expired). The credential id from the token is recorded even when it names nothing — that is the row worth having. |
| `authorization_denied` | An authenticated principal was refused a capability on a project, either for lack of membership or because its role does not hold that capability. The reason distinguishes the two. |
| `rate_limited` | A credential exceeded its request rate. |

A request with **no** `Authorization` header at all is not recorded: those are
overwhelmingly health checks and stray browsers, and recording them would bury
the rows that matter.

**Opting out.** `ccd serve --security-log=false` disables recording entirely —
nothing is written to the table and no security event is logged. The server
warns at startup when it starts in that mode. Turning it off is what costs you
the ability to notice an attack in progress or reconstruct an incident
afterwards; the rest of the server behaves identically.

**Rate limits.** Defaults are 100 requests/second sustained per credential with
a burst of 200, and at most 64 authentications in flight at once. Tune with
`--rate-limit-per-credential`, `--rate-limit-burst` and `--max-concurrent-auth`
(`0` disables either mechanism). Limits are **per credential, not per IP**:
`X-Forwarded-For` is not trusted, so behind a reverse proxy a per-IP bucket
would put every caller in one shared bucket, where any one of them could lock
out the rest.
