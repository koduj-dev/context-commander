#!/bin/sh
# Restore a backup made by backup.sh into a FRESH compose deployment —
# docs/backup-restore.md.
#
#   cd deploy/quickstart && ../backup/restore.sh DIR
#
# "Fresh" means an empty database (a new cc_postgres_data volume, so the init
# scripts have created cc_owner and cc_app) and an empty cc_app_storage
# volume. The script refuses to restore over data.
#
# CC_ROOT_KEY is read exactly as compose reads it (the environment, or .env
# next to the compose file). It must be the key the backup was taken under;
# the script compares fingerprints and stops before the server is started
# with the wrong one.
#
# Environment: COMPOSE and CC_BACKUP_IMAGE, as in backup.sh.
set -eu

COMPOSE=${COMPOSE:-docker compose}
IMAGE=${CC_BACKUP_IMAGE:-alpine:3}
DB=context_commander
SUPERUSER=postgres

die() {
	echo "restore: $*" >&2
	exit 1
}

[ $# -eq 1 ] || die "usage: restore.sh BACKUP_DIR"
in=$(cd "$1" && pwd)
for f in db.dump storage.tar.gz root-key-refs.txt SHA256SUMS; do
	[ -f "$in/$f" ] || die "$in/$f is missing"
done
(cd "$in" && sha256sum -c --quiet SHA256SUMS) || die "checksums in $in/SHA256SUMS do not match"

# 1. The key, before anything is written.
key=$($COMPOSE config app | sed -n 's/^ *CC_ROOT_KEY: *//p' | tr -d "\"'" | head -n 1)
[ -n "$key" ] || die "CC_ROOT_KEY is not set for the app service"
ref="env:$(printf '%s' "$key" | base64 -d | sha256sum | cut -c1-16)"
if [ -s "$in/root-key-refs.txt" ] && ! grep -qx "$ref" "$in/root-key-refs.txt"; then
	echo "restore: CC_ROOT_KEY has fingerprint $ref, but this backup's project keys are wrapped under:" >&2
	sed 's/^/restore:   /' "$in/root-key-refs.txt" >&2
	die "wrong root key — with it nothing encrypted in this backup can be read. Nothing was changed."
fi
echo "restore: CC_ROOT_KEY matches the backup ($ref)"

# 2. The database: an empty one only.
$COMPOSE up -d --wait postgres
tables=$($COMPOSE exec -T postgres psql -U "$SUPERUSER" -d "$DB" -Atq \
	-c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")
[ "$tables" = "0" ] || die "database $DB already has $tables tables; restore needs a fresh cc_postgres_data volume"

echo "restore: database <- $in/db.dump"
# As the superuser and WITH owners: tables must belong to cc_owner, the
# RLS-exempt owner role the policies rely on (docs/adr/0002), and the
# SECURITY DEFINER functions must keep running as it. --no-owner would hand
# everything to postgres and break both.
$COMPOSE exec -T postgres pg_restore -U "$SUPERUSER" -d "$DB" \
	--exit-on-error --single-transaction <"$in/db.dump"

# 3. The storage volume: create it the way compose would, then fill it.
$COMPOSE create app >/dev/null
project=$($COMPOSE config | sed -n 's/^name: *//p' | head -n 1)
volume=$(docker volume ls -q \
	--filter "label=com.docker.compose.project=$project" \
	--filter "label=com.docker.compose.volume=cc_app_storage")
[ -n "$volume" ] || die "no cc_app_storage volume for compose project $project"
docker run --rm -v "$volume:/data" "$IMAGE" sh -c '[ -z "$(ls -A /data)" ]' ||
	die "volume $volume is not empty; restore needs a fresh one"

echo "restore: volume $volume <- $in/storage.tar.gz"
# Numeric owners: the files belong to the image's nonroot user, 65532, which
# has no name inside the helper image.
docker run --rm -v "$volume:/data" -v "$in:/backup:ro" "$IMAGE" \
	tar -C /data -xzpf /backup/storage.tar.gz --numeric-owner

# 4. Start everything. The migrate service runs first and finds nothing to
# do on a dump from the same release; on a dump from an older one it applies
# what is missing, exactly as an upgrade would.
$COMPOSE up -d
echo "restore: done. Sign in and read an artifact, a memory and a secret to verify (docs/backup-restore.md)."
