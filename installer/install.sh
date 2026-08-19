#!/usr/bin/env bash
#
# InfraPilot installer.
#
# Installs the InfraPilot Agent and CLI on a Debian or Ubuntu host: detects the
# platform, installs binaries, creates a dedicated service account and its
# directories, installs the systemd unit, and starts and verifies the service.
#
# Usage:
#   sudo ./install.sh [options]
#
# Options:
#   --prefix DIR     Install binaries under DIR/bin (default /usr/local)
#   --from DIR       Take prebuilt binaries from DIR instead of building
#   --no-start       Install and enable, but do not start the service
#   --dry-run        Print what would be done, change nothing
#   --uninstall      Remove the service, binaries and account, keeping data
#   -h, --help       Show this help
#
# Existing configuration is never overwritten. Existing data is never removed,
# including by --uninstall: deleting a database is the operator's decision, and
# a script that made it for them would be a bug, not a convenience.

set -euo pipefail

readonly SERVICE_NAME="infrapilot-agent"
readonly WEB_SERVICE_NAME="infrapilot-web"
readonly SERVICE_USER="infrapilot"
readonly SERVICE_GROUP="infrapilot"
readonly CONFIG_DIR="/etc/infrapilot"
readonly DATA_DIR="/var/lib/infrapilot"
readonly UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}.service"

# Permissions. These are the documented filesystem contract; see docs/security.md.
readonly DIR_MODE=0750     # directories: owner rwx, group rx, others nothing
readonly CONFIG_MODE=0640  # configuration: owner rw, group r, others nothing
readonly BIN_MODE=0755     # binaries: executable by all, writable only by root

PREFIX="/usr/local"
FROM_DIR=""
DRY_RUN=0
NO_START=0
UNINSTALL=0

# repo_root is the directory containing this script's parent, so the installer
# works when invoked by any path.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log()  { printf '\033[0;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[0;32m  ok\033[0m %s\n' "$*"; }
warn() { printf '\033[0;33m  warning\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[0;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# run executes a command, or prints it under --dry-run.
#
# Arguments are passed through as an array and never re-parsed by a shell, so a
# path containing a space or a shell metacharacter cannot become a command.
run() {
  if (( DRY_RUN )); then
    printf '  would run: %s\n' "$*"
    return 0
  fi
  "$@"
}

usage() {
  # Print the header comment, stopping at the first blank comment-free line so
  # the help text cannot drift from the documentation above it.
  sed -n '3,23p' "${BASH_SOURCE[0]}" | sed 's/^#\{1,\} \{0,1\}//'
}

parse_args() {
  while (( $# )); do
    case "$1" in
      --prefix)     [[ $# -ge 2 ]] || die "--prefix needs a directory"; PREFIX="$2"; shift 2 ;;
      --from)       [[ $# -ge 2 ]] || die "--from needs a directory";   FROM_DIR="$2"; shift 2 ;;
      --no-start)   NO_START=1; shift ;;
      --dry-run)    DRY_RUN=1; shift ;;
      --uninstall)  UNINSTALL=1; shift ;;
      -h|--help)    usage; exit 0 ;;
      *)            die "unknown option: $1 (try --help)" ;;
    esac
  done

  [[ "$PREFIX" == /* ]] || die "--prefix must be an absolute path, got: $PREFIX"
  if [[ -n "$FROM_DIR" ]]; then
    [[ -d "$FROM_DIR" ]] || die "--from directory does not exist: $FROM_DIR"
  fi
}

# --- Detection -------------------------------------------------------------

require_root() {
  if (( DRY_RUN )); then
    return 0
  fi
  [[ "$(id -u)" -eq 0 ]] || die "this installer must run as root; try: sudo $0"
}

detect_os() {
  log "Detecting the operating system"

  [[ "$(uname -s)" == "Linux" ]] || die "InfraPilot supports Linux; this host runs $(uname -s)"
  [[ -r /etc/os-release ]] || die "cannot read /etc/os-release, so the distribution is unknown"

  # Sourced in a subshell-scoped function: os-release is a shell fragment by
  # design, but only the fields below are used.
  local ID="" VERSION_ID="" PRETTY_NAME=""
  # shellcheck disable=SC1091
  . /etc/os-release

  case "${ID:-}" in
    ubuntu|debian) ok "${PRETTY_NAME:-${ID} ${VERSION_ID:-}}" ;;
    *)
      warn "${PRETTY_NAME:-${ID:-unknown}} is not a tested platform; v0.1.0 targets Ubuntu and Debian"
      warn "continuing, but systemd and a Debian-style layout are assumed"
      ;;
  esac
}

detect_arch() {
  log "Detecting the architecture"

  local machine
  machine="$(uname -m)"
  case "$machine" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)       die "unsupported architecture: $machine (amd64 and arm64 are supported)" ;;
  esac
  ok "$machine ($ARCH)"
}

require_systemd() {
  command -v systemctl >/dev/null 2>&1 || die "systemctl not found; this installer requires systemd"
  [[ -d /run/systemd/system ]] || warn "systemd does not appear to be running; the service cannot be started"
}

# --- Build and install -----------------------------------------------------

# resolve_binaries locates the two binaries, building them when they were not
# supplied. Building requires the Go toolchain; a packager passes --from and
# needs no compiler on the target host.
resolve_binaries() {
  if [[ -n "$FROM_DIR" ]]; then
    log "Using prebuilt binaries from $FROM_DIR"
    BIN_SRC="$FROM_DIR"
    local binary
    for binary in infrapilot infrapilot-agent infrapilot-web; do
      [[ -f "${BIN_SRC}/${binary}" ]] || die "missing binary: ${BIN_SRC}/${binary}"
    done
    ok "found infrapilot, infrapilot-agent and infrapilot-web"
    return
  fi

  log "Building from source"
  command -v go >/dev/null 2>&1 || die "the Go toolchain is not installed; build elsewhere and pass --from DIR"
  [[ -f "${REPO_ROOT}/go.mod" ]] || die "no go.mod under ${REPO_ROOT}; run this from a source checkout or pass --from DIR"

  BIN_SRC="$(mktemp -d)"
  # Remove the build directory on exit, whatever the outcome.
  trap 'rm -rf "$BIN_SRC"' EXIT

  run env CGO_ENABLED=0 go build -C "$REPO_ROOT" -trimpath -o "${BIN_SRC}/infrapilot" ./cmd/infrapilot
  run env CGO_ENABLED=0 go build -C "$REPO_ROOT" -trimpath -o "${BIN_SRC}/infrapilot-agent" ./cmd/infrapilot-agent
  run env CGO_ENABLED=0 go build -C "$REPO_ROOT" -trimpath -o "${BIN_SRC}/infrapilot-web" ./cmd/infrapilot-web
  ok "built infrapilot, infrapilot-agent and infrapilot-web"
}

create_account() {
  log "Creating the service account"

  if getent group "$SERVICE_GROUP" >/dev/null; then
    ok "group ${SERVICE_GROUP} already exists"
  else
    run groupadd --system "$SERVICE_GROUP"
    ok "created group ${SERVICE_GROUP}"
  fi

  if getent passwd "$SERVICE_USER" >/dev/null; then
    ok "user ${SERVICE_USER} already exists"
  else
    # A system account with no login shell and no home directory: it exists to
    # own files and run one service, not to be logged into.
    run useradd --system \
      --gid "$SERVICE_GROUP" \
      --home-dir "$DATA_DIR" \
      --no-create-home \
      --shell /usr/sbin/nologin \
      --comment "InfraPilot Agent" \
      "$SERVICE_USER"
    ok "created user ${SERVICE_USER} (system account, no login)"
  fi
}

install_binaries() {
  log "Installing binaries into ${PREFIX}/bin"

  run install -d -m "$BIN_MODE" "${PREFIX}/bin"
  # Owned by root and not writable by the service account: the Agent must not
  # be able to modify the binary it runs.
  run install -m "$BIN_MODE" -o root -g root "${BIN_SRC}/infrapilot"       "${PREFIX}/bin/infrapilot"
  run install -m "$BIN_MODE" -o root -g root "${BIN_SRC}/infrapilot-agent" "${PREFIX}/bin/infrapilot-agent"
  run install -m "$BIN_MODE" -o root -g root "${BIN_SRC}/infrapilot-web" "${PREFIX}/bin/infrapilot-web"
  if (( DRY_RUN )); then
    printf '  would write: %s/install.json\n' "$CONFIG_DIR"
  else
    install -d -m "$DIR_MODE" -o root -g "$SERVICE_GROUP" "$CONFIG_DIR"
    local metadata
    metadata="$(mktemp)"
    printf '{"method":"binary","prefix":"%s","binary_dir":"%s/bin","services":["%s.service","%s.service"]}\n' "$PREFIX" "$PREFIX" "$SERVICE_NAME" "$WEB_SERVICE_NAME" > "$metadata"
    install -m 0644 -o root -g root "$metadata" "$CONFIG_DIR/install.json"
    rm -f "$metadata"
  fi
  ok "installed infrapilot, infrapilot-agent and infrapilot-web"
}

create_directories() {
  log "Creating directories"

  # systemd also provisions these via StateDirectory and ConfigurationDirectory,
  # but creating them here means the permissions are correct before the service
  # ever starts, and that --no-start leaves a complete installation.
  run install -d -m "$DIR_MODE" -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$DATA_DIR"
  ok "${DATA_DIR} (mode $(printf '%04o' "$DIR_MODE")), owned by ${SERVICE_USER}"

  # Configuration is owned by root: an operator edits it, and the Agent only
  # reads it. A compromised Agent must not be able to rewrite its own settings.
  run install -d -m "$DIR_MODE" -o root -g "$SERVICE_GROUP" "$CONFIG_DIR"
  ok "${CONFIG_DIR} (mode $(printf '%04o' "$DIR_MODE")), owned by root"
}

# install_config writes a starter configuration file, and never replaces one.
install_config() {
  log "Installing configuration"

  local target="${CONFIG_DIR}/config.yaml"
  if [[ -e "$target" ]]; then
    local backup="${target}.backup.$(date -u +%Y%m%dT%H%M%SZ)"
    run cp -p "$target" "$backup"
    ok "backed up existing configuration to ${backup}"
    ok "${target} already exists; leaving it untouched"
    return
  fi

  local sample="${REPO_ROOT}/installer/config.sample.yaml"
  if [[ ! -f "$sample" ]]; then
    # Defaults are a complete configuration, so a missing sample is not fatal.
    warn "no sample configuration at ${sample}; the Agent will run on defaults"
    return
  fi

  run install -m "$CONFIG_MODE" -o root -g "$SERVICE_GROUP" "$sample" "$target"
  ok "${target} (mode $(printf '%04o' "$CONFIG_MODE")), readable by ${SERVICE_GROUP}"
}

install_service() {
  log "Installing the systemd service"

  local unit="${REPO_ROOT}/installer/systemd/${SERVICE_NAME}.service"
  [[ -f "$unit" ]] || die "unit file not found: $unit"

  # The unit hardcodes /usr/local/bin. Under a different --prefix the ExecStart
  # path is rewritten, because a unit pointing at a binary that is not there
  # fails at start with a message that does not name the cause.
  if [[ "$PREFIX" == "/usr/local" ]]; then
    run install -m 0644 -o root -g root "$unit" "$UNIT_PATH"
  elif (( DRY_RUN )); then
    printf '  would install: %s -> %s (ExecStart rewritten for prefix %s)\n' "$unit" "$UNIT_PATH" "$PREFIX"
  else
    local rendered
    rendered="$(mktemp)"
    sed "s|^ExecStart=/usr/local/bin/${SERVICE_NAME}$|ExecStart=${PREFIX}/bin/${SERVICE_NAME}|" "$unit" > "$rendered"
    grep -q "^ExecStart=${PREFIX}/bin/${SERVICE_NAME}$" "$rendered" \
      || { rm -f "$rendered"; die "could not set ExecStart for prefix ${PREFIX}"; }
    install -m 0644 -o root -g root "$rendered" "$UNIT_PATH"
    rm -f "$rendered"
  fi
  ok "${UNIT_PATH}"

  run systemctl daemon-reload
  run systemctl enable "$SERVICE_NAME"
  local web_unit="${REPO_ROOT}/installer/systemd/${WEB_SERVICE_NAME}.service"
  [[ -f "$web_unit" ]] || die "unit file not found: $web_unit"
  if [[ "$PREFIX" == "/usr/local" ]]; then
    run install -m 0644 -o root -g root "$web_unit" "/etc/systemd/system/${WEB_SERVICE_NAME}.service"
  elif (( DRY_RUN )); then
    printf '  would install: %s -> /etc/systemd/system/%s.service\n' "$web_unit" "$WEB_SERVICE_NAME"
  else
    sed "s|^ExecStart=/usr/local/bin/infrapilot-web$|ExecStart=${PREFIX}/bin/infrapilot-web|" "$web_unit" > "${web_unit}.tmp"
    run install -m 0644 -o root -g root "${web_unit}.tmp" "/etc/systemd/system/${WEB_SERVICE_NAME}.service"
    rm -f "${web_unit}.tmp"
  fi
  run systemctl enable "$WEB_SERVICE_NAME"
  ok "service enabled; it will start on boot"
}

start_service() {
  if (( NO_START )); then
    log "Skipping start (--no-start)"
    ok "start it later with: systemctl start ${SERVICE_NAME}"
    return
  fi

  log "Starting the service"
  run systemctl restart "$SERVICE_NAME"

  if (( DRY_RUN )); then
    return
  fi

  # Give the Agent a moment to open its database and record liveness before
  # the verification step asks whether it is running.
  sleep 2
}

# --- Verification ----------------------------------------------------------

verify() {
  log "Verifying the installation"

  if (( DRY_RUN )); then
    ok "dry run: nothing to verify"
    return 0
  fi

  local failures=0

  local binary
  for binary in infrapilot infrapilot-agent infrapilot-web; do
    if [[ -x "${PREFIX}/bin/${binary}" ]]; then
      ok "${PREFIX}/bin/${binary} is installed"
    else
      warn "${PREFIX}/bin/${binary} is missing or not executable"
      (( failures++ )) || true
    fi
  done

  if [[ -d "$DATA_DIR" ]]; then
    ok "${DATA_DIR} exists (mode $(stat -c '%a' "$DATA_DIR"), owner $(stat -c '%U' "$DATA_DIR"))"
  else
    warn "${DATA_DIR} is missing"
    (( failures++ )) || true
  fi

  if (( ! NO_START )); then
    if systemctl is-active --quiet "$SERVICE_NAME"; then
      ok "${SERVICE_NAME} is running"
    else
      warn "${SERVICE_NAME} is not running; see: journalctl -u ${SERVICE_NAME} -n 50"
      (( failures++ )) || true
    fi
  fi

  # The real check: the CLI's own diagnostics. If doctor is happy, the
  # installation works from the operator's point of view rather than merely
  # having the right files in the right places.
  if [[ -x "${PREFIX}/bin/infrapilot" ]]; then
    log "Running infrapilot doctor"
    if "${PREFIX}/bin/infrapilot" doctor; then
      ok "diagnostics passed"
    else
      warn "diagnostics reported problems; see the report above"
      (( failures++ )) || true
    fi
  fi

  if (( failures )); then
    die "${failures} verification step(s) failed"
  fi
}

# --- Uninstall -------------------------------------------------------------

uninstall() {
  log "Uninstalling InfraPilot"

  if systemctl list-unit-files "${SERVICE_NAME}.service" >/dev/null 2>&1; then
    run systemctl disable --now "$SERVICE_NAME" "$WEB_SERVICE_NAME" || true
    ok "service stopped and disabled"
  fi

  run rm -f "$UNIT_PATH" "/etc/systemd/system/${WEB_SERVICE_NAME}.service"
  run systemctl daemon-reload
  ok "unit removed"

  run rm -f "${PREFIX}/bin/infrapilot" "${PREFIX}/bin/infrapilot-agent" "${PREFIX}/bin/infrapilot-web"
  ok "binaries removed"

  if getent passwd "$SERVICE_USER" >/dev/null; then
    run userdel "$SERVICE_USER" || warn "could not remove the ${SERVICE_USER} account"
    ok "service account removed"
  fi

  # Data and configuration are deliberately left in place. Losing a database to
  # an uninstall is not recoverable, so removing it is the operator's decision.
  log "Data and configuration were kept"
  printf '  %s and %s were not touched.\n' "$DATA_DIR" "$CONFIG_DIR"
  printf '  Remove them by hand if you are sure: rm -rf %s %s\n' "$DATA_DIR" "$CONFIG_DIR"
}

# --- Entry point -----------------------------------------------------------

main() {
  parse_args "$@"
  require_root

  if (( UNINSTALL )); then
    uninstall
    log "Uninstall complete"
    return 0
  fi

  detect_os
  detect_arch
  require_systemd
  resolve_binaries
  create_account
  install_binaries
  create_directories
  install_config
  install_service
  start_service
  verify

  log "InfraPilot is installed"
  printf '\n  Status:  infrapilot status\n'
  printf '  Check:   infrapilot doctor\n'
  printf '  Logs:    journalctl -u %s -f\n' "$SERVICE_NAME"
  printf '  Config:  %s/config.yaml\n\n' "$CONFIG_DIR"
}

main "$@"
