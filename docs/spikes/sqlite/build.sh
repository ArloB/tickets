#!/usr/bin/env bash
# Assertion 7: CGO_ENABLED=0 cross-compile for windows/amd64 and
# linux/amd64 from one machine. Run from the module root:
#   docs/spikes/sqlite/build.sh
set -euo pipefail
cd "$(dirname "$0")/../../.."

out=/tmp/sqlite-xcompile
mkdir -p "$out"

echo "--- windows/amd64, CGO_ENABLED=0 ---"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$out/spike-windows.exe" ./docs/spikes/sqlite
echo "--- linux/amd64, CGO_ENABLED=0 ---"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$out/spike-linux" ./docs/spikes/sqlite

echo "Built: $out/spike-windows.exe and $out/spike-linux"
echo "Run each on its native platform to confirm assertions 1-6 also pass there."
