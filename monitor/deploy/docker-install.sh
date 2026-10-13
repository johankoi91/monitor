#!/usr/bin/env bash
set -euo pipefail
# Fresh install from the extracted offline bundle. Configuration is private.
role=${1:?center or agent required}
config=${2:?absolute private configuration directory required}
data=${3:?absolute data directory required}
name=${4:-avops-$role}
case "$role" in center|agent) ;; *) exit 1;; esac
case "$config:$data" in /*:/*) ;; *) exit 1;; esac
test "$(id -u)" = 0
test -d "$config"
test -z "$(docker ps -aq --filter "name=^/$name$")"
bundle=$(cd "$(dirname "$0")/.." && pwd)
version=$(tr -d '\r\n' < "$bundle/VERSION")
test -n "$version"
cd "$bundle"
sha256sum --check --quiet SHA256SUMS
docker load -i images/avops-images-linux-amd64.tar >/dev/null
install -d -m 0700 "$data"
if test "$role" = center; then
  for file in center.env database.env center.yaml server.pem server-key.pem; do test -f "$config/$file"; done
  # Root-private env files are consumed by Docker, never mounted into the image.
  runtime_config="$data/runtime-config"
  install -d -m 0755 "$runtime_config"
  for file in center.yaml server.pem server-key.pem; do install -m 0400 -o 65532 -g 65532 "$config/$file" "$runtime_config/$file"; done
  chown 65532:65532 "$data"
  docker run -d --pull=never --name "$name" --restart unless-stopped --network host \
    --env-file "$config/center.env" --env-file "$config/database.env" \
    -e AVOPS_CENTER_CONFIG=/config/center.yaml -e AVOPS_DATA_DIR=/state \
    -e AVOPS_TLS_CERT=/config/server.pem -e AVOPS_TLS_KEY=/config/server-key.pem \
    -e AVOPS_STORAGE_DISK_PATH=/state \
    -v "$runtime_config:/config:ro" -v "$data:/state" \
    --read-only --tmpfs /tmp:size=16m,mode=1777 --memory 256m --cpus 0.5 --pids-limit 256 \
    --security-opt no-new-privileges --log-opt max-size=10m --log-opt max-file=3 "avops-center:$version"
else
  test -f "$config/agent.env"
  test -f "$config/restart-rules.yaml"
  docker run -d --pull=never --name "$name" --restart unless-stopped --network host \
    --env-file "$config/agent.env" -e AVOPS_AGENT_DATA_DIR=/state \
    -e AVOPS_RESTART_RULES_FILE=/config/restart-rules.yaml \
    -v /var/run/docker.sock:/var/run/docker.sock -v "$data:/state" \
    -v "$config/restart-rules.yaml:/config/restart-rules.yaml:ro" \
    --read-only --tmpfs /tmp:size=16m,mode=1777 --memory 128m --cpus 0.25 --pids-limit 128 \
    --security-opt no-new-privileges --log-opt max-size=10m --log-opt max-file=3 "avops-agent:$version"
fi
