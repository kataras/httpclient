#!/usr/bin/env bash
# Builds every example module against the checked-out library.
# Each example has its own go.mod with a replace directive pointing at the
# repository root, so nothing is fetched from the network.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

for dir in "$root"/_examples/*/; do
    [ -f "$dir/go.mod" ] || continue
    echo "==> building ${dir}"
    (cd "$dir" && go mod download && go build ./... && go vet ./...)
done
