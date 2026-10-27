#!/bin/sh
# Dedicated unprivileged user, an owned data dir, locked-down config files,
# and the enabled service. Idempotent and safe on both install and upgrade
# (it restarts into the freshly-installed binary, which runs migrations
# first — see ccd.service). Runs as root from dpkg/rpm.
set -e

if ! getent passwd ccd >/dev/null 2>&1; then
	useradd --system --no-create-home --shell /usr/sbin/nologin ccd
fi

install -d -o ccd -g ccd -m 0750 /var/lib/ccd /var/lib/ccd/objects

# Both files hold secrets and are never world-readable. systemd reads them
# itself before dropping privileges. ccd.env is the server's own settings, so
# its group may read it too; migrate.env holds the schema owner's password,
# which the server must not have (docs/adr/0002), and stays root's alone.
chown root:ccd /etc/context-commander/ccd.env
chmod 0640 /etc/context-commander/ccd.env
chown root:root /etc/context-commander/migrate.env
chmod 0600 /etc/context-commander/migrate.env

# CC_ROOT_KEY cannot be generated here: there is no safe default, and losing
# a silently-generated key would make everything written with it unreadable.
if ! grep -q '^CC_ROOT_KEY=.' /etc/context-commander/ccd.env; then
	cat >&2 <<-'MSG'
	ccd: not configured yet. Create the database roles, then fill in
	  /etc/context-commander/ccd.env      (CC_DATABASE_URL as cc_app, CC_ROOT_KEY)
	  /etc/context-commander/migrate.env  (CC_DATABASE_URL as cc_owner)
	and run: sudo systemctl start ccd
	Step by step: https://github.com/koduj-dev/context-commander/blob/main/docs/install.md
	MSG
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl enable ccd.service || true
	# `restart` (not `start`) so an upgrade actually runs the new binary; on a
	# fresh install it just starts it. Harmless while unconfigured — it fails
	# and stays down until `systemctl start ccd`.
	systemctl restart ccd.service || true
fi

exit 0
