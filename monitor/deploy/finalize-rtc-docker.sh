#!/usr/bin/env bash
set -euo pipefail
umask 077
backup=/var/backups/avops-docker-20261006
test -f "$backup/backup-consistent"
test "$(/opt/avops-docker/28.5.2/docker version --format '{{.Server.Version}}')" = 28.5.2
mapfile -t ids < <(jq -r 'sort_by(.Created)|.[].Id' "$backup/before-private.json")
/opt/avops-docker/28.5.2/docker inspect "${ids[@]}" > "$backup/after-private.json"
normalize='sort_by(.Id)|map({Id,Image,Config:(.Config|{Image,Env,Entrypoint,Cmd,WorkingDir,User,Healthcheck}),HostConfig:(.HostConfig|{Binds,PortBindings,NetworkMode,RestartPolicy,Privileged,Memory,NanoCpus}|.NetworkMode|=if .=="default" then "bridge" else . end),Mounts:(.Mounts|map({Type,Source,Destination,RW})|sort_by(.Destination))})'
jq "$normalize" "$backup/before-private.json" > "$backup/before-config-private.json"
jq "$normalize" "$backup/after-private.json" > "$backup/after-config-private.json"
cmp -s "$backup/before-config-private.json" "$backup/after-config-private.json"
jq -e 'all(.[]; .State.Running == true)' "$backup/after-private.json" >/dev/null
install -m 0755 /opt/avops-docker/28.5.2/docker /usr/local/bin/docker
systemctl start avops-agent
date -u +%FT%TZ > "$backup/cutover-completed"
jq -n --slurpfile before "$backup/before-private.json" --slurpfile after "$backup/after-private.json" '{engine_version:"28.5.2",configuration_preserved:true,network_alias_normalized:"default=bridge",containers:[$before[0][] as $b | $after[0][] | select(.Id==$b.Id)|{name:.Name,id:.Id,before_started_at:$b.State.StartedAt,after_started_at:.State.StartedAt,running:.State.Running}]}' > "$backup/proof.json"
echo 'Docker 28.5.2 running; original IDs, images, launch configuration and mounts preserved.'
