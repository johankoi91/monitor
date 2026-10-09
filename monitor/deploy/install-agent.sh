#!/usr/bin/env bash
set -euo pipefail
# Usage: bash install-agent.sh STAGED_DIRECTORY
# Docker access is local; restart targets require explicit approved rules.
stage=${1:?staged directory required}
test "$(id -u)" = 0
for file in agent agent.env avops-agent.service; do test -f "$stage/$file"; done
test -S /var/run/docker.sock
if test -f /opt/avops-monitor/agent; then
  install -m 0755 /opt/avops-monitor/agent /opt/avops-monitor/agent.previous
  if test -f /etc/avops-monitor/agent.env; then install -m 0600 /etc/avops-monitor/agent.env /etc/avops-monitor/agent.env.previous; fi
fi
install -d -m 0755 /opt/avops-monitor
install -d -m 0700 /etc/avops-monitor
install -d -m 0700 /var/lib/avops-agent
install -m 0755 "$stage/agent" /opt/avops-monitor/agent.next
mv /opt/avops-monitor/agent.next /opt/avops-monitor/agent
install -m 0600 "$stage/agent.env" /etc/avops-monitor/agent.env
install -m 0644 "$stage/avops-agent.service" /etc/systemd/system/avops-agent.service
systemctl daemon-reload
systemctl enable avops-agent.service
systemctl restart avops-agent.service
systemctl is-active avops-agent.service
