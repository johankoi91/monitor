#!/usr/bin/env bash
set -euo pipefail
export COPYFILE_DISABLE=1
module=$(cd "$(dirname "$0")/.." && pwd)
repo=$(cd "$module/.." && pwd)
version=$(tr -d '\r\n' < "$repo/VERSION")
test "$version" = V1.0
work=${AVOPS_RELEASE_WORK:?absolute public build directory required}
images=${AVOPS_RELEASE_IMAGES:?absolute directory with all six image archives required}
output=${AVOPS_RELEASE_OUTPUT:-$repo/releases}
case "$work:$images:$output" in /*:/*:/*) ;; *) exit 1;; esac
mkdir -p "$work/bin" "$output"
cd "$module/frontend"
npm run build
cd "$module"
for tool in center agent provision pgprovision dbmigrate canary; do
 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w' -o "$work/bin/$tool" "./cmd/$tool"
done
cd "$repo/notification_receiver"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '-s -w' -o "$work/bin/notification-receiver" .
python3 "$module/scripts/release.py" bundle --repo "$repo" --bin "$work/bin" --images "$images" --output "$output"
