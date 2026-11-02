#!/usr/bin/env bash
set -euo pipefail
export COPYFILE_DISABLE=1
bundle=${1:?absolute extracted bundle directory required}
test -d "$bundle/images"
test -f "$bundle/VERSION"
cd "$bundle"
python3 source/monitor/deploy/verify-bundle-images.py "$bundle"
find bin images deploy source -type f -print0 | sort -z | xargs -0 shasum -a 256 > SHA256SUMS
shasum -a 256 VERSION README.md >> SHA256SUMS
cd "$(dirname "$bundle")"
tar --no-xattrs -czf "$(basename "$bundle").tar.gz" "$(basename "$bundle")"
shasum -a 256 "$(basename "$bundle").tar.gz" > "$(basename "$bundle").tar.gz.sha256"
