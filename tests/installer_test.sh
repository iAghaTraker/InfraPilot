#!/usr/bin/env bash
#
# Tests for installer/install.sh.
#
# These run unprivileged and change nothing on the host: every case exercises
# either an argument-handling path or --dry-run. Anything needing root belongs
# in a container, not in a test an operator might run on a live machine.
#
# Usage: tests/installer_test.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
readonly INSTALLER="${REPO_ROOT}/installer/install.sh"

PASS=0
FAIL=0

pass() { printf '  \033[0;32mPASS\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
fail() { printf '  \033[0;31mFAIL\033[0m %s\n' "$1"; printf '       %s\n' "${2:-}"; FAIL=$((FAIL + 1)); }

# expect_exit runs the installer and checks its exit status.
expect_exit() {
  local name="$1" want="$2"; shift 2
  local output status
  output="$("$INSTALLER" "$@" 2>&1)"
  status=$?
  if [[ "$status" -eq "$want" ]]; then
    pass "$name"
  else
    fail "$name" "exit ${status}, want ${want}: ${output}"
  fi
}

# expect_output runs the installer and checks that its output contains a string.
expect_output() {
  local name="$1" want="$2"; shift 2
  local output
  output="$("$INSTALLER" "$@" 2>&1)"
  if [[ "$output" == *"$want"* ]]; then
    pass "$name"
  else
    fail "$name" "output does not contain ${want}"
  fi
}

# refute_output fails when the output contains a string.
refute_output() {
  local name="$1" unwanted="$2"; shift 2
  local output
  output="$("$INSTALLER" "$@" 2>&1)"
  if [[ "$output" != *"$unwanted"* ]]; then
    pass "$name"
  else
    fail "$name" "output unexpectedly contains ${unwanted}"
  fi
}

printf '\033[0;34m==>\033[0m Installer\n'

[[ -f "$INSTALLER" ]] || { fail "the installer exists" "not found: $INSTALLER"; exit 1; }
pass "the installer exists"

if [[ -x "$INSTALLER" ]]; then
  pass "the installer is executable"
else
  fail "the installer is executable" "chmod +x ${INSTALLER}"
fi

if bash -n "$INSTALLER" 2>/dev/null; then
  pass "the installer parses"
else
  fail "the installer parses" "$(bash -n "$INSTALLER" 2>&1)"
fi

# A script that manipulates system state must abort on an unhandled error
# rather than continuing with a half-built installation.
if grep -qE '^set -euo pipefail' "$INSTALLER"; then
  pass "the installer sets a strict shell mode"
else
  fail "the installer sets a strict shell mode" "expected: set -euo pipefail"
fi

printf '\033[0;34m==>\033[0m Argument handling\n'

expect_exit  "--help succeeds"                  0 --help
expect_output "--help documents the options"    "--dry-run" --help
expect_exit  "an unknown option is rejected"    1 --bogus
expect_exit  "--prefix requires a value"        1 --prefix
expect_exit  "--from requires a value"          1 --from
expect_exit  "a relative --prefix is rejected"  1 --prefix relative
expect_exit  "a missing --from is rejected"     1 --from /nonexistent-directory
expect_output "a relative --prefix explains itself" "absolute" --prefix relative

printf '\033[0;34m==>\033[0m Dry run\n'

expect_exit   "--dry-run succeeds"                   0 --dry-run
expect_output "--dry-run does not require root"      "Detecting the operating system" --dry-run
expect_output "--dry-run reports the service user"   "infrapilot" --dry-run
expect_output "--dry-run creates the data directory" "/var/lib/infrapilot" --dry-run
expect_output "--dry-run installs the unit"          "/etc/systemd/system/infrapilot-agent.service" --dry-run

# Nothing may actually run: every state-changing command must be announced.
refute_output "--dry-run does not run systemctl"     "Failed to" --dry-run
expect_output "--dry-run announces daemon-reload"    "would run: systemctl daemon-reload" --dry-run
expect_output "--dry-run announces useradd"          "would run: useradd" --dry-run

# The documented permissions are part of the security contract.
expect_output "the data directory is 0750"           "mode 0750" --dry-run
expect_output "configuration is 0640"                "mode 0640" --dry-run

expect_output "--no-start skips starting"            "Skipping start" --dry-run --no-start
expect_output "--prefix redirects the binaries"      "/opt/ip/bin/infrapilot" --dry-run --prefix /opt/ip
expect_output "--prefix rewrites ExecStart"          "ExecStart rewritten" --dry-run --prefix /opt/ip

printf '\033[0;34m==>\033[0m Uninstall\n'

expect_exit   "--uninstall succeeds"              0 --uninstall --dry-run
expect_output "--uninstall keeps data"            "were not touched" --uninstall --dry-run
expect_output "--uninstall removes the unit"      "unit removed" --uninstall --dry-run
refute_output "--uninstall does not remove data"  "would run: rm -rf /var/lib/infrapilot" --uninstall --dry-run

printf '\033[0;34m==>\033[0m Systemd unit\n'

UNIT="${REPO_ROOT}/installer/systemd/infrapilot-agent.service"

if [[ -f "$UNIT" ]]; then
  pass "the unit file exists"

  # Boolean hardening directives. systemd treats yes/true/1/on as equivalent,
  # so any of them is accepted: the test asserts the protection is on, not the
  # spelling used to turn it on.
  for directive in \
    "NoNewPrivileges" \
    "ProtectHome" \
    "PrivateTmp" \
    "PrivateDevices" \
    "ProtectKernelTunables" \
    "ProtectKernelModules" \
    "RestrictSUIDSGID" \
    "LockPersonality" \
    "MemoryDenyWriteExecute"
  do
    if grep -qiE "^${directive}=(yes|true|1|on)$" "$UNIT"; then
      pass "the unit enables ${directive}"
    else
      fail "the unit enables ${directive}" "not enabled: ${directive}"
    fi
  done

  # Directives whose value is not a boolean, checked for presence.
  for directive in \
    "User=infrapilot" \
    "ProtectSystem=strict" \
    "StateDirectory=infrapilot" \
    "RestrictAddressFamilies=" \
    "SystemCallFilter=" \
    "CapabilityBoundingSet="
  do
    if grep -q "^${directive}" "$UNIT"; then
      pass "the unit sets ${directive%%=*}"
    else
      fail "the unit sets ${directive%%=*}" "missing: ${directive}"
    fi
  done

  # The Agent must never run as root.
  if grep -qE '^User=root' "$UNIT"; then
    fail "the unit does not run as root" "User=root found"
  else
    pass "the unit does not run as root"
  fi

  # systemd ignores start-limit keys in [Service]; they only work in [Unit].
  if awk '/^\[Service\]/{s=1} /^\[Install\]/{s=0} s && /^StartLimit/{found=1} END{exit !found}' "$UNIT"; then
    fail "start-limit keys are in [Unit]" "StartLimit* found in [Service], where systemd ignores it"
  else
    pass "start-limit keys are in [Unit]"
  fi

  if command -v systemd-analyze >/dev/null 2>&1; then
    # Only the unit's own diagnostics matter here; unrelated host units and the
    # absence of the not-yet-installed binary are not this test's business.
    analysis="$(systemd-analyze verify "$UNIT" 2>&1 | grep "infrapilot-agent.service" | grep -v "is not executable")"
    if [[ -z "$analysis" ]]; then
      pass "systemd-analyze reports no problems"
    else
      fail "systemd-analyze reports no problems" "$analysis"
    fi
  else
    printf '  \033[0;33mSKIP\033[0m systemd-analyze is not available\n'
  fi
else
  fail "the unit file exists" "not found: $UNIT"
fi

printf '\033[0;34m==>\033[0m Sample configuration\n'

SAMPLE="${REPO_ROOT}/installer/config.sample.yaml"

if [[ -f "$SAMPLE" ]]; then
  pass "the sample configuration exists"

  # A sample carrying a credential would be copied into production verbatim.
  if grep -qiE '^[[:space:]]*[a-z_]*(password|secret|token|api_key|private_key)[[:space:]]*:[[:space:]]*[^[:space:]]' "$SAMPLE"; then
    fail "the sample holds no credentials" "a credential-like value is set"
  else
    pass "the sample holds no credentials"
  fi

  if grep -q "^version:" "$SAMPLE"; then
    pass "the sample declares a schema version"
  else
    fail "the sample declares a schema version" "no version key"
  fi
else
  fail "the sample configuration exists" "not found: ${SAMPLE} (go test ./internal/config -update)"
fi

printf '\n'
if (( FAIL )); then
  printf '\033[0;31m%d failed\033[0m, %d passed\n' "$FAIL" "$PASS"
  exit 1
fi
printf '\033[0;32mall %d checks passed\033[0m\n' "$PASS"
