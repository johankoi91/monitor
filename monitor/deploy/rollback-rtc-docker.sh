#!/usr/bin/env bash
set -euo pipefail
umask 077
backup=/var/backups/avops-docker-20261006
test "$(id -u)" = 0
test -f "$backup/backup-consistent"
test -f "$backup/before-private.json"
test -d "$backup/docker-root"
test ! -e /var/lib/docker.failed-avops-20261006
systemctl stop avops-agent
if /opt/avops-docker/28.5.2/docker info >/dev/null 2>&1; then
  mapfile -t running < <(/opt/avops-docker/28.5.2/docker ps -q)
  if test "${#running[@]}" -gt 0; then /opt/avops-docker/28.5.2/docker stop -t 30 "${running[@]}"; fi
fi
systemctl stop docker
# Keep the entire migrated root for diagnosis; restore the consistent old copy.
mv /var/lib/docker /var/lib/docker.failed-avops-20261006
mv "$backup/docker-root" /var/lib/docker
mv /etc/systemd/system/docker.service.d/avops-upgrade.conf "$backup/config/avops-upgrade.conf.disabled"
if test -f /usr/local/bin/docker; then mv /usr/local/bin/docker "$backup/config/docker-cli-28.5.2"; fi
systemctl daemon-reload
systemctl start docker
test "$(/usr/bin/docker version --format '{{.Server.Version}}')" = 18.09.0
mapfile -t ids < <(jq -r 'sort_by(.Created)|.[].Id' "$backup/before-private.json")
for id in "${ids[@]}"; do /usr/bin/docker start "$id"; done
systemctl start avops-agent
date -u +%FT%TZ > "$backup/rolled-back"
echo 'Original Docker root restored; migrated root retained for diagnosis.'
