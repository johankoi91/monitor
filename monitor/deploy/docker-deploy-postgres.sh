#!/usr/bin/env bash
set -euo pipefail
test "$(id -u)" = 0
test -f /etc/avops-postgres/postgres.env
docker compose -f /opt/avops-monitor/docker-postgres/compose.yml up -d
docker inspect avops-postgres --format '{{.State.Status}}'
