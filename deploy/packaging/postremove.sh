#!/bin/sh
# Reload systemd after the unit file is removed. The data dir (/var/lib/ccd),
# /etc/context-commander/ccd.env (which holds CC_ROOT_KEY) and the `ccd` user
# are deliberately left in place so reinstalling keeps the database key and
# stored objects; remove them by hand to purge.
set -e

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi

exit 0
