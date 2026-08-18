# Architecture

How InfraPilot v0.1.0 is put together, and the reasoning behind the parts that
were decisions rather than defaults.

This describes what exists. Where a choice was made to accommodate a later
version, that is stated as such — not as a feature.

## Agent-first

InfraPilot has one rule that everything else follows from:

**The Agent owns all infrastructure state. Everything else is a client.**

```
   CLI            Web Panel (v0.4)      Desktop App (v0.11)
    │                    │                      │
    └────────────────────┼──────────────────────┘
                         ▼
                 InfraPilot Agent
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
   Configuration      SQLite         Host state
```

A client never inspects the filesystem, the database or the process table on
its own initiative. It asks the Agent, or — in v0.1.0, where there is no
transport yet — it calls the same `internal/core` code the Agent uses.

This matters now because it is what makes v0.4 and v0.11 possible without a
rewrite. If the CLI grew its own `os.Stat` calls and its own notion of what
"healthy" means, then a Web Panel would either duplicate that logic or disagree
with it. Both outcomes are worse than the constraint.

The corollary: **the core platform does not depend on a centralised InfraPilot
cloud.** A host with no internet connection is fully manageable.

## What v0.1.0 actually contains

```
cmd/infrapilot/           CLI entry point (thin)
cmd/infrapilot-agent/     Agent entry point (thin)

internal/agent/           Agent lifecycle: start-up, heartbeat, shutdown
internal/cli/             Command dispatch and rendering
internal/core/            Agent-side operations: status, doctor
internal/config/          Layered configuration and validation
internal/storage/         SQLite: open, migrate, health
internal/logging/         Structured logging and redaction
internal/errors/          Typed errors and exit codes
internal/system/          Host inspection, paths, permissions, process liveness

pkg/version/              Build identity — public because the SDK will need it

installer/                Install script, systemd unit, sample configuration
tests/                    Installer test suite
scripts/                  Developer tooling
docs/                     This documentation

web/ desktop/ modules/ sdk/   Placeholders with a README explaining the plan
```

Both `main` functions are deliberately thin: they wire configuration, logging
and one subsystem together, then translate a failure into an exit code. Anything
worth testing lives in a package, because a `main` function is the one place a
test cannot reach.

### Deviation from the suggested structure

The specification sketched top-level `agent/` and `cli/` directories. This
repository puts the binaries in `cmd/` and the implementations in
`internal/agent` and `internal/cli`.

The reason: `cmd/<binary>` is the Go convention that build tooling, `go
install` and every Go developer already expects, and `internal/` makes the
compiler enforce that nothing outside this module imports the implementation.
That enforcement is worth having on a project whose public surface — `pkg/` and
eventually an SDK — is meant to be a deliberate, small subset. A top-level
`agent/` package would be importable by anyone the moment the repository is
public, which would make every internal refactor a breaking change.

Everything else follows the suggested layout.

## Dependency direction

```
cmd/  ──────────────►  internal/agent, internal/cli
                              │
                              ▼
                        internal/core
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      internal/config  internal/storage  internal/system
              │               │               │
              └───────────────┼───────────────┘
                              ▼
                internal/logging, internal/errors
```

Dependencies point one way. `internal/errors` and `internal/logging` are the
foundation and import nothing from InfraPilot but each other; `internal/core`
composes the layers below it; `internal/cli` renders what core returns and
contains no operational logic.

The rule that keeps this honest: **`internal/core` decides, `internal/cli`
renders.** A future Web Panel calls the same `core.CollectStatus` and
`core.Diagnose`, and gets the same answers, without reimplementing anything.

## The two binaries

### `infrapilot-agent`

The long-running service. Under systemd it runs as the unprivileged
`infrapilot` account.

Its lifecycle, in `internal/agent`:

1. Validate the environment — refuse to continue if the host cannot support it.
2. Prepare the data directory, with explicit permissions.
3. Open the database and run migrations.
4. Write the PID file.
5. Run the heartbeat loop until the context is cancelled.
6. Shut down: stop the loop, checkpoint and close the database, remove the PID
   file — bounded by `agent.shutdown_timeout`.

Signals are handled with `signal.NotifyContext` for `SIGTERM` and `SIGINT`. A
stop that arrives *during* start-up exits cleanly rather than reporting the
interrupted step as a crash, because `Restart=on-failure` would otherwise fight
an operator who stopped the service at an inconvenient moment.

**The Agent opens no network listener.** Nothing binds a port or a socket. That
is a v0.1.0 requirement, not an omission: remote access arrives in v0.3 with
the pairing system, and building the foundation without a listener means the
eventual remote surface is added deliberately rather than inherited.

### `infrapilot`

The CLI. Three commands in v0.1.0: `version`, `status`, `doctor`.

Command dispatch is a registry — a slice of `{Name, Summary, Run}` — so adding
a command means adding one entry, and the help text cannot drift from what is
implemented because both come from the same list.

Two behaviours are load-bearing:

**Configuration failure is not fatal to the CLI.** A config load error is
carried in the environment struct rather than returned, so `status` and `doctor`
still run and report the problem. A diagnostic tool that refuses to start when
something is wrong is unavailable exactly when it is needed.

**`status` and `doctor` change nothing.** They open the database read-only
(`mode=ro`, which also prevents SQLite from creating the file), so asking a
question cannot migrate a schema or bring a database into existence.
`TestStatusCreatesNothing` asserts this.

## How the Agent's state is observed without a listener

`status` needs to report whether the Agent is running. With no socket and no
privileged interface, that takes three checks that must all agree:

1. Read the PID from `agent.pid` in the data directory.
2. Send signal 0 — a no-op delivery that reports whether the process exists.
3. Compare `/proc/<pid>/comm` against the expected command name.

The third step is what makes it correct. A stale PID file whose number has been
recycled by an unrelated process would otherwise report the Agent as running.
The kernel truncates `comm` to 15 characters, so the comparison is a prefix
match against `infrapilot-agent`.

The PID file is treated as a hint requiring corroboration, never as authority.
A corrupt or hand-edited file cannot make `status` lie.

## Configuration

Three layers, each overriding the last: defaults → YAML file → environment.
The result is validated before anything starts, and validation reports every
problem at once rather than one per run.

Two decisions worth naming:

**Only fields v0.1.0 uses exist.** Adding settings for planned features would
create configuration that silently does nothing, which is worse than no
configuration — it reads as a promise.

**Unknown keys are an error.** Silently ignoring one means an operator believes
a setting took effect when it did not, and a typo in a security setting would
fail open.

The on-disk shape (`fileConfig`) is separate from the validated in-memory
`Config`, with every field a pointer so that "absent" is distinguishable from
"set to the zero value". Without that, a file omitting `logging.level` would
reset it rather than inherit the default.

Details: [configuration.md](configuration.md).

## Storage

SQLite through `modernc.org/sqlite` — a pure-Go implementation, so no cgo, no
system SQLite in the trust boundary, and cross-compilation is just `GOOS` and
`GOARCH`.

- WAL journal mode, **verified after opening** rather than assumed. A
  filesystem that silently refuses WAL is reported.
- `foreign_keys=ON`, also verified.
- Versioned migrations recorded in `schema_migrations`, run only by the Agent.
- Read-only handles for observers.

v0.1.0 stores only the schema version. The migration machinery exists because
retrofitting migrations onto a database that already holds production data is
considerably harder than having them from the first release.

## Errors

`internal/errors` gives every error a `Kind` (usage, config, validation,
permission, not-found, storage, unsupported, internal) and an operation name.
The kind maps to an exit code: `0` success, `1` failure, `2` usage.

Errors are wrapped at package boundaries so a failure reports where it came
from. The package deliberately does **not** reimplement `Is` and `As` — callers
alias the standard library as `stderrors` — because a parallel error API that
almost matches the standard one is a trap for anyone who assumes it does.

Messages name what failed and which path was involved. They do not include the
value that failed, and they do not expose internal types: yaml.v3's
unknown-key error, for instance, is rewritten to name the setting and line
rather than printing the Go struct it was decoding into.

## Logging

`log/slog`, wrapped by a handler that redacts attribute values whose keys look
sensitive before anything is written — at every level, including `debug`.

Redaction is a safety net, not the strategy. The strategy is not to log the
value at all. A net catches the key you thought of.

Logs go to stderr, which under systemd means the journal.

## Doctor

A registry of independent checks — operating system, architecture,
configuration, data directory, database, logging, agent — each returning PASS,
WARN or FAIL with a message.

Independence is the design: one check panicking or failing must not prevent the
others from running, because the situation where you most need a diagnostic is
the one where something is already broken. `TestChecksAreIndependent` asserts
it.

**Warnings never fail the exit code.** A fresh installation that has not yet
started the Agent legitimately warns about a missing data directory and
database. If that exited non-zero, the installer would report a working install
as broken, and operators would learn to ignore the exit code.

## Testing

Every package has tests alongside it. Beyond the usual coverage, three kinds are
worth calling out because they encode requirements rather than behaviour:

- **Secret-leak tests.** `internal/cli/render_test.go` runs all three commands
  with `PASSWORD`, `AWS_SECRET_ACCESS_KEY` and a probe variable set, and fails
  if any value appears in the output. `internal/agent` does the same for the log.
- **Side-effect tests.** `TestStatusCreatesNothing` and
  `TestDataDirectoryCheckLeavesNoFiles` assert that observing changes nothing.
- **Contract tests.** `tests/installer_test.sh` asserts the documented
  permissions and every hardening directive in the systemd unit, so weakening
  one fails the suite instead of silently shipping.

The race detector is not optional: the Agent is a concurrent lifecycle, which is
the code most likely to have the bugs it finds.

## What is deliberately absent

Not "not done yet" — decided against for v0.1.0:

| Absent | Why |
| --- | --- |
| Network listener, remote API | v0.3. Building without one means the remote surface is added deliberately. |
| Pairing, keys, credentials | v0.3. No credential store means nothing to leak while the foundation is being built. |
| Web Panel, Desktop App | v0.4, v0.11. Both are clients of an Agent that has to be right first. |
| Service management, modules | v0.2, v0.7. Managing systemd units needs a privilege boundary that does not exist yet. |
| Backups, migration, monitoring | v0.9, v0.10, v0.14. |
| RBAC, public API, AI | v0.15, v0.16, v0.18. |

The placeholder directories (`web/`, `desktop/`, `modules/`, `sdk/`) hold a
README each and no code. They exist so the intended shape is visible, not to
suggest work has started.

## Constraints a change must respect

1. The Agent is the source of truth; clients do not inspect the host directly.
2. `internal/core` decides, `internal/cli` renders.
3. No configuration for a feature that does not exist.
4. No network listener in v0.1.
5. The Agent runs unprivileged and cannot write its own binary or configuration.
6. No secret in a log, an error message or command output.
7. Errors are handled or wrapped, never discarded silently.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full set, and
[security.md](security.md) for how the security ones are enforced.
