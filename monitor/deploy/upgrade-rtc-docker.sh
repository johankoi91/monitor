#!/usr/bin/env bash
# RTC-specific reviewed cutover. Requires explicit business downtime authority.
# Stage directory is passed after all binaries/images and the warm backup exist.
set -euo pipefail
umask 077
stage=${1:?validated stage directory required}
backup=/var/backups/avops-docker-20261006
test "$(id -u)" = 0
test -f "$stage/docker-28.5.2.conf"
test -f "$stage/finalize-rtc-docker.sh"
command -v jq >/dev/null
test -f /opt/avops-docker/28.5.2/dockerd
test -d "$backup/docker-root"
test ! -e "$backup/cutover-started"
test ! -e /etc/systemd/system/docker.service.d/avops-upgrade.conf
test "$(docker version --format '{{.Server.Version}}')" = 18.09.0
test "$(docker info --format '{{.DockerRootDir}}')" = /var/lib/docker
test "$(docker info --format '{{.Driver}}')" = overlay2
test "$(docker info --format '{{.Swarm.LocalNodeState}}')" = inactive
install -d -m 0700 "$backup/config"
iptables-save > "$backup/config/iptables-before"
ip6tables-save > "$backup/config/ip6tables-before"
firewall-cmd --zone=public --list-all > "$backup/config/firewall-public-runtime"
firewall-cmd --permanent --zone=public --list-all > "$backup/config/firewall-public-permanent"
cp -a /etc/sysconfig/docker /etc/sysconfig/docker-storage /etc/sysconfig/docker-network "$backup/config/"
cp -a /usr/lib/systemd/system/docker.service "$backup/config/"
if test -d /etc/docker; then cp -a /etc/docker "$backup/config/etc-docker"; fi
if test -d /etc/systemd/system/docker.service.d; then cp -a /etc/systemd/system/docker.service.d "$backup/config/"; fi
mapfile -t all_ids < <(docker ps -q --no-trunc)
test "${#all_ids[@]}" -ge 13
docker inspect "${all_ids[@]}" > "$backup/before-private.json"
jq -e 'all(.[]; .State.Running == true)' "$backup/before-private.json" >/dev/null
mapfile -t ids < <(jq -r 'sort_by(.Created) | .[].Id' "$backup/before-private.json")
date -u +%FT%TZ > "$backup/cutover-started"
systemctl stop avops-agent
# Explicitly stop the recorded running instances before a major runtime upgrade.
# They will be started by their original IDs, never recreated or deleted.
docker stop -t 30 "${ids[@]}" > "$backup/stopped-ids"
for id in "${ids[@]}"; do test "$(docker inspect -f '{{.State.Running}}' "$id")" = false; done
systemctl stop docker
# Complete the already-prepared data copy with the daemon and containers stopped.
rsync -aHAX --numeric-ids --delete --exclude='/overlay2/*/merged' /var/lib/docker/ "$backup/docker-root/"
date -u +%FT%TZ > "$backup/backup-consistent"
install -d -m 0755 /etc/systemd/system/docker.service.d
install -m 0644 "$stage/docker-28.5.2.conf" /etc/systemd/system/docker.service.d/avops-upgrade.conf
systemctl daemon-reload
if ! systemctl start docker; then
  echo 'New Docker did not start; original data/config are preserved for reviewed rollback.' >&2
  exit 1
fi
test "$(/opt/avops-docker/28.5.2/docker version --format '{{.Server.Version}}')" = 28.5.2
test "$(/opt/avops-docker/28.5.2/docker info --format '{{.Driver}}')" = overlay2
for id in "${ids[@]}"; do /opt/avops-docker/28.5.2/docker start "$id"; done
/opt/avops-docker/28.5.2/docker inspect "${ids[@]}" > "$backup/after-private.json"
# Preserve the already-authorized management entry after firewalld integration.
firewall-cmd --zone=public --add-port=20220/tcp
firewall-cmd --permanent --zone=public --add-port=20220/tcp
bash "$stage/finalize-rtc-docker.sh"
