# Configuration

InfraPilot runs correctly with no configuration file. Every setting has a
default, and the defaults are chosen so a fresh install works unattended.

Configuration is resolved in layers, each overriding the one before it:

1. **Built-in defaults** — a complete, valid configuration on their own.
2. **The YAML file**, if present.
3. **Environment variables**, for systemd and container deployments.

The result is validated before anything starts. A process refuses to run on
invalid configuration rather than silently correcting it: silent correction is
how an operator ends up believing a setting is in effect when it is not.

## Where the file lives

| Mode | Configuration | State |
| --- | --- | --- |
| Production | `/etc/infrapilot/config.yaml` | `/var/lib/infrapilot` |
| Development (`INFRAPILOT_DEV=1`) | `$XDG_CONFIG_HOME/infrapilot/config.yaml` | `$XDG_DATA_HOME/infrapilot` |

`XDG_CONFIG_HOME` defaults to `~/.config` and `XDG_DATA_HOME` to
`~/.local/share`, per the XDG Base Directory specification.

`infrapilot status` prints the resolved paths, so you never have to guess which
file is in effect:

```sh
infrapilot status
```

The installer writes an annotated `config.yaml` on first install only. **An
existing file is never overwritten**, on install or on upgrade.

## The file

```yaml
version: 1

agent:
  data_dir: /var/lib/infrapilot
  shutdown_timeout: 15s
  heartbeat_interval: 1m0s

logging:
  level: info
  format: text

storage:
  path: infrapilot.db
  busy_timeout: 5s
```

Every key is optional. Omit a section entirely and its defaults apply.

The shipped sample at `installer/config.sample.yaml` is generated from the real
defaults in `internal/config`, so it cannot drift from the code. Regenerate it
after changing a default:

```sh
go test ./internal/config -update
```

### `version`

The configuration schema version. The current version is `1`.

A file declaring a version this build does not understand is **rejected**, not
best-effort parsed. A future format change will then fail loudly instead of
being misread. Omitting the key is allowed and means "whatever this build
expects".

### `agent`

| Key | Default | Range | Meaning |
| --- | --- | --- | --- |
| `data_dir` | `/var/lib/infrapilot` | absolute path | Directory holding InfraPilot state, including the database |
| `shutdown_timeout` | `15s` | 1s – 10m | How long a graceful shutdown may take before the Agent exits anyway |
| `heartbeat_interval` | `1m0s` | 1s – 24h | How often the Agent records a liveness entry in its log |

`data_dir` must be absolute. A relative path would resolve against the process
working directory, which for a systemd service is not something an operator
controls.

`shutdown_timeout` bounds shutdown so a stuck subsystem cannot block a restart
forever. **Keep systemd's `TimeoutStopSec` above it** — the shipped unit uses
`30s`. If you raise the timeout past 30 seconds, raise `TimeoutStopSec` too, or
systemd will kill the Agent before its own shutdown completes.

`heartbeat_interval` in v0.1.0 writes to the log only; there is no remote
endpoint to report to. The one-second floor exists because a sub-second
heartbeat would flood the journal.

### `logging`

| Key | Default | Accepted | Meaning |
| --- | --- | --- | --- |
| `level` | `info` | `debug`, `info`, `warn`, `error` | Minimum severity to emit |
| `format` | `text` | `text`, `json` | Output format |

Use `json` when shipping logs to an aggregator; `text` is easier to read in a
terminal or in `journalctl`.

Logs go to standard error, which under systemd means the journal:

```sh
journalctl -u infrapilot-agent -f
```

Attribute values whose keys look sensitive are replaced with `[REDACTED]`
before anything is written, at every level including `debug`. See
[security.md](security.md).

### `storage`

| Key | Default | Range | Meaning |
| --- | --- | --- | --- |
| `path` | `infrapilot.db` | relative or absolute | Database file |
| `busy_timeout` | `5s` | 100ms – 1m | How long a query waits for a lock before failing |

A **relative** `path` resolves inside `agent.data_dir` and may not escape it —
`../../etc/shadow` is rejected, not silently followed. An **absolute** path is
used as given, which is how you put the database on a different filesystem.

The database is SQLite in WAL mode, created `0600`.

## Durations

Anywhere a duration is accepted, both forms work:

- A Go duration string: `30s`, `5m`, `1h30m`, `1m0s`.
- A plain number of seconds: `30` means `30s`.

## Environment variables

Environment variables override the file. Only variables that are **set and
non-empty** take effect; an unset variable leaves the previous layer untouched.
A set-but-invalid variable is an error, not a fallback to the default.

### Paths

These are resolved before the configuration file is read — they decide *which*
file is read — so they have no YAML equivalent.

| Variable | Effect |
| --- | --- |
| `INFRAPILOT_DEV` | Any value except `0`, `false`, `no`, `off` enables development mode |
| `INFRAPILOT_CONFIG` | Absolute path to the configuration file |
| `INFRAPILOT_DATA_DIR` | Absolute path to the data directory |

`INFRAPILOT_CONFIG` and `INFRAPILOT_DATA_DIR` are applied independently and
take precedence over development mode. Both must be absolute paths.

### Settings

| Variable | Overrides |
| --- | --- |
| `INFRAPILOT_LOG_LEVEL` | `logging.level` |
| `INFRAPILOT_LOG_FORMAT` | `logging.format` |
| `INFRAPILOT_SHUTDOWN_TIMEOUT` | `agent.shutdown_timeout` |
| `INFRAPILOT_HEARTBEAT_INTERVAL` | `agent.heartbeat_interval` |
| `INFRAPILOT_STORAGE_PATH` | `storage.path` |
| `INFRAPILOT_BUSY_TIMEOUT` | `storage.busy_timeout` |

Under systemd, set them in a drop-in rather than editing the shipped unit, so
a package upgrade does not discard your change:

```sh
sudo systemctl edit infrapilot-agent
```

```ini
[Service]
Environment=INFRAPILOT_LOG_LEVEL=debug
Environment=INFRAPILOT_LOG_FORMAT=json
```

```sh
sudo systemctl restart infrapilot-agent
```

## Permissions

The configuration file is owned by `root:infrapilot`, mode `0640`: the Agent
can read it and cannot write it. A compromised Agent must not be able to
rewrite its own settings.

It must never be world-readable. `infrapilot doctor` warns when it is.

Full filesystem layout: [security.md](security.md#filesystem-permissions).

## Errors

**A missing file is not an error.** Defaults plus environment are a complete
configuration.

**An unreadable or malformed file is.** The operator stated an intent that
could not be honoured, and guessing would be worse than refusing to start.

**An unknown key is an error.** A typo is reported rather than ignored —
silently ignoring one means an operator believes a setting took effect when it
did not, and a typo in a security setting would then fail open.

**Every problem is reported at once**, not one per run, so fixing a file does
not mean rerunning once per mistake:

```
invalid configuration (from /etc/infrapilot/config.yaml): logging.level:
unsupported log level "verbose": must be one of debug, info, warn, error;
agent.shutdown_timeout: must be at most 10m0s, got 1h0m0s
```

The Agent refuses to start on invalid configuration. The CLI does not: `status`
and `doctor` still run and report the problem, because a diagnostic tool that
cannot run when something is wrong is a diagnostic tool that is never available
when you need it.

## Checking your configuration

```sh
infrapilot doctor
```

`doctor` validates the configuration, checks the file's permissions, and
reports the data directory, database and logging state. It exits `0` when
nothing failed — warnings do not fail the run — and `1` when a check failed.

To check a file before putting it in place:

```sh
INFRAPILOT_CONFIG=/absolute/path/to/candidate.yaml infrapilot doctor
```
