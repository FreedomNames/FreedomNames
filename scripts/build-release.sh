#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Cross-compile freedom-names for the release matrix and package
#  each binary as a tarball (.tar.gz for linux/darwin, .zip for windows).
# Depends on one environment variable: $APP_VERSION (e.g. "v0.3.0")
#
# Output: build_release/freedom-names-$APP_VERSION-<os>-<arch>.{tar.gz,zip}

set -euo pipefail

if [ -z "${APP_VERSION:-}" ]; then
    echo "ERROR: APP_VERSION env. variable is not set! Exit"
    exit 1
fi

# Strip a leading 'v' so the injected version matches the nodeVersion style.
VERSION="${APP_VERSION#v}"

OUT_DIR="build_release"
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# Release matrix: OS/arch pairs we ship.
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
    "windows/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

for platform in "${PLATFORMS[@]}"; do
    GOOS="${platform%/*}"
    GOARCH="${platform#*/}"

    bin_name="freedom-names"
    [ "$GOOS" = "windows" ] && bin_name="freedom-names.exe"

    echo "INFO: Building $GOOS/$GOARCH ..."
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
        go build -trimpath \
        -ldflags "-s -w -X main.buildVersion=${VERSION}" \
        -o "$OUT_DIR/$bin_name" .

    base="freedom-names-$APP_VERSION-$GOOS-$GOARCH"
    if [ "$GOOS" = "windows" ]; then
        (cd "$OUT_DIR" && zip -q "$base.zip" "$bin_name")
    else
        (cd "$OUT_DIR" && tar -czf "$base.tar.gz" "$bin_name")
    fi
    rm -f "$OUT_DIR/$bin_name"
done

echo "INFO: Release artifacts:"
ls -1 "$OUT_DIR"
