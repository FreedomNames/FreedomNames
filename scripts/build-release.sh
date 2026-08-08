#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Cross-compile freedom-names for the release matrix, package each
#  binary as a tarball (.tar.gz for linux/darwin, .zip for windows), and write a
#  SHA256SUMS file covering all of them.
# Depends on one environment variable: $APP_VERSION (e.g. "v0.3.0")
#
# Output: build_release/freedom-names-$APP_VERSION-<os>-<arch>.{tar.gz,zip}
#         build_release/SHA256SUMS

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
        -ldflags "-s -w -X gitlab.melroy.org/freedom-names/freedom-names/internal/version.Version=${VERSION}" \
        -o "$OUT_DIR/$bin_name" ./cmd/freedom-names

    base="freedom-names-$APP_VERSION-$GOOS-$GOARCH"
    # Keep the release self-describing for users who download an archive
    # directly. Linux additionally carries the bootstrap systemd unit.
    mkdir -p "$OUT_DIR/package"
    mv "$OUT_DIR/$bin_name" "$OUT_DIR/package/$bin_name"
    cp README.md LICENSE "$OUT_DIR/package/"
    if [ "$GOOS" = "windows" ]; then
        (cd "$OUT_DIR/package" && zip -q "../$base.zip" "$bin_name" README.md LICENSE)
    else
        if [ "$GOOS" = "linux" ]; then
            # The bootstrap installer consumes the unit from the same verified
            # archive as the binary, avoiding an unversioned deployment file.
            mkdir -p "$OUT_DIR/package/deploy"
            cp deploy/freedom-names-bootstrap.service "$OUT_DIR/package/deploy/"
            (cd "$OUT_DIR/package" && tar -czf "../$base.tar.gz" "$bin_name" README.md LICENSE deploy)
        else
            (cd "$OUT_DIR/package" && tar -czf "../$base.tar.gz" "$bin_name" README.md LICENSE)
        fi
    fi
    rm -rf "$OUT_DIR/package"
done

# Checksums over the exact archives being published, so a download from either
# host can be verified with `sha256sum -c SHA256SUMS`. The release is mirrored
# to GitHub, which means users fetch these bytes from a second place we do not
# control; a checksum list published alongside them is what makes the two
# comparable. Generated here rather than in CI so a local release build produces
# the same set of artifacts.
#
# Run from inside OUT_DIR so the file names in SHA256SUMS are bare, which is
# what `sha256sum -c` expects next to the downloaded archives.
(cd "$OUT_DIR" && sha256sum freedom-names-* > SHA256SUMS)

echo "INFO: Release artifacts:"
ls -1 "$OUT_DIR"
echo "INFO: SHA256SUMS:"
cat "$OUT_DIR/SHA256SUMS"
