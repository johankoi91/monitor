#!/usr/bin/env bash
set -euo pipefail

# Run only on the notification receiver host, using the directory supplied as $1.
stage_dir="${1:?staging directory is required}"
test "$(id -u)" = 0
test -f "$stage_dir/notification-receiver"
test -f "$stage_dir/receiver.env"
test -f "$stage_dir/server.pem"
test -f "$stage_dir/server-key.pem"
test -f "$stage_dir/avops-notification-receiver.service"

if ! id avops-notify >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /sbin/nologin avops-notify
fi
install -d -m 0755 /opt/avops-notification-receiver
install -d -m 0750 -o root -g avops-notify /etc/avops-notification-receiver
install -d -m 0700 -o avops-notify -g avops-notify /var/lib/avops-notification-receiver
install -m 0755 "$stage_dir/notification-receiver" /opt/avops-notification-receiver/notification-receiver
install -m 0600 "$stage_dir/receiver.env" /etc/avops-notification-receiver/receiver.env
install -m 0640 -o root -g avops-notify "$stage_dir/server.pem" /etc/avops-notification-receiver/server.pem
install -m 0640 -o root -g avops-notify "$stage_dir/server-key.pem" /etc/avops-notification-receiver/server-key.pem
install -m 0644 "$stage_dir/avops-notification-receiver.service" /etc/systemd/system/avops-notification-receiver.service
systemctl daemon-reload
systemctl enable avops-notification-receiver.service
systemctl restart avops-notification-receiver.service
systemctl is-active avops-notification-receiver.service
