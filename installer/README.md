# Installer

Installs the InfraPilot Agent and CLI on a systemd Linux host. v0.1.0 targets
Ubuntu and Debian.

## Install

From a source checkout:

```sh
sudo ./installer/install.sh
```

For a release installation on Ubuntu or Debian:

```sh
curl -fsSL https://raw.githubusercontent.com/iAghaTraker/InfraPilot/main/install.sh | sudo bash
```

Release archives target `amd64` or `arm64` and are verified against the
published `checksums.txt` before installation. Re-running the installer creates
a timestamped configuration backup and preserves state.

This builds both binaries, creates the `infrapilot` service account, installs
the systemd unit, and starts and verifies the service. Building requires the Go
toolchain; to install on a host without one, build elsewhere and pass the
binaries in:

```sh
CGO_ENABLED=0 go build -trimpath -o dist/infrapilot       ./cmd/infrapilot
CGO_ENABLED=0 go build -trimpath -o dist/infrapilot-agent ./cmd/infrapilot-agent
CGO_ENABLED=0 go build -trimpath -o dist/infrapilot-web ./cmd/infrapilot-web
sudo ./installer/install.sh --from dist
```

### Options

| Option | Effect |
| --- | --- |
| `--prefix DIR` | Install binaries under `DIR/bin` (default `/usr/local`) |
| `--from DIR` | Use prebuilt binaries instead of building |
| `--no-start` | Install and enable the service without starting it |
| `--dry-run` | Print every action, change nothing |
| `--uninstall` | Remove the service, binaries and account, keeping data |

`--dry-run` needs no privileges and is the way to see exactly what would happen
before running it for real.

## What gets installed

| Path | Owner | Mode | Purpose |
| --- | --- | --- | --- |
| `/usr/local/bin/infrapilot` | `root:root` | `0755` | CLI |
| `/usr/local/bin/infrapilot-agent` | `root:root` | `0755` | Agent |
| `/usr/local/bin/infrapilot-web` | `root:root` | `0755` | Web API process |
| `/etc/infrapilot/` | `root:infrapilot` | `0750` | Configuration |
| `/etc/infrapilot/config.yaml` | `root:infrapilot` | `0640` | Settings |
| `/var/lib/infrapilot/` | `infrapilot:infrapilot` | `0750` | State |
| `/var/lib/infrapilot/infrapilot.db` | `infrapilot:infrapilot` | `0600` | Database |
| `/etc/systemd/system/infrapilot-agent.service` | `root:root` | `0644` | Service |
| `/etc/systemd/system/infrapilot-web.service` | `root:root` | `0644` | Web API service |

Binaries and configuration are owned by `root` and are not writable by the
service account: the Agent reads its configuration and must never rewrite it or
the binary it runs. Only the state directory is writable by the Agent.

Nothing is world-readable. See [docs/security.md](../docs/security.md).

## Service account

The Agent runs as `infrapilot`, a system account with no login shell and no
home directory of its own. It exists to own state and run one service.

The Agent is not run as root. It manages infrastructure, which makes it exactly
the kind of service worth attacking; privileged operations, when they are
needed in a later version, will be isolated behind an explicit boundary rather
than granted to the whole process.

## Configuration

An annotated `config.yaml` is installed on first install only. **An existing
configuration file is never overwritten**, on install or upgrade.

The file is generated from the real defaults in `internal/config`, so it cannot
drift from the code. Regenerate it after changing a default:

```sh
go test ./internal/config -update
```

Every setting is optional — the Agent runs on its defaults when the file is
absent.

## Verifying

The installer finishes by running `infrapilot doctor` and fails if the report
does. To check by hand:

```sh
infrapilot status
infrapilot doctor
systemctl status infrapilot-agent
journalctl -u infrapilot-agent -f
infrapilot web status
infrapilot web logs
```

## Uninstalling

```sh
sudo ./installer/install.sh --uninstall
```

This stops and removes the service, the binaries and the service account.

**Data and configuration are kept.** `/var/lib/infrapilot` and
`/etc/infrapilot` are left in place: losing a database to an uninstall is not
recoverable, so removing it is your decision, not the script's. Remove them by
hand when you are sure.

The installed CLI provides the safer lifecycle command:

```sh
sudo infrapilot uninstall
```

It explicitly confirms before stopping and removing the two InfraPilot
services, systemd units, and binaries, while preserving identities, databases,
logs, and configuration. To remove all local data, use
`sudo infrapilot uninstall --purge`; purge requires the exact phrase
`REMOVE ALL INFRAPILOT DATA`.

## Testing

```sh
tests/installer_test.sh
```

The suite runs unprivileged and changes nothing: it covers argument handling,
`--dry-run` output, the documented permissions, and the hardening directives in
the systemd unit. Where `systemd-analyze` is available it validates the unit
too.

## Future packaging

`sudo apt install infrapilot` is the eventual goal. v0.1.0 deliberately ships a
script instead: the layout above — FHS paths, a system account, a hardened unit,
an idempotent install that preserves configuration — is what a `.deb` would
need to produce anyway, and getting it right in a readable script first means
the packaging has something correct to wrap.
