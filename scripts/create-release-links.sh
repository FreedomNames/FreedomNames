#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Add asset links to an existing GitLab Release (created via the
#  GitLab UI). Links point at the versioned Generic Package Registry package.
# Depends on one environment variable: $APP_VERSION (e.g. "v0.3.0")

set -uo pipefail

if [ -z "${APP_VERSION:-}" ]; then
    echo "ERROR: APP_VERSION env. variable is not set! Exit"
    exit 1
fi

url_for() {
    # $1 = package filename
    echo "${PACKAGE_REGISTRY_URL}/$1"
}

# Add one release asset link. $1 = display name, $2 = package filename,
# $3 = link_type (defaults to "package"; the checksum list is "other", since it
# is not a package to download and run).
add_link() {
    curl --fail-with-body --request POST \
        --header "JOB-TOKEN: $CI_JOB_TOKEN" \
        --data link_type="${3:-package}" \
        --data name="$1" \
        --data url="$(url_for "$2")" \
        "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/releases/$APP_VERSION/assets/links"
}

output=$(curl -s --header "JOB-TOKEN: $CI_JOB_TOKEN" "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/releases/$APP_VERSION/assets/links")
if [[ "$output" == "" ]]; then
    echo "ERROR: Retrieving links from API returns an empty request! Something is wrong."
    exit 1
fi

if [[ "$output" == "[]" ]]; then
    echo "INFO: Creating new release links for FreedomNames $APP_VERSION!"

    # !! In the reverse order of how we want the links to be displayed !!
    # Meaning the first added, will be the displayed last.
    add_link "FreedomNames - SHA256 checksums"                      "SHA256SUMS" "other"
    add_link "FreedomNames - macOS arm64 (Apple Silicon) (.tar.gz)" "freedom-names-$APP_VERSION-darwin-arm64.tar.gz"
    add_link "FreedomNames - macOS amd64 (Intel) (.tar.gz)"         "freedom-names-$APP_VERSION-darwin-amd64.tar.gz"
    add_link "FreedomNames - Windows arm64 (.zip)"                  "freedom-names-$APP_VERSION-windows-arm64.zip"
    add_link "FreedomNames - Windows amd64 (.zip)"                  "freedom-names-$APP_VERSION-windows-amd64.zip"
    add_link "FreedomNames - Linux arm64 (.tar.gz)"                 "freedom-names-$APP_VERSION-linux-arm64.tar.gz"

    # Added last, so it is displayed first (see reverse-order note above).
    add_link "FreedomNames - Linux amd64 (.tar.gz)"                 "freedom-names-$APP_VERSION-linux-amd64.tar.gz"

elif [[ "$output" == "{\"message\":\"404 Not found\"}" ]]; then
    echo "WARN: Release doesn't exist yet/can't be found yet in GitLab: $APP_VERSION..."
else
    echo "INFO: Links already exist. Skipping creating new links for FreedomNames $APP_VERSION!"
fi
