#!/usr/bin/env bash
set -euo pipefail
# Internal deployment rollback. Check and finish/resolve active operations first.
# Data/journals remain intact; rollback never deletes operation identities.
role=${1:?center or agent required}
test "$(id -u)" = 0
case "$role" in
  center)
    test -f /opt/avops-monitor/center.previous
    test -f /etc/avops-monitor/center.yaml.previous
    test -f /etc/avops-monitor/center.env.previous
    systemctl stop avops-center
    install -m 0755 /opt/avops-monitor/center.previous /opt/avops-monitor/center.next
    mv /opt/avops-monitor/center.next /opt/avops-monitor/center
    install -m 0600 /etc/avops-monitor/center.env.previous /etc/avops-monitor/center.env
    for file in center.yaml server.pem server-key.pem; do
      if test -f "/etc/avops-monitor/$file.previous"; then install -m 0640 -o root -g avops-monitor "/etc/avops-monitor/$file.previous" "/etc/avops-monitor/$file"; fi
    done
    systemctl start avops-center
    systemctl is-active avops-center
    ;;
  agent)
    test -f /opt/avops-monitor/agent.previous
    test -f /etc/avops-monitor/agent.env.previous
    systemctl stop avops-agent
    install -m 0755 /opt/avops-monitor/agent.previous /opt/avops-monitor/agent.next
    mv /opt/avops-monitor/agent.next /opt/avops-monitor/agent
    install -m 0600 /etc/avops-monitor/agent.env.previous /etc/avops-monitor/agent.env
    systemctl start avops-agent
    systemctl is-active avops-agent
    ;;
  *) exit 2 ;;
esac
