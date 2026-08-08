#!/usr/bin/env bash
# Contract checks for the standalone, root-only release installer.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
installer="$script_dir/install.sh"

bash -n "$installer"
help_output="$(bash "$installer" --help)"
[[ "$help_output" == *'raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh'* ]]
[[ "$help_output" == *'<bootstrap|normal>'* ]]
[[ "$help_output" == *'bootstrap also installs and starts systemd'* ]]

# Bash leaves BASH_SOURCE unset for scripts consumed from standard input.
pipe_help_output="$(bash -s -- --help <"$installer")"
[[ "$pipe_help_output" == *'raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh'* ]]
[[ "$pipe_help_output" == *'<bootstrap|normal>'* ]]

test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

# Source only helpers, then mock the API response used by latest_version.
# shellcheck disable=SC1090
source "$installer"
work_dir="$test_dir"
download() {
  printf '%s\n' '{"tag_name":"v0.9.4"}' >"$2"
}
[[ "$(latest_version)" == v0.9.4 ]]

archive='freedom-names-v0.9.4-linux-amd64.tar.gz'
printf '%s\n' payload >"$test_dir/$archive"
checksum="$(sha256sum "$test_dir/$archive" | awk '{print $1}')"
printf '%s  %s\n' "$checksum" "$archive" >"$test_dir/SHA256SUMS"
(
  cd "$test_dir"
  verify_archive "$archive" SHA256SUMS
)
printf '%s\n' altered >"$test_dir/$archive"
if (
  cd "$test_dir"
  verify_archive "$archive" SHA256SUMS
) 2>/dev/null; then
  echo 'checksum mismatch unexpectedly passed' >&2
  exit 1
fi

grep -Fq 'freedom-names-bootstrap.service' "$installer"
grep -Fq 'release archive predates the bootstrap systemd unit' "$installer"
grep -Fq 'systemctl enable' "$installer"
grep -Fq 'systemctl restart' "$installer"
grep -Fq 'bootstrap|normal) main "$1"' "$installer"
grep -Fq 'Freedom Names normal node is installed.' "$installer"
grep -Fq 'missing mode' "$installer"
