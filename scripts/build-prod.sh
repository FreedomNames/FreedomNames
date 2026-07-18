#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Release production build for the current platform (stripped,
#  reproducible paths), with the version baked in from the current git tag.
#  For the full cross-platform release matrix, see build-release.sh (CI).

set -euo pipefail
cd "$(dirname "$0")/.."

# Require an exact tag so a production binary always matches a release.
if ! VERSION="$(git describe --tags --exact-match 2>/dev/null)"; then
    echo "ERROR: HEAD is not on a release tag (git describe --exact-match failed)."
    echo "Tag first, or use build.sh for a development build. Exit"
    exit 1
fi
VERSION="${VERSION#v}"

echo "INFO: Building freedom-names ${VERSION} (production) ..."
CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.buildVersion=${VERSION}" \
    -o freedom-names .
echo "INFO: Done: ./freedom-names"
