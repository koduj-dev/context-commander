#!/bin/sh
# Stop + disable the service ONLY on a real removal — NOT on upgrade, where the
# service should keep running and is handled by the new package's postinstall.
# dpkg passes "remove" on removal (and "upgrade" on upgrade); rpm passes 0 on
# uninstall (and 1 on upgrade). Runs as root.
set -e

if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
	if [ -d /run/systemd/system ]; then
		systemctl stop ccd.service ccd-purge.timer || true
		systemctl disable ccd.service ccd-purge.timer || true
	fi
fi

exit 0
