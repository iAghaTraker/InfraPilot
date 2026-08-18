# Contributing to InfraPilot

InfraPilot is an open-source project and contributions are welcome. This
document describes how to build it, what the checks are, and the rules a change
has to respect.

InfraPilot is at v0.1.0 — the foundation. Please read
[ROADMAP.md](ROADMAP.md) before proposing a feature: much of what InfraPilot
will eventually do is deliberately not implemented yet, and a pull request
that adds a v0.4 feature to v0.1 will be asked to wait rather than merged.

## Development setup

You need:

- **Go 1.25 or newer.** The module targets 1.25; CI pins the same version.
- **Linux.** The Agent reads `/proc` and integrates with systemd, so Agent
  work needs Linux. The CLI and the pure-Go packages build elsewhere, but the
  tests assume Linux paths.
- **Git.**

No cgo toolchain is required. SQLite comes from `modernc.org/sqlite`, a pure-Go
implementation, so `CGO_ENABLED=0` builds work and cross-compilation is just a
matter of setting `GOOS` and `GOARCH`.

```sh
git clone https://github.com/iAghaTraker/InfraPilot.git
cd infrapilot
go build ./...
go test ./...
```

### Running it without installing

Development mode moves configuration and state into user-local directories, so
you never need root to try a change:

```sh
export INFRAPILOT_DEV=1
go run ./cmd/infrapilot doctor
go run ./cmd/infrapilot-agent
```

Paths resolve under `$XDG_CONFIG_HOME/infrapilot` and
`$XDG_DATA_HOME/infrapilot`. See [docs/configuration.md](docs/configuration.md).

## The checks

Run these before opening a pull request. They are exactly what CI runs, so a
green local run means a green pipeline.

```sh
scripts/check.sh
```

That runs everything below in order and reports each result. Tools you do not
have installed (shellcheck, systemd-analyze, govulncheck) are skipped with a
notice rather than failing; CI runs them regardless. `scripts/check.sh --quick`
skips the race detector and the vulnerability scan for a faster inner loop.

To run them individually:

```sh
gofmt -l ./cmd ./internal ./pkg   # must print nothing
go vet ./...
go build ./...
go test ./... -count=1
go test ./... -count=1 -race
go mod tidy                        # must leave go.mod and go.sum unchanged
tests/installer_test.sh            # when you touch the installer or the unit
```

`-count=1` disables the test cache, so a pass means the tests actually ran.
The race detector is not optional: the Agent is a concurrent lifecycle and it is
the check that catches the bugs that package is prone to.

### Formatting

Standard `gofmt`. No alternative formatter, no configuration. CI reports
unformatted files rather than rewriting your branch:

```sh
gofmt -w ./cmd ./internal ./pkg
```

Shell scripts must pass `shellcheck` with no findings.

## Architecture rules

These are not style preferences. A change that breaks one of them will be sent
back regardless of how well it is written.

**The Agent is the source of truth.** The CLI, and later the Web Panel and
Desktop App, are clients. A client never inspects the filesystem, the database
or the process table on its own — it asks the Agent, or it calls the same
`internal/core` code the Agent uses. If you find yourself adding an
`os.Stat` to `internal/cli`, the logic belongs in `internal/core`.

**`internal/core` holds the operations.** `internal/cli` renders them. Keeping
rendering out of core is what lets a future Web Panel reuse the same code
without reimplementing behaviour.

**No feature configuration without a feature.** `internal/config` carries only
fields v0.1.0 actually uses. A setting that silently does nothing is worse than
no setting, because it looks like a promise.

**Never ignore an error.** No bare `_ =` on a call that can fail, no empty
`if err != nil `. Either handle it, wrap it with context, or explain in a
comment why discarding it is correct.

**Wrap errors with an operation.** `internal/errors` classifies errors by
`Kind` and carries an `op` string. Use `errors.Wrap`/`errors.Wrapf` at package
boundaries so a failure reports where it came from and maps to the right exit
code.

**Comments explain why, not what.** The code already says what it does. A
comment earns its place by recording a decision, a constraint or a trap.

**Tests come with the change.** A new behaviour needs a test; a bug fix needs a
regression test. `go test ./...` must pass before and after.

## Security expectations

Read [SECURITY.md](SECURITY.md) for reporting, and
[docs/security.md](docs/security.md) for the properties the code holds.

For a contribution specifically:

- **No secrets in logs.** `internal/logging` redacts known-sensitive keys, but
  do not rely on it — do not log the value in the first place.
- **No secrets in error messages.** An error explains what failed, not what the
  credential was.
- **No credentials in the repository.** No default password, no token, no key,
  not even in a test fixture or a sample configuration file.
- **Validate paths.** Anything that builds a path from configuration or, later,
  from a client must go through `system.ResolveInDir`.
- **No shell string interpolation.** Build argument arrays; never assemble a
  command line from values that came from outside the program.
- **Respect the permission constants.** `system.DirMode` (0750),
  `system.ConfigMode` (0640) and `system.PrivateFileMode` (0600). Nothing
  InfraPilot creates may be world-readable.
- **No network listener in v0.1.** The Agent does not open a port. Adding one
  is a v0.3 design question, not a pull request.
- **The Agent stays unprivileged.** It must not require root, and it must not
  be able to write its own binary or configuration.

If a change has a security implication, say so in the pull request description.
Flagging it is not an admission that the change is wrong; it is what lets it be
reviewed properly.

## Commits

Write commits that explain themselves. The subject line says what changed, the
body says why:

```
Move start-limit keys into [Unit]

systemd ignores StartLimitIntervalSec and StartLimitBurst in [Service],
so the crash-loop guard was never applied. systemd-analyze reports this
as an unknown key.
```

- Present tense, imperative subject ("Add", "Fix", "Move"), no trailing period.
- Keep the subject under about 72 characters.
- One logical change per commit. A refactor and a bug fix are two commits.
- Explain *why* in the body when it is not obvious. What changed is visible in
  the diff; the reasoning is not.

No commit-message convention is enforced by tooling. Conventional Commits
prefixes are accepted but not required.

## Pull requests

1. Branch from `main`.
2. Make the change, with tests.
3. Run the checks above.
4. Open the pull request against `main`.

In the description, cover:

- **What** the change does.
- **Why** it is needed. Link an issue if one exists.
- **How you tested it** — the commands you ran, not just "tests pass".
- **Anything you did not do**, and why. A known gap stated plainly is fine; a
  known gap left unmentioned is not.

What to expect:

- CI must be green. It runs the same checks you ran locally, plus shellcheck,
  `systemd-analyze verify`, an arm64 cross-compile and `govulncheck`.
- Review comments are about the code. Please do not read them as a verdict on
  the contribution.
- A large or architectural change is easier to land if you open an issue first.
  Agreement on the approach before the code is written saves rewriting it.

Do not include unrelated changes: no drive-by reformatting, no dependency
bumps bundled with a feature, no "while I was in there" cleanups. They make a
change harder to review and harder to revert.

## Reporting bugs

Include the InfraPilot version (`infrapilot version`), the OS and architecture,
what you expected, what happened, and how to reproduce it. `infrapilot doctor`
output is usually the fastest way to describe the state of an installation —
it is designed not to print secrets, but read it before pasting it.

For anything with a security impact, use the process in
[SECURITY.md](SECURITY.md) instead of a public issue.

## Licence

InfraPilot is MIT licensed. By contributing you agree that your contribution is
licensed under the same terms. See [LICENSE](LICENSE).
