#!/usr/bin/env bash
set -euo pipefail
# Provision/migrate PostgreSQL before first start; runtime credentials are separate.
# Usage: bash install-center.sh STAGED_DIRECTORY
# Directory contains center, center.env, center.yaml, server.pem, server-key.pem,
# and avops-center.service. It never touches existing RTC/logging containers.
stage=${1:?staged directory required}
test "$(id -u)" = 0
for file in center center.env center.yaml database.env center-postgres.conf server.pem server-key.pem avops-center.service; do test -f "$stage/$file"; done
if test -f /opt/avops-monitor/center; then
  install -m 0755 /opt/avops-monitor/center /opt/avops-monitor/center.previous
  for file in center.env center.yaml server.pem server-key.pem; do
    if test -f "/etc/avops-monitor/$file"; then install -m 0600 "/etc/avops-monitor/$file" "/etc/avops-monitor/$file.previous"; fi
  done
fi
if ! id avops-monitor >/dev/null 2>&1; then useradd --system --no-create-home --shell /sbin/nologin avops-monitor; fi
install -d -m 0755 /opt/avops-monitor
install -d -m 0750 -o root -g avops-monitor /etc/avops-monitor
install -d -m 0700 -o avops-monitor -g avops-monitor /var/lib/avops-monitor
install -m 0755 "$stage/center" /opt/avops-monitor/center.next
mv /opt/avops-monitor/center.next /opt/avops-monitor/center
install -m 0600 "$stage/center.env" /etc/avops-monitor/center.env
install -m 0600 "$stage/database.env" /etc/avops-monitor/database.env
install -d -m 0755 /etc/systemd/system/avops-center.service.d
install -m 0644 "$stage/center-postgres.conf" /etc/systemd/system/avops-center.service.d/postgres.conf
install -m 0640 -o root -g avops-monitor "$stage/center.yaml" /etc/avops-monitor/center.yaml
install -m 0640 -o root -g avops-monitor "$stage/server.pem" /etc/avops-monitor/server.pem
install -m 0640 -o root -g avops-monitor "$stage/server-key.pem" /etc/avops-monitor/server-key.pem
install -m 0644 "$stage/avops-center.service" /etc/systemd/system/avops-center.service
systemctl daemon-reload
systemctl enable avops-center.service
systemctl restart avops-center.service
systemctl is-active avops-center.service
