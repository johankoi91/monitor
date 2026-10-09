#!/usr/bin/env bash
set -euo pipefail
# Build an offline x86_64 bundle. Keys, environment files and runtime data stay out.
task_module=$(cd "$(dirname "$0")/.." && pwd)
task_repo=$(cd "$task_module/.." && pwd)
task_version=${AVOPS_RELEASE_VERSION:-1.0.1}
case "$task_version" in ''|*[!0-9A-Za-z.-]*) exit 1;; esac
task_release="$task_module/build/release/monitor-$task_version-linux-amd64"
test ! -e "$task_release"
mkdir -p "$task_release/bin" "$task_release/images" "$task_release/source"
cd "$task_module"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$task_release/bin/center" ./cmd/center
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$task_release/bin/agent" ./cmd/agent
for task_tool in dbmigrate pgprovision pgverify; do CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$task_release/bin/$task_tool" "./cmd/$task_tool"; done
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$task_release/bin/viewer-darwin-arm64" ./cmd/viewer
test -s "$task_module/build/cadvisor-release-linux-amd64"
cp "$task_module/build/cadvisor-release-linux-amd64" "$task_release/bin/cadvisor"
cd "$task_repo/notification_receiver"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$task_release/bin/notification-receiver" .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$task_release/bin/receiver-viewer-darwin-arm64" ./cmd/viewer
test -f "$task_module/build/avops-images-$task_version-linux-amd64.tar"
cp "$task_module/build/avops-images-$task_version-linux-amd64.tar" "$task_release/images/avops-images-linux-amd64.tar"
test -f "$task_module/build/avops-postgres-17-linux-amd64.tar"
cp "$task_module/build/avops-postgres-17-linux-amd64.tar" "$task_release/images/"
cd "$task_repo"
tar -cf - --exclude=.git --exclude=node_modules --exclude=build --exclude=dist --exclude=data --exclude='*.pem' --exclude='*.key' --exclude='*.env' --exclude='.env*' --exclude='*.jsonl' --exclude='*.log' --exclude='*.pid' --exclude='*.tar' --exclude='*.tar.gz' --exclude='*.mp4' --exclude='*.wav' --exclude='*.m4a' --exclude='*.mov' --exclude=admin-curl.conf --exclude=receiver-curl.conf --exclude=center.yaml --exclude='./notification_receiver/notification-receiver' . | tar -xf - -C "$task_release/source"
cp "$task_module/deploy/OFFLINE-INSTALL.md" "$task_release/README.md"
cd "$task_release"
shasum -a 256 bin/* images/* > SHA256SUMS
cd "$(dirname "$task_release")"
tar -czf "monitor-$task_version-linux-amd64.tar.gz" "monitor-$task_version-linux-amd64"
printf '%s\n' "$task_module/build/release/monitor-$task_version-linux-amd64.tar.gz"
