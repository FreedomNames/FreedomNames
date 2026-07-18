#!/usr/bin/env bash
# By: Melroy van den Berg
# Description: Apply gofmt to the whole tree.

set -euo pipefail
cd "$(dirname "$0")/.."

gofmt -w .
echo "INFO: gofmt applied"
