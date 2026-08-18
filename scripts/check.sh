#!/usr/bin/env bash
#
# Run every check CI runs, locally.
#
# CI is the authority, but discovering a formatting failure after pushing wastes
# a round trip. This runs the same commands in the same order, so a green run
# here means a green pipeline.
#
# Usage:
#   scripts/check.sh          # everything available on this host
#   scripts/check.sh --quick  # skip the race detector and the vulnerability scan
#
# Optional tools (shellcheck, systemd-analyze, govulncheck) are skipped with a
# notice when absent rather than failing: a contributor without them can still
# use this script, and CI will run them anyway.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
cd "$REPO_ROOT" || exit 1

QUICK=0
for arg in "$@"; do
  case "$arg" in
    --quick) QUICK=1 ;;
    -h|--help) sed -n '3,16p' "${BASH_SOURCE[0]}" | sed 's/^#\{1,\} \{0,1\}//'; exit 0 ;;
    *) printf 'unknown option: %s (try --help)\n' "$arg" >&2; exit 1 ;;
  esac
done

FAILED=()

step() { printf '\n\033[0;34m==>\033[0m %s\n' "$1"; }
ok()   { printf '\033[0;32m  ok\033[0m %s\n' "$1"; }
bad()  { printf '\033[0;31m  fail\033[0m %s\n' "$1"; FAILED+=("$1"); }
skip() { printf '\033[0;33m  skip\033[0m %s\n' "$1"; }

# run executes a command, recording a failure without aborting, so one problem
# does not hide the rest.
run() {
  local name="$1"; shift
  if "$@"; then ok "$name"; else bad "$name"; fi
}

step "Formatting"
unformatted="$(gofmt -l ./cmd ./internal ./pkg)"
if [[ -z "$unformatted" ]]; then
  ok "gofmt"
else
  bad "gofmt (run: gofmt -w ./cmd ./internal ./pkg)"
  printf '%s\n' "$unformatted"
fi

step "Dependencies"
# go.mod and go.sum must already describe the build. Compare copies rather than
# trusting that tidy is a no-op.
cp go.mod go.mod.check && cp go.sum go.sum.check
if go mod tidy 2>/dev/null && diff -q go.mod go.mod.check >/dev/null && diff -q go.sum go.sum.check >/dev/null; then
  ok "go mod tidy leaves go.mod and go.sum unchanged"
else
  bad "go mod tidy changes go.mod or go.sum"
  # Restore what was committed, so a check does not silently rewrite the tree.
  mv go.mod.check go.mod && mv go.sum.check go.sum
fi
rm -f go.mod.check go.sum.check

step "Vet"
run "go vet" go vet ./...

step "Build"
run "go build" go build ./...
run "arm64 cross-compile" env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...

step "Tests"
run "go test" go test ./... -count=1

if (( QUICK )); then
  skip "race detector (--quick)"
else
  step "Race detector"
  run "go test -race" go test ./... -count=1 -race
fi

step "Shell scripts"
if command -v shellcheck >/dev/null 2>&1; then
  run "shellcheck" shellcheck installer/install.sh tests/installer_test.sh scripts/check.sh
else
  skip "shellcheck is not installed"
fi

step "Installer"
run "installer tests" tests/installer_test.sh

step "systemd unit"
UNIT="installer/systemd/infrapilot-agent.service"
if command -v systemd-analyze >/dev/null 2>&1; then
  # Only the unit's own diagnostics matter; the binary is not installed here,
  # so its absence is expected rather than a problem with the unit.
  analysis="$(systemd-analyze verify "$UNIT" 2>&1 | grep "infrapilot-agent.service" | grep -v "is not executable")"
  if [[ -z "$analysis" ]]; then
    ok "systemd-analyze verify"
  else
    bad "systemd-analyze verify"
    printf '%s\n' "$analysis"
  fi
else
  skip "systemd-analyze is not available"
fi

if (( QUICK )); then
  skip "govulncheck (--quick)"
else
  step "Vulnerabilities"
  if command -v govulncheck >/dev/null 2>&1; then
    run "govulncheck" govulncheck ./...
  else
    skip "govulncheck is not installed (go install golang.org/x/vuln/cmd/govulncheck@latest)"
  fi
fi

printf '\n'
if (( ${#FAILED[@]} )); then
  printf '\033[0;31m%d check(s) failed:\033[0m\n' "${#FAILED[@]}"
  printf '  %s\n' "${FAILED[@]}"
  exit 1
fi
printf '\033[0;32mall checks passed\033[0m\n'
