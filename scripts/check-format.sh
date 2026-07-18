#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Check formatting and vet, like CI does. Fails when a file
#  needs reformatting (use fix-format.sh to apply).

set -euo pipefail
cd "$(dirname "$0")/.."

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
    echo "ERROR: The following files need gofmt:"
    echo "$unformatted"
    exit 1
fi
go vet ./...
echo "INFO: Format and vet OK"
