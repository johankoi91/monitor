#!/usr/bin/env bash
set -euo pipefail
stage=${1:?private material directory required}
test "$(id -u)" = 0
test -f "$stage/postgres.env"
test -z "$(docker ps -aq --filter name=^/avops-postgres$)"
test -z "$(ss -H -lnt sport = :5432)"
test ! -e /var/lib/avops-postgres
install -d -m 0700 /etc/avops-postgres
install -m 0600 "$stage/postgres.env" /etc/avops-postgres/postgres.env
install -d -m 0700 -o 999 -g 999 /var/lib/avops-postgres
docker run -d --name avops-postgres --restart unless-stopped \
  --publish 127.0.0.1:5432:5432 \
  --env-file /etc/avops-postgres/postgres.env \
  --volume /var/lib/avops-postgres:/var/lib/postgresql/data \
  --memory 512m --cpus 0.75 --pids-limit 256 \
  --log-opt max-size=10m --log-opt max-file=3 \
  docker.m.daocloud.io/library/postgres:17@sha256:ae69c452f483507a6b99fb654cf93aad7fe156ffd2c56247707eef4e36d3c12b \
  -c shared_buffers=64MB -c max_connections=40 -c work_mem=4MB \
  -c password_encryption=scram-sha-256 -c log_statement=none
