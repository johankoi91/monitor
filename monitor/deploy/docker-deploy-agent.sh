#!/usr/bin/env bash
set -euo pipefail
# Fresh install; does not stop production services or overwrite a container.
bundle=${1:?extracted offline bundle required}
config=${2:?private config directory required}
data=${3:?persistent data directory required}
name=${4:-avops-agent}
exec bash "$bundle/deploy/docker-install.sh" agent "$config" "$data" "$name"
