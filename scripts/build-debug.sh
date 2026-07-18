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
    -ldflags "-X main.buildVersion=${VERSION}-debug" \
    -o freedom-names .
echo "INFO: Done: ./freedom-names"
