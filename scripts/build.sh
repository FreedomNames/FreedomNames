#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Development build. Bakes the version from the nearest git tag
#  (e.g. "0.8.1" on the tag itself, "0.8.1-3-g480ccee" past it) into the
#  binary, so /health and /info always report where the build came from.

set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(git describe --tags --always --dirty)"
VERSION="${VERSION#v}"

echo "INFO: Building freedom-names ${VERSION} ..."
go build -ldflags "-X main.buildVersion=${VERSION}" -o freedom-names .
echo "INFO: Done: ./freedom-names"
