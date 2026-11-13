# Backup and restore

What must be backed up for a full restore of Context Commander, in which
order, and how to restore it and prove it worked. Everything here was worked
out from the code and run end to end; see "How this was tested" at the end.

## What to back up

Three things. A backup missing any one of them is not a backup.

| What | Where | Why it matters |
|---|---|---|
| **The root key**, `CC_ROOT_KEY` | An environment variable: `.env` next to the compose file, or `/etc/context-commander/ccd.env` for the packages. There is no other way to provide it. | It wraps every project key, and the project keys encrypt all content ([ADR 0005](adr/0005-key-hierarchy-and-artifact-size-limit.md)). It is never written to the database or to storage. |
| **The database**, `context_commander` | The `cc_postgres_data` volume, or your Postgres server | Everything except the bytes of artifacts and skills: users, token and session hashes, organizations, projects, members, the wrapped project keys, and the encrypted memories, secrets, handoffs, history, plans, ideas, audit payloads and embedding vectors. |
| **The object store** | `CC_STORAGE_DIR`: the `cc_app_storage` volume mounted at `/data` in the image, `/var/lib/ccd/objects` for the packages, `./data/objects` by default | Artifact versions (`projects/<id>/artifacts/…`, encrypted) and skill package versions (`skills/<id>/versions/…`, not encrypted). The database names each file; it does not contain it. |

**Keep the root key apart from the data backups.** Someone holding both can
read everything; someone holding only the dump and the storage archive reads
ciphertext, apart from skill packages and plaintext metadata. Put it in a password manager or a vault, where at least two
people can reach it.

**If the root key is lost, all encrypted content is gone.** Artifacts,
memories, secrets, handoffs, history, plans and ideas cannot be decrypted by
anyone, by any means: there is no recovery key and no way around it. What
remains readable is plaintext metadata: users, project names, artifact
paths, memberships, the usage ledger. The database and storage backups do
not help without the key.

Also keep, with your configuration rather than your data:

- **`.env` and `compose.override.yml`** (or `ccd.env` and any
  `systemctl edit ccd` drop-in). `.env` holds `CC_ROOT_KEY`, so treat a copy
  of it as a copy of the key.
- **Files the server reads at startup**, if you use them: the OIDC client
  secret (`CC_OIDC_CLIENT_SECRET` or `--oidc-client-secret-file`; Google can
  issue a new one), the LDAP bind password file, TLS certificates, and the
  usage price table (`CC_USAGE_PRICES`). A price table kept on the storage
  volume, as [team-deployment.md](team-deployment.md) suggests with
  `/data/usage-prices.json`, is already in the storage backup.

## What does not need a backup

- **Sign-in signing keys.** The key that signs the OIDC sign-in attempt
  cookie is derived from the root key ([ADR 0023](adr/0023-oidc-hardening.md));
  there is no separate secret. Browser sessions and tokens are rows in the
  database (`client_credentials`), so they come back with it. A sign-in in
  progress during the restore just has to be started again.
- **The first-run setup code.** It only exists in memory while the
  installation has no users.
- **Embedding vectors.** They are in the database and come back with it.
  They can also be rebuilt from the memories: `ccd embeddings reindex`, with
  the server's `--embeddings-*` settings and the same root key
  ([semantic-search.md](semantic-search.md)). The decrypted vector cache
  (`--embeddings-cache-mb`) is process memory.
- **Ollama models** (`cc_ollama_models`). Pull the same model again:
  `docker compose exec ollama ollama pull bge-m3`. A different model is
  fine too; the server re-embeds memories in the background.
- **Rate limits, presence streams and other in-memory state.** They start
  empty, as after any restart.

## Order: the database first, then storage

Take the dump first and copy the storage directory after it. Then the
server can keep running during the backup.

The write path of [ADR 0013](adr/0013-transactional-outbox-and-storage-lifecycle.md)
is what makes this safe. An artifact or skill write uploads its bytes under a
new, never-reused name first, and only then commits the row that names them.
So every row in a dump points to a file that already existed when the dump
was taken, and a storage copy made afterwards contains it. The other order
can miss files: a write that uploads after the storage copy and commits
before the dump leaves a row whose file is not in the backup.

Copying storage second can capture files that no row in the dump names yet
(a write that was in flight). They are unused and harmless.

**Nothing has to be paused.** Today nothing in `ccd` deletes a stored file.
Deleting an artifact only marks it deleted. `PurgeArtifact` and
`SweepOrphanObjects` in `internal/artifacts` implement the physical cleanup
that ADR 0013 describes, but no command and no background job calls them.
If that ever changes, revisit this section: a purge that runs between the
dump and the storage copy would delete files the dump still names, so the
cleanup would have to stop for the length of the backup.

`pg_dump` reads one consistent snapshot, so the dump never contains half of
a transaction.

## Roles, owners and extensions

- **Dump and restore as the superuser** (`postgres` in the compose files).
  It sees every row regardless of row-level security and can restore objects
  owned by other roles.
- **Keep the owners.** Do not pass `--no-owner` or `--role` to `pg_restore`.
  The tables must belong to `cc_owner`: the RLS policies rely on the owner
  being exempt from them ([ADR 0002](adr/0002-non-recursive-rls.md)). The
  `SECURITY DEFINER` functions (35 of them at the time of writing) run with
  their owner's rights, so they must belong to `cc_owner` as well. Restored
  with `--no-owner`, all 47 tables and all 35 functions end up owned by
  `postgres`, and the integration test below fails.
- **The roles must exist before the restore.** `cc_owner` and `cc_app` are
  cluster-wide, so a database dump does not contain them. In the compose
  setup a fresh `cc_postgres_data` volume creates them
  (`deploy/postgres/init/01-roles.sql`). On your own Postgres, create them
  first with the same attributes, and with the passwords your connection
  strings use. `pg_dumpall --roles-only` on the old server prints them.
- **Restore into an empty database owned by `cc_owner`, before anything
  migrates it.** In the quickstart compose file, `docker compose up` runs the
  `migrate` service, which creates the schema, and the restore then fails on
  objects that already exist. Start only `postgres` first.
- **Extensions.** `pgcrypto` is in the dump and is created by the restore.
  If you use `--embeddings-store=pgvector`, the new Postgres must also have
  pgvector (the `pgvector/pgvector:pg16` image): a fresh volume of that image
  creates the extension through `deploy/postgres/init/02-pgvector.sql`, and
  the restore keeps it. A dump with pgvector tables cannot be restored into
  the plain `postgres:16-alpine` image.
- **Same or newer Postgres major version.** `pg_restore` restores into a
  newer major version, not an older one.
- **`ccd migrate` after a restore is safe.** On a dump from the same release
  it finds nothing to do. On a dump from an older release it applies the
  missing migrations, as an upgrade would. The compose deployment runs it on
  every start anyway.

## Backup with Docker Compose

`deploy/backup/backup.sh` does all of the above for the quickstart and team
deployment (`deploy/quickstart/docker-compose.yml`). Run it from the
directory with the compose file and `.env`:

```bash
cd deploy/quickstart
../backup/backup.sh /srv/backups/cc-$(date -u +%F)
```

It writes:

| File | Contents |
|---|---|
| `db.dump` | `pg_dump --format=custom` of `context_commander`, owners included |
| `storage.tar.gz` | the `cc_app_storage` volume |
| `root-key-refs.txt` | fingerprints of the root key(s) the project keys are wrapped under, never the key |
| `SHA256SUMS` | checksums of the three |

If the `CC_ROOT_KEY` compose would start the server with does not match the
fingerprints, it prints a warning. That catches a key that was changed in
`.env` while the old one may still be found.

To pass other compose options, set `COMPOSE`, for example
`COMPOSE="docker compose -f /srv/cc/docker-compose.yml"`. Schedule it with
cron or a systemd timer and copy the directory off the host.

The same, by hand:

```bash
docker compose exec -T postgres pg_dump -U postgres -d context_commander --format=custom > db.dump
docker run --rm -v context-commander-quickstart_cc_app_storage:/data:ro -v "$PWD:/backup" \
  alpine:3 tar -C /data -czf /backup/storage.tar.gz .
```

The volume name is `<compose project>_cc_app_storage`; `docker volume ls`
shows it.

## Restore with Docker Compose

Restore into a **fresh** deployment: new, empty `cc_postgres_data` and
`cc_app_storage` volumes, and `.env` with **the same `CC_ROOT_KEY`**. On the
same host, remove the old volumes by name (`docker volume rm …`), or use
another compose project name. `docker compose down -v` also removes them, but
it removes every volume of the project
([gotchas.md](gotchas.md#docker-compose-down--v-is-project-wide-not-service-scoped)).

```bash
cd deploy/quickstart
../backup/restore.sh /srv/backups/cc-2026-11-13
```

`restore.sh`:

1. checks `SHA256SUMS`;
2. compares the fingerprint of `CC_ROOT_KEY` (read the way compose reads it)
   with `root-key-refs.txt`, and **stops before changing anything** if they
   differ;
3. starts only `postgres`, refuses a database that already has tables, and
   runs `pg_restore --exit-on-error --single-transaction` as `postgres` with
   owners kept;
4. creates the `cc_app_storage` volume, refuses one that is not empty, and
   unpacks the archive with numeric owners (the image runs as uid 65532);
5. runs `docker compose up -d`: `migrate` first (nothing to do), then the
   server.

The same, by hand:

```bash
docker compose up -d --wait postgres
docker compose exec -T postgres pg_restore -U postgres -d context_commander \
  --exit-on-error --single-transaction < db.dump
docker compose create app
docker run --rm -v context-commander-quickstart_cc_app_storage:/data -v "$PWD:/backup:ro" \
  alpine:3 tar -C /data -xzpf /backup/storage.tar.gz --numeric-owner
docker compose up -d
```

## Packages (systemd) and your own Postgres

The same steps, with your paths and roles:

```bash
# Backup: database first, then storage.
sudo -u postgres pg_dump -d context_commander --format=custom > db.dump
sudo tar -C /var/lib/ccd/objects -czf storage.tar.gz .

# Restore: roles exist, database empty and owned by cc_owner, server stopped.
sudo systemctl stop ccd
sudo -u postgres createdb -O cc_owner context_commander
sudo -u postgres pg_restore -d context_commander --exit-on-error --single-transaction db.dump
sudo tar -C /var/lib/ccd/objects -xzpf storage.tar.gz --numeric-owner
sudo systemctl start ccd     # runs ccd-migrate.service first
```

`CC_ROOT_KEY` in `/etc/context-commander/ccd.env` must be the old key.

## Verify a restore

`restore.sh` has already checked the key. Then:

1. **The server is up and the key is right.** `docker compose logs app`
   shows `listening on …` and no line containing `wrapped under root key`.
2. **Read one of each kind of content.** Sign in, open a project, and open
   an artifact (this reads storage and the database), a memory and a
   handoff. Resolve a secret from the CLI, which is the only place a secret
   is shown in plaintext:

   ```bash
   docker compose run --rm -e CC_TOKEN=cc_... app \
     secret resolve --project <namespace>/<slug> --name <name>
   ```

3. **Check the owners** if you restored by hand:

   ```bash
   docker compose exec -T postgres psql -U postgres -d context_commander -Atc \
     "SELECT tableowner, count(*) FROM pg_tables WHERE schemaname = 'public' GROUP BY 1"
   ```

   One line, `cc_owner|<n>`.

A failed restore shows up like this:

| Symptom | Cause |
|---|---|
| Every content read is `500 internal error`, and the log says `project key was wrapped under root key "env:…", but the current root key is "env:…"` | The wrong `CC_ROOT_KEY` |
| Memories and secrets read fine, artifacts fail | The storage archive was not restored, or was restored to another directory |
| `permission denied` on artifact writes | The storage files are not owned by uid 65532: unpack with `--numeric-owner`, as root |
| `pg_restore: error: … already exists` | The database was not empty, usually because `migrate` ran before the restore |

**Rehearse it.** A backup that has never been restored is a hope. Restore
into a separate compose project now and then, without touching production:

```bash
cp /srv/cc/.env /tmp/cc-drill/.env      # the same CC_ROOT_KEY
COMPOSE="docker compose -p cc-drill --env-file /tmp/cc-drill/.env -f /srv/cc/docker-compose.yml" \
  deploy/backup/restore.sh /srv/backups/cc-2026-11-13
```

That project publishes the same port, so stop the production `app` while the
drill runs, or add an override file with another port.

## The fingerprint

`root_key_ref` in `project_keys` records which root key wrapped each project
key: `env:` followed by the first 16 hex characters of the SHA-256 of the
key's 32 bytes. It identifies the key without revealing it. To compare a key
by hand:

```bash
printf '%s' "$CC_ROOT_KEY" | base64 -d | sha256sum | cut -c1-16
docker compose exec -T postgres psql -U postgres -d context_commander -Atc \
  "SELECT DISTINCT root_key_ref FROM project_keys"
```

## Known gaps

- **The server starts with the wrong root key.** `ccd serve` does not check
  the key against `project_keys` at startup, so it comes up, serves the
  interface and sign-in, and then fails every read of encrypted content
  with a 500.
  The cause is only in the log. `restore.sh` checks the fingerprint before
  starting the server; a manual restore should do the same. A startup check
  would need a way for `cc_app` to read `root_key_ref` past row-level
  security, which is a privilege change and wants its own decision.
- **The root key cannot be rotated.** `RotateRootKey` exists in
  `internal/crypto`, but no command runs it. If the key leaks, there is no
  supported way to re-wrap the project keys under a new one yet.
- **The root key comes only from an environment variable.** There is no
  file, KMS or Vault provider yet, so the key sits in `.env` or `ccd.env` on
  the server, and backing up those files backs up the key.
- **Deleted artifacts are never cleaned up from storage** (see "Order"
  above). Good for backups, but storage only grows.
- **S3 is not covered.** `internal/storage/s3` exists, but `ccd serve` only
  builds the filesystem store, so S3 is not a deployment option yet.

## How this was tested

- **Integration test:** `TestDumpAndRestoreRoundTrip` in
  `internal/dbtest/restore_integration_test.go` runs on every `make test`. It
  writes an artifact, a memory and a secret through the services, dumps the
  database with `pg_dump --format=custom` and restores it with `pg_restore`
  into a new, empty database (`dbtest.DumpAndRestore`). It then checks that
  every table and `SECURITY DEFINER` function still belongs to `cc_owner`,
  that `ccd migrate` succeeds on the copy, that all three read back with the
  same key and the same object store, that RLS still keeps a non-member out,
  and that a different key decrypts nothing.
- **Compose drill:** with the quickstart compose file, plain
  `postgres:16-alpine` and again with `pgvector/pgvector:pg16` and
  `migrate --pgvector`. Bootstrap, create a project, write an artifact over
  HTTP, a memory over HTTP and a secret through the CLI, run `backup.sh`,
  run `docker compose down -v`, check that `restore.sh` refuses a different
  key, restore with the right one, then read all three back through the
  server and the CLI.
