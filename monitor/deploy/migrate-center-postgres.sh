#!/usr/bin/env bash
set -euo pipefail
umask 077
stage=${1:?validated staged binaries directory required}
backup=/var/backups/avops-center-pg-20261007
private=/etc/avops-monitor/pg-stage-20261007
test "$(id -u)" = 0
test -f "$stage/center"
test -f "$stage/dbmigrate"
test -f "$private/database.env"
command -v jq >/dev/null
test ! -e "$backup/legacy-state"
curl -fsS --config /etc/avops-monitor/admin-curl.conf 'http://127.0.0.1:18084/api/v1/operations?active=true' | jq -e '.operations|length==0' >/dev/null
systemctl stop avops-center
install -d -m 0700 "$backup"
cp -a /var/lib/avops-monitor "$backup/legacy-state"
install -m 0755 /opt/avops-monitor/center "$backup/center-before-postgres"
cp -a /etc/avops-monitor/center.env /etc/avops-monitor/center.yaml "$backup/"
set -a
. "$private/owner.env"
set +a
if ! "$stage/dbmigrate" -import-dir "$backup/legacy-state" > "$backup/import-manifest.json"; then
  systemctl start avops-center
  echo 'Import failed and rolled back; original center restored.' >&2
  exit 1
fi
install -m 0600 "$private/database.env" /etc/avops-monitor/database.env
install -d -m 0755 /etc/systemd/system/avops-center.service.d
install -m 0644 "$stage/center-postgres.conf" /etc/systemd/system/avops-center.service.d/postgres.conf
install -m 0755 "$stage/center" /opt/avops-monitor/center
systemctl daemon-reload
systemctl start avops-center
systemctl is-active avops-center
echo 'PostgreSQL cutover started; source files and original binary preserved.'
