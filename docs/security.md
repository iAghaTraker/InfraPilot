# Security

How InfraPilot v0.1.0 is built to be attacked and survive it, and what it does
not defend against.

For reporting a vulnerability, see [SECURITY.md](../SECURITY.md).

## Threat model

InfraPilot manages infrastructure. An attacker who controls the Agent controls
the host, which makes the Agent a high-value target and the reason it holds as
little privilege as it can.

**Defended against in v0.1.0:**

- A compromised Agent process escalating to root, or persisting by rewriting
  its own binary or configuration.
- A local unprivileged user on the host reading InfraPilot's state, database or
  configuration.
- A malicious or mistaken value in the configuration file escaping the data
  directory.
- Credentials leaking through logs, error messages or command output.

**Not defended against:**

- **Root on the host.** Root can replace the binary, edit the unit and read
  every file. InfraPilot makes no claim to contain an attacker who already has
  root.
- **A remote attacker.** There is nothing to attack remotely: v0.1.0 opens no
  network listener. This changes in v0.3, which is when a remote threat model
  becomes meaningful.
- **A malicious module.** The module system does not exist yet.

## No network exposure

The Agent listens on no port, binds no socket and accepts no remote command.
`RestrictAddressFamilies=AF_UNIX` in the systemd unit permits only the local
socket family the journal needs, so a bug that tried to open a TCP listener
would be denied by the kernel rather than merely being absent from the code.

The CLI does not talk to the Agent over a socket either. It reads the same
files, through the same `internal/core` code, opening the database read-only.

Remote management arrives in v0.3 with the pairing system. Building v0.1.0
without a listener means the eventual remote surface is added deliberately, to
a foundation that never assumed one.

## Privilege

The Agent runs as `infrapilot`: a system account with no login shell
(`/usr/sbin/nologin`), no home directory of its own, and membership in no group
but its own.

It never runs as root. Operations that genuinely need privilege — managing
other services, in a later version — will be isolated behind an explicit
boundary rather than granted to the whole process.

The systemd unit drops what remains:

| Directive | Effect |
| --- | --- |
| `CapabilityBoundingSet=` (empty) | No capabilities at all |
| `AmbientCapabilities=` (empty) | None gained on exec |
| `NoNewPrivileges=true` | setuid and file capabilities cannot raise privilege |
| `ProtectSystem=strict` | The whole filesystem is read-only except `ReadWritePaths` |
| `ProtectHome=true` | `/home`, `/root` and `/run/user` are invisible |
| `PrivateTmp=true` | A private `/tmp`, so no shared-tmp attack |
| `PrivateDevices=true` | Only a minimal device set |
| `ProtectProc=invisible` | Other processes are not visible in `/proc` |
| `ProtectKernelTunables=true` | `/proc/sys` and `/sys` are read-only |
| `ProtectKernelModules=true` | Module loading is denied |
| `ProtectKernelLogs=true` | The kernel ring buffer is inaccessible |
| `ProtectControlGroups=true` | cgroups are read-only |
| `ProtectClock=true` | The system clock cannot be changed |
| `ProtectHostname=true` | The hostname cannot be changed |
| `RestrictNamespaces=true` | No namespace creation, so no container escape primitive |
| `RestrictSUIDSGID=true` | setuid and setgid bits cannot be set |
| `RestrictRealtime=true` | No realtime scheduling |
| `RestrictAddressFamilies=AF_UNIX` | No network sockets |
| `LockPersonality=true` | The execution domain is fixed |
| `MemoryDenyWriteExecute=true` | No writable-executable memory, blocking common shellcode |
| `SystemCallFilter=@system-service` plus a denylist | Privileged, mount, swap, reboot, raw-I/O, debug and cpu-emulation syscalls return `EPERM` |
| `SystemCallArchitectures=native` | No non-native syscall ABI |
| `RemoveIPC=true` | IPC objects are cleaned up on exit |
| `KeyringMode=private` | No shared kernel keyring |

`tests/installer_test.sh` asserts these are present, so removing one fails the
test suite rather than silently weakening a deployment. `systemd-analyze
verify` runs in CI against the same unit.

## Filesystem permissions

Nothing InfraPilot creates is world-readable. The modes are constants in
`internal/system/system.go` — `DirMode` 0750, `ConfigMode` 0640,
`PrivateFileMode` 0600 — so the code and the installer cannot disagree.

### Production layout

| Path | Owner | Mode | Why |
| --- | --- | --- | --- |
| `/usr/local/bin/infrapilot` | `root:root` | `0755` | Executable by all, writable only by root |
| `/usr/local/bin/infrapilot-agent` | `root:root` | `0755` | The Agent cannot rewrite the binary it runs |
| `/etc/infrapilot/` | `root:infrapilot` | `0750` | The Agent reads its configuration; only root edits it |
| `/etc/infrapilot/config.yaml` | `root:infrapilot` | `0640` | Group-readable so the Agent can read it, never writable by it |
| `/var/lib/infrapilot/` | `infrapilot:infrapilot` | `0750` | The only directory the Agent writes |
| `/var/lib/infrapilot/infrapilot.db` | `infrapilot:infrapilot` | `0600` | Private to the Agent |
| `/var/lib/infrapilot/agent.pid` | `infrapilot:infrapilot` | `0600` | Liveness marker |
| `/etc/systemd/system/infrapilot-agent.service` | `root:root` | `0644` | Read by systemd |

The asymmetry is the point: **configuration is root-owned and the Agent can
only read it; state is Agent-owned and root does not need to touch it.** A
compromised Agent cannot change its own settings to widen its own permissions,
and cannot replace its binary to survive a restart.

Directories are created with an explicit `os.Chmod` after `MkdirAll`, because
`MkdirAll` applies the process umask and a umask of 027 or 022 would turn 0750
into 0700 or 0755. Relying on the umask would make the resulting mode depend on
the environment the installer happened to run in.

`ReadWritePaths=/var/lib/infrapilot` in the unit means that even if a bug tried
to write elsewhere, `ProtectSystem=strict` denies it at the kernel level.

### Development mode

With `INFRAPILOT_DEV=1`, paths move under `$XDG_CONFIG_HOME/infrapilot` and
`$XDG_DATA_HOME/infrapilot`. The same modes apply. Development mode needs no
root, which means nobody has to run a work-in-progress Agent as root to try it.

### Verifying

```sh
ls -ld /etc/infrapilot /var/lib/infrapilot
ls -l  /etc/infrapilot/config.yaml /var/lib/infrapilot/infrapilot.db
infrapilot doctor
```

`doctor` checks the data directory and database permissions and reports a
warning when they are wider than they should be.

## Secrets

v0.1.0 stores no credentials: there is no password, no token, no key and no
account. That is not a reason to be careless, because v0.3 will introduce
keys, and the logging path has to be trustworthy before it carries anything
worth stealing.

**No hardcoded secrets.** No credential is compiled into either binary. There
is no default password to change, because there is no password. The sample
configuration ships no credential, and `tests/installer_test.sh` fails if a
credential-like key in it is ever given a value.

**Logs are redacted.** `internal/logging` wraps the `slog` handler and replaces
the value of any attribute whose key looks sensitive — `password`, `passwd`,
`passphrase`, `secret`, `token`, `credential`, `apikey`, `api_key`,
`private_key`, `privatekey`, `signing_key`, `session_key`, `authorization`,
`auth_header` — with `[REDACTED]`. Matching is case-insensitive and applies to
nested groups.

Redaction is a safety net, not the strategy. The strategy is not to log the
value at all. A net catches the key you thought of; the packages are written so
there is nothing to catch.

**Status and doctor print no secrets.** They report health, paths, versions and
sizes. They do not enumerate environment variables, echo configuration values
that could be sensitive, or dump database contents.
`internal/cli/render_test.go` runs all three commands with `PASSWORD`,
`AWS_SECRET_ACCESS_KEY` and a probe variable set, and fails if any value
appears in the output.

**Errors carry context, not payloads.** `internal/errors` records an operation
name and a `Kind`. Messages name what failed and which path was involved; they
do not include the value that failed, and they do not expose internals that
only help an attacker.

## Input validation

**Configuration is validated before anything starts.** `config.Validate`
reports every problem it finds, not just the first, and a process refuses to
start on invalid configuration rather than silently correcting it. Silent
correction is how an operator ends up believing a setting is in effect when it
is not.

**Paths cannot escape.** A relative `storage.path` resolves through
`system.ResolveInDir`, which rejects an absolute path, an empty name, and any
value that resolves outside the base — including `../../etc/shadow`. The prefix
comparison appends a separator, so a sibling directory sharing a name prefix
(`/var/lib/infrapilot-evil`) is not mistaken for a child. `INFRAPILOT_CONFIG`
and `INFRAPILOT_DATA_DIR` must be absolute, because a relative override would
make the resolved location depend on a working directory that a systemd service
does not control.

**Bounds are enforced.** Durations are range-checked
(`shutdown_timeout` 1s–10m, `heartbeat_interval` 1s–24h, `busy_timeout`
100ms–1m). A one-millisecond shutdown timeout would make every restart look
like a crash; a sub-second heartbeat would flood the journal.

## No shell injection

InfraPilot v0.1.0 executes no external command and spawns no shell. Nothing
builds a command line from configuration.

The installer is a shell script, so it takes the corresponding care: its `run`
helper passes arguments as an array (`"$@"`) which is never re-parsed by a
shell, so a path containing a space or a metacharacter cannot become a command.
It runs under `set -euo pipefail`, and `shellcheck` runs against it in CI with
no findings permitted.

## Database

SQLite, via `modernc.org/sqlite` — a pure-Go implementation, so there is no cgo
and no system SQLite in the trust boundary.

- The file is created `0600` inside a `0750` directory.
- `journal_mode=WAL`, verified after opening rather than assumed. A filesystem
  that silently refuses WAL is reported, not tolerated.
- `foreign_keys=ON`, also verified.
- `busy_timeout` bounds lock waits, so a contended database fails with a clear
  error instead of hanging.
- **`status` and `doctor` open the database read-only** (`mode=ro`), which also
  stops SQLite from creating the file. An observing command cannot migrate a
  schema, cannot write, and cannot bring a database into existence as a side
  effect of being asked a question.
- Migrations are versioned and recorded in `schema_migrations`, and run only
  from the Agent.

## Liveness without a listener

`infrapilot status` reports whether the Agent is running. Determining that
without a network endpoint or a privileged interface takes three steps, all of
which must agree:

1. Read the PID from `/var/lib/infrapilot/agent.pid`.
2. Send signal 0 to it — no-op delivery that reports whether the process exists
   and is signalable.
3. Compare `/proc/<pid>/comm` against the expected command name.

The third step is what makes it safe. A stale PID file whose number has been
recycled by an unrelated process would otherwise report the Agent as running.
The kernel truncates `comm` to 15 characters, so `infrapilot-agent` appears
truncated and the comparison is a prefix match.

The PID file is advisory, never authoritative: it is treated as a hint that
must be corroborated, so a corrupt or hand-edited file cannot make `status`
lie.

## Shutdown

The Agent handles `SIGTERM` and `SIGINT` through `signal.NotifyContext`, and
shutdown is bounded by `agent.shutdown_timeout` (15s by default) so a stuck
subsystem cannot block a restart forever. `TimeoutStopSec=30` in the unit sits
above that, so systemd's own patience outlasts the Agent's.

A stop signal arriving *during* start-up exits cleanly rather than reporting
the interrupted step as a crash — otherwise `Restart=on-failure` would fight an
operator who stopped the service at the wrong moment. `Restart=on-failure`,
not `always`, for the same reason: a deliberate stop stays stopped.
`StartLimitIntervalSec=300` and `StartLimitBurst=5`, in `[Unit]` where systemd
actually reads them, stop a crash loop from hammering the host.

## Dependencies

Kept small on purpose: `modernc.org/sqlite` and `gopkg.in/yaml.v3`. Every
dependency is code that runs with the Agent's privileges.

- Versions are pinned in `go.mod`, and CI fails if `go mod tidy` would change
  `go.mod` or `go.sum` — the committed state must describe the build.
- `govulncheck` runs in CI. It uses reachability analysis, so an advisory
  against a function InfraPilot never calls does not fail the build, and one
  against a function it does call, does.

At the time of writing, no reachable vulnerability is reported in either binary.

## Verifying the claims

```sh
go test ./... -count=1 -race     # includes the redaction and traversal tests
tests/installer_test.sh          # permissions and unit hardening
systemd-analyze verify installer/systemd/infrapilot-agent.service
govulncheck ./...
infrapilot doctor
```

Each property in this document has a test behind it. If one of them does not
hold on your host, that is a bug worth reporting — see
[SECURITY.md](../SECURITY.md).
