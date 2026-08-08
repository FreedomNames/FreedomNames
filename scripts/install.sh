#!/usr/bin/env bash
# Freedom Names release installer. Bootstrap mode also installs a systemd service.
set -euo pipefail

readonly REPOSITORY='FreedomNames/FreedomNames'
readonly SERVICE_NAME='freedom-names-bootstrap'
readonly SERVICE_USER='freedom'
readonly SERVICE_HOME='/home/freedom'
readonly INSTALL_DIR='/usr/local/bin'
readonly SERVICE_UNIT='freedom-names-bootstrap.service'

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[1;33m'
reset='\033[0m'

error() { printf '%berror:%b %s\n' "$red" "$reset" "$*" >&2; exit 1; }
info() { printf '%b::%b %s\n' "$green" "$reset" "$*"; }
warn() { printf '%bwarning:%b %s\n' "$yellow" "$reset" "$*" >&2; }

cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf "$work_dir"
  fi
}

require_root() {
  [[ "$(id -u)" -eq 0 ]] || error 'run this installer as root (for example: curl ... | sudo bash -s -- bootstrap)'
}

require_platform() {
  [[ "$(uname -s)" == Linux ]] || error "Linux is required (got $(uname -s))"
  [[ -r /etc/os-release ]] || error 'cannot identify the Linux distribution'
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    debian|ubuntu) ;;
    *) error "only Debian and Ubuntu are supported (got ${PRETTY_NAME:-unknown})" ;;
  esac
  command -v systemctl >/dev/null 2>&1 || error 'systemd is required (systemctl was not found)'
}

install_dependencies() {
  local missing=()
  command -v curl >/dev/null 2>&1 || missing+=(curl)
  command -v tar >/dev/null 2>&1 || missing+=(tar)
  command -v sha256sum >/dev/null 2>&1 || missing+=(coreutils)
  dpkg-query --show --showformat='${db:Status-Status}' ca-certificates 2>/dev/null | grep -qx installed || missing+=(ca-certificates)
  if ((${#missing[@]})); then
    info "Installing required packages: ${missing[*]}"
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates "${missing[@]}"
  fi
}

detect_architecture() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s\n' amd64 ;;
    aarch64|arm64) printf '%s\n' arm64 ;;
    *) error "unsupported architecture: $(uname -m) (supported: x86_64, aarch64)" ;;
  esac
}

download() {
  local url=$1 destination=$2
  curl --fail --location --show-error --silent --retry 3 --retry-delay 1 --output "$destination" "$url"
}

latest_version() {
  local release_json version
  release_json="$work_dir/release.json"
  download "https://api.github.com/repos/${REPOSITORY}/releases/latest" "$release_json" || error 'could not resolve the latest GitHub release'
  version="$(sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$release_json" | head -n1)"
  [[ -n "$version" ]] || error 'the latest GitHub release has no tag name'
  printf '%s\n' "$version"
}

verify_archive() {
  local archive=$1 checksums=$2 checksum_line
  checksum_line="$(grep -E "^[[:xdigit:]]{64}[[:space:]]{2}${archive}$" "$checksums" || true)"
  [[ -n "$checksum_line" ]] || error "SHA256SUMS has no checksum for ${archive}"
  printf '%s\n' "$checksum_line" | sha256sum --check --status - || error "checksum verification failed for ${archive}"
}

main() {
  local mode=$1
  require_root
  require_platform
  install_dependencies

  local arch version archive was_active=false
  arch="$(detect_architecture)"
  work_dir="$(mktemp -d)"
  trap cleanup EXIT
  version="$(latest_version)"
  archive="freedom-names-${version}-linux-${arch}.tar.gz"

  info "Installing Freedom Names ${version} ${mode} node for linux-${arch}"
  download "https://github.com/${REPOSITORY}/releases/download/${version}/${archive}" "$work_dir/$archive" || error "could not download GitHub release ${version}"
  download "https://github.com/${REPOSITORY}/releases/download/${version}/SHA256SUMS" "$work_dir/SHA256SUMS" || error "could not download checksums for GitHub release ${version}"
  # SHA256SUMS contains a relative archive name; verify it from the download directory.
  (cd "$work_dir" && verify_archive "$archive" SHA256SUMS)
  tar --no-same-owner -xzf "$work_dir/$archive" -C "$work_dir"

  [[ -x "$work_dir/freedom-names" ]] || error 'release archive does not contain freedom-names'
  if [[ "$mode" == bootstrap && ! -f "$work_dir/deploy/$SERVICE_UNIT" ]]; then
    # Releases before the installer shipped only the binary. Keep the new
    # installer useful against the current release while all future Linux
    # archives carry this unit alongside their verified binary.
    warn 'release archive predates the bootstrap systemd unit; downloading the current unit'
    mkdir -p "$work_dir/deploy"
    download "https://raw.githubusercontent.com/${REPOSITORY}/main/deploy/${SERVICE_UNIT}" "$work_dir/deploy/$SERVICE_UNIT" || error 'could not download the bootstrap systemd unit'
  fi
  "$work_dir/freedom-names" --version >/dev/null || error 'release binary could not be executed'

  install -D -m 0755 "$work_dir/freedom-names" "$INSTALL_DIR/freedom-names"
  if [[ "$mode" == normal ]]; then
    printf '\n%s\n' 'Freedom Names normal node is installed.'
    printf '%s\n' '  Start: freedom-names'
    return
  fi

  getent group "$SERVICE_USER" >/dev/null || groupadd --system "$SERVICE_USER"
  id "$SERVICE_USER" >/dev/null 2>&1 || useradd --system --gid "$SERVICE_USER" --create-home --home-dir "$SERVICE_HOME" --shell /usr/sbin/nologin "$SERVICE_USER"
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0700 "$SERVICE_HOME/.freedom"
  chown "$SERVICE_USER:$SERVICE_USER" "$SERVICE_HOME"

  install -D -m 0644 "$work_dir/deploy/$SERVICE_UNIT" "/etc/systemd/system/${SERVICE_NAME}.service"
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    was_active=true
  fi
  systemctl daemon-reload
  systemctl enable "$SERVICE_NAME"
  if [[ "$was_active" == true ]]; then
    systemctl restart "$SERVICE_NAME"
  else
    systemctl start "$SERVICE_NAME"
  fi

  printf '\n%s\n' 'Freedom Names bootstrap node is installed and running.'
  printf '%s\n' "  Service: systemctl status ${SERVICE_NAME}"
  printf '%s\n' '  Health:  curl --fail http://127.0.0.1:8430/health'
  printf '%s\n' '  Info:    curl --fail http://127.0.0.1:8430/info'
  printf '%s\n' "  Identity: ${SERVICE_HOME}/.freedom/private.key (back this up)"
}

# BASH_SOURCE is unset when Bash reads this script from standard input, which is
# the documented curl | sudo bash invocation.  Fall back to $0 in that case,
# while retaining the guard that prevents main from running when sourced.
if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
  case "${1:-}" in
    bootstrap|normal) main "$1" ;;
    --help|-h)
      printf '%s\n' 'Usage: curl -fsSL https://raw.githubusercontent.com/FreedomNames/FreedomNames/main/scripts/install.sh | sudo bash -s -- <bootstrap|normal>'
      printf '%s\n' 'Installs the latest release on Debian/Ubuntu hosts; bootstrap also installs and starts systemd.'
      ;;
    '') error 'missing mode (try: bootstrap or normal)' ;;
    *) error "unknown mode: $1 (try: bootstrap or normal)" ;;
  esac
fi
