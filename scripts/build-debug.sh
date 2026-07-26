#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Debug build: race detector on, optimizations and inlining off
#  for accurate stepping in a debugger (delve).

set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(git describe --tags --always --dirty)"
VERSION="${VERSION#v}"

echo "INFO: Building freedom-names ${VERSION} (debug) ..."
go build -race -gcflags "all=-N -l" \
    -ldflags "-X gitlab.melroy.org/freedom-names/freedom-names/internal/version.Version=${VERSION}-debug" \
    -o freedom-names ./cmd/freedom-names
echo "INFO: Done: ./freedom-names"
