#!/usr/bin/env bash
set -euo pipefail
# One collector per host, only accessible through host loopback.
test "$(id -u)" = 0
test -z "$(docker ps -aq --filter name=^/avops-cadvisor$)"
test -z "$(ss -H -lnt sport = :18089)"
image=${AVOPS_CADVISOR_IMAGE:-avops-cadvisor:1.0.4}
docker image inspect "$image" >/dev/null
docker run -d --pull=never --name avops-cadvisor --restart unless-stopped \
  --privileged --device /dev/kmsg \
  --memory 256m --cpus 0.5 --pids-limit 128 \
  --publish 127.0.0.1:18089:8080 \
  --volume /:/rootfs:ro --volume /var/run:/var/run:ro \
  --volume /sys:/sys:ro --volume /var/lib/docker:/var/lib/docker:ro \
  --log-opt max-size=10m --log-opt max-file=3 \
  "$image" \
  --docker_only=true --housekeeping_interval=5s \
  --allow_dynamic_housekeeping=false --store_container_labels=false \
  --enable_metrics=cpu,memory,network,disk,diskIO,oom_event
# No InfluxDB/Kafka/storage driver; Agent consumes only the local v2 stats API.
