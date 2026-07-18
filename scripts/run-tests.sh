#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Run the unit tests with the race detector, like CI does.

set -euo pipefail
cd "$(dirname "$0")/.."

go test -race ./...
