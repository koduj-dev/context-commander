# Installing

Every way to get Context Commander running, and how to check that what you
downloaded is what was released. To try it on one machine in a few minutes,
the [quickstart](quickstart.md) with Docker Compose is shorter. This page is
for running it for real.

What every installation needs:

- **Postgres 16 or newer** with two roles: `cc_owner`, which owns the schema
  and runs migrations, and `cc_app`, which the server connects as. The server
  never gets the owner's credentials ([ADR 0002](adr/0002-non-recursive-rls.md)).
- **A root key**, `CC_ROOT_KEY`. It encrypts every project key, and through
  them all content ([ADR 0005](adr/0005-key-hierarchy-and-artifact-size-limit.md)). It cannot
  be recovered. Back it up somewhere other than the server before you write
  anything into it.
- **A directory for content.** Artifact and skill bytes live on the
  filesystem and the rest lives in Postgres. Back up both together.

## Debian / Ubuntu & Fedora packages (.deb / .rpm)

Each release publishes `.deb` and `.rpm` packages (amd64 + arm64) on the
[Releases](https://github.com/koduj-dev/context-commander/releases) page.

```bash
sudo apt install ./ccd_<version>_amd64.deb      # Debian / Ubuntu
sudo dnf install ./ccd-<version>.x86_64.rpm     # Fedora / RHEL
```

Or use the **signed APT repository**, so `apt upgrade` keeps it current.
It is the same repository and key as Docker Commander's. If you already have
it configured, `sudo apt install ccd` is enough.

```bash
curl -fsSL https://koduj-dev.github.io/apt/key.asc \
  | sudo tee /etc/apt/keyrings/koduj-dev.asc >/dev/null
echo "deb [signed-by=/etc/apt/keyrings/koduj-dev.asc] https://koduj-dev.github.io/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/koduj-dev.list >/dev/null
sudo apt update && sudo apt install ccd
```

What the package installs:

| Path | What it is |
|------|------------|
| `/usr/bin/ccd` | the server and CLI |
| `man ccd`, `man ccd-serve`, … | one manual page per command, generated from `--help` |
| `ccd.service` | the server, as user `ccd`, on `127.0.0.1:8490` |
| `ccd-migrate.service` | migrations, run before every start of `ccd.service` |
| `/etc/context-commander/ccd.env` | server settings (`root:ccd`, 0640) |
| `/etc/context-commander/migrate.env` | the schema owner's credentials (root only, 0600) |
| `/var/lib/ccd/objects` | content storage |

Both env files are conffiles, so your edits survive upgrades. Postgres is not
installed for you, because it can just as well run on another machine. The
service stays down until it is configured, in three steps.

**1. Create the database and its two roles.** Use real passwords:

```bash
OWNER_PW="$(openssl rand -hex 24)"; APP_PW="$(openssl rand -hex 24)"
sudo -u postgres psql -v ON_ERROR_STOP=1 <<SQL
CREATE ROLE cc_owner NOSUPERUSER NOBYPASSRLS LOGIN PASSWORD '$OWNER_PW';
CREATE ROLE cc_app   NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE LOGIN PASSWORD '$APP_PW';
CREATE DATABASE context_commander OWNER cc_owner;
\connect context_commander
GRANT ALL ON SCHEMA public TO cc_owner;
GRANT USAGE ON SCHEMA public TO cc_app;
SQL
```

Migrations grant `cc_app` exactly the access it needs, table by table, so
nothing more is granted here.

**2. Fill in the two env files.**

```bash
sudo tee -a /etc/context-commander/migrate.env >/dev/null <<EOF
CC_DATABASE_URL=postgres://cc_owner:$OWNER_PW@127.0.0.1:5432/context_commander?sslmode=disable
EOF
sudo tee -a /etc/context-commander/ccd.env >/dev/null <<EOF
CC_DATABASE_URL=postgres://cc_app:$APP_PW@127.0.0.1:5432/context_commander?sslmode=disable
CC_ROOT_KEY=$(openssl rand -base64 32)
EOF
```

Copy `CC_ROOT_KEY` out of `ccd.env` into your password manager now.

To run migrations by hand instead, leave `CC_DATABASE_URL` in `migrate.env`
commented out. `ccd-migrate.service` is then skipped. You run
`ccd migrate` with the owner's URL after each upgrade, before you start the
server.

**3. Start it, and create the first user.**

```bash
sudo systemctl start ccd
curl -fsS http://127.0.0.1:8490/healthz && echo ok

sudo sh -c 'set -a; . /etc/context-commander/ccd.env; exec ccd bootstrap --email you@example.com --name "Your Name"'
```

`bootstrap` prints a personal access token once. That user is the first
installation admin. The settings are loaded with `set -a; .` rather than
passed as arguments, so the database password never shows up in `ps`.

Then open <http://127.0.0.1:8490>, or carry on from step 3 of the
[quickstart](quickstart.md#3-create-a-project).

**Upgrades** are `apt upgrade` or `dnf upgrade`. The package restarts the
service, and the restart runs the new version's migrations first. If a
migration fails, the server stays down rather than running against a schema it
does not expect. See `journalctl -u ccd-migrate`.

**Reaching it from other machines.** The service listens on loopback. Put a
TLS-terminating reverse proxy in front of it, or set `CC_TLS_CERT` and
`CC_TLS_KEY` in `ccd.env` and change the address with `sudo systemctl edit ccd`:

```ini
[Service]
ExecStart=
ExecStart=/usr/bin/ccd serve --addr 0.0.0.0:8490
```

Other settings, such as OIDC or LDAP sign-in and the storage quota, are
listed in `ccd.env` and in `ccd serve --help`.

**Removing** the package stops the service. It keeps `/etc/context-commander`,
`/var/lib/ccd` and the `ccd` user, so a reinstall finds its key and its data.
Delete them by hand to purge.

## Docker

```bash
docker pull ghcr.io/koduj-dev/context-commander:<version>
```

The image is multi-arch (amd64/arm64) and distroless, and it runs as a
non-root user. It is the image the [quickstart](quickstart.md) compose file
builds, so that file is the reference for running it: one container runs
`migrate` as `cc_owner` and exits, and then the server runs as `cc_app`. To
use the published image instead of building one, replace
`build: context: ../..` with `image: ghcr.io/koduj-dev/context-commander:<version>`
in both services.

Pre-releases (`0.1.0-beta.1`) are tagged with their exact version only.
`latest` and `0.1` move with stable releases. In production, pin an
immutable digest (`...@sha256:…`) and verify it (see below).

## Release binary and Homebrew

On a developer machine you need the CLI half of `ccd`: `sync-skills`,
`context` and `token`. The server runs elsewhere.

```bash
brew install koduj-dev/tap/ccd        # macOS & Linux
ccd version
```

Or take the binary for your OS and architecture from the
[Releases](https://github.com/koduj-dev/context-commander/releases) page:
`ccd-linux-*`, `ccd-darwin-*` or `ccd-windows-*.exe`.

## From source

Requires **Go 1.27+**. Node.js 24 is needed only if you change the UI,
because the built UI is committed and embedded.

```bash
git clone https://github.com/koduj-dev/context-commander.git
cd context-commander
make build                # bin/ccd, version "dev"
VERSION=v0.1.0 make release   # every platform into dist-bin/, as CI does
```

## Verifying a download

Every release ships a `SHA256SUMS` and a keyless **cosign** signature bundle
(`SHA256SUMS.bundle`). Together they cover the binaries and the SPDX
**SBOM**. Each binary and package also has its own build **provenance**:

```bash
sha256sum -c SHA256SUMS --ignore-missing        # checksums (binaries + SBOM)

cosign verify-blob --bundle SHA256SUMS.bundle \
  --certificate-identity-regexp '^https://github\.com/koduj-dev/context-commander/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com SHA256SUMS

gh attestation verify ccd_<version>_amd64.deb --repo koduj-dev/context-commander
```

Verifying the bundle needs **cosign v3+**. The container image is signed and
carries SLSA provenance and an SBOM as well:

```bash
IMAGE=ghcr.io/koduj-dev/context-commander@sha256:<digest>
cosign verify "$IMAGE" \
  --certificate-identity-regexp '^https://github\.com/koduj-dev/context-commander/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

gh attestation verify "oci://$IMAGE" --repo koduj-dev/context-commander
```

## Releasing

For maintainers: put the version's notes under `## [x.y.z] — YYYY-MM-DD` in
`CHANGELOG.md`, with a link definition at the bottom. Then tag and push:

```bash
git tag v0.1.0-beta.1 && git push origin v0.1.0-beta.1
```

`release.yml` builds, signs and publishes everything above, and the release
text is that CHANGELOG section. The job fails before publishing anything if
the section is missing, because a GitHub release cannot be changed once it is
created. Then `apt-publish.yml` adds the `.deb` to the APT repository.

A tag with a suffix (`-beta.1`, `-rc.1`) becomes a GitHub pre-release. It
updates the Homebrew formula and the APT repository only while there is no
stable release yet. After 1.0, `apt upgrade` and `brew upgrade` never land on
a release candidate.
