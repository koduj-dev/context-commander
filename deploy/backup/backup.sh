#!/bin/sh
# Back up a Context Commander compose deployment — docs/backup-restore.md.
#
#   cd deploy/quickstart && ../backup/backup.sh [DIR]
#
# Writes DIR (default ./cc-backup-<UTC timestamp>) with:
#   db.dump            pg_dump custom format of the whole database, owners kept
#   storage.tar.gz     the cc_app_storage volume (artifact and skill bytes)
#   root-key-refs.txt  fingerprints of the root key(s) the project keys are
#                      wrapped under — never the key itself
#   SHA256SUMS
#
# It does NOT write CC_ROOT_KEY. Without that key the backup is unreadable
# ciphertext; keep the key somewhere else (a password manager, a vault).
#
# The order is the point: the database first, then the storage volume. A
# write uploads its bytes before it commits the row that names them
# (docs/adr/0013) and nothing in ccd deletes stored bytes, so every row in a
# dump taken first names a file that is already on the volume when it is
# copied second. The other order can capture a row whose bytes it missed.
# That makes a backup of the running server safe; stopping it is not needed.
#
# Environment:
#   COMPOSE   the compose command, default "docker compose"; add -f/-p/
#             --env-file here, e.g. COMPOSE="docker compose -f /srv/cc/docker-compose.yml"
#   CC_BACKUP_IMAGE  image used to read the volume, default alpine:3
set -eu

COMPOSE=${COMPOSE:-docker compose}
IMAGE=${CC_BACKUP_IMAGE:-alpine:3}
DB=context_commander
SUPERUSER=postgres

die() {
	echo "backup: $*" >&2
	exit 1
}

out=${1:-cc-backup-$(date -u +%Y%m%dT%H%M%SZ)}
if [ -e "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ]; then
	die "$out exists and is not empty"
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)

# The compose project name, to find its storage volume by label rather than
# guessing the "<project>_cc_app_storage" spelling.
project=$($COMPOSE config | sed -n 's/^name: *//p' | head -n 1)
[ -n "$project" ] || die "could not read the compose project name from '$COMPOSE config'"
volume=$(docker volume ls -q \
	--filter "label=com.docker.compose.project=$project" \
	--filter "label=com.docker.compose.volume=cc_app_storage")
[ -n "$volume" ] || die "no cc_app_storage volume for compose project $project"

echo "backup: database -> $out/db.dump"
$COMPOSE exec -T postgres pg_dump -U "$SUPERUSER" -d "$DB" --format=custom >"$out/db.dump"

$COMPOSE exec -T postgres psql -U "$SUPERUSER" -d "$DB" -Atq \
	-c "SELECT DISTINCT root_key_ref FROM project_keys ORDER BY 1" >"$out/root-key-refs.txt"

echo "backup: volume $volume -> $out/storage.tar.gz"
# The helper runs as root; hand the archive back to whoever ran the script.
docker run --rm -v "$volume:/data:ro" -v "$out:/backup" "$IMAGE" \
	sh -c "tar -C /data -czf /backup/storage.tar.gz . && chown $(id -u):$(id -g) /backup/storage.tar.gz"

(cd "$out" && sha256sum db.dump storage.tar.gz root-key-refs.txt >SHA256SUMS)

# Warn now, while the old key may still be found, if the key compose would
# start the server with is not the one the data is wrapped under.
key=$($COMPOSE config app | sed -n 's/^ *CC_ROOT_KEY: *//p' | tr -d "\"'" | head -n 1)
if [ -n "$key" ] && [ -s "$out/root-key-refs.txt" ]; then
	ref="env:$(printf '%s' "$key" | base64 -d | sha256sum | cut -c1-16)"
	if ! grep -qx "$ref" "$out/root-key-refs.txt"; then
		echo "backup: WARNING: CC_ROOT_KEY ($ref) is not the key the project keys are wrapped under:" >&2
		sed 's/^/backup:   /' "$out/root-key-refs.txt" >&2
	fi
fi

echo "backup: done. CC_ROOT_KEY is not in $out — store it separately."
