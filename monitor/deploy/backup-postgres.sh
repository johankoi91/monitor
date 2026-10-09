#!/usr/bin/env bash
set -euo pipefail
umask 077
destination=${1:?new absolute backup file required}
case "$destination" in /*) ;; *) exit 1;; esac
test ! -e "$destination"
install -d -m 0700 "$(dirname "$destination")"
docker exec avops-postgres pg_dump -U avops_owner -d avops_monitor -Fc > "$destination"
docker exec -i avops-postgres pg_restore --list < "$destination" >/dev/null
chmod 0600 "$destination"
echo "PostgreSQL backup verified; encryption key must be backed up separately."
