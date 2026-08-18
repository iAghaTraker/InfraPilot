# InfraPilot

Open-source, self-hosted infrastructure management platform.

InfraPilot aims to make Linux and VPS management approachable for beginners
while staying powerful enough for advanced users.

**You own the infrastructure. InfraPilot helps you manage it.**

---

## 🚧 Project status: v0.4.1 — Release installer and Web Panel foundation

**InfraPilot is early development and is not production-ready.**

v0.3.0 builds on the v0.2.0 foundation and adds local cryptographic device identity and pairing.
It deliberately implements a small,
well-tested core rather than a broad, shallow feature set. What follows is an
accurate account of what exists today.

### What works

- **`infrapilot-agent`** — a long-running service with a full lifecycle:
  start-up validation, SQLite initialisation and migrations, a heartbeat, and a
  bounded graceful shutdown on `SIGTERM`/`SIGINT`.
- **`infrapilot`** — a CLI for installation status, service management and host information.
- **Configuration** — layered defaults → YAML → environment, validated before
  anything starts, with unknown keys rejected rather than ignored.
- **Storage** — SQLite in WAL mode with versioned migrations, opened read-only
  by observing commands.
- **Logging** — structured `log/slog` output in text or JSON, with sensitive
  values redacted.
- **Errors** — typed, classified, wrapped with operation context, mapped to
  meaningful exit codes.
- **Installer** — a systemd unit with extensive hardening, an install script
  that never overwrites configuration and never deletes data, and a 51-check
  test suite covering both.
- **CI** — formatting, vet, build, tests, race detector, arm64 cross-compile,
  shellcheck, `systemd-analyze verify` and `govulncheck`.
- **Service Manager** — systemd-backed service listing, status, lifecycle,
  enablement and bounded journal logs.
- **System information** — operating system, kernel, CPU, memory, disk and uptime.
- **Secure device identity** — Ed25519 keys, expiring single-use pairing tokens,
  trusted-device listing/revocation, and challenge-signature primitives.

### What does not exist yet

Everything else. Specifically, InfraPilot v0.2.0 **cannot**:

- Manage systemd services, Nginx, databases or Minecraft servers.
- Be accessed remotely — **the Agent opens no ne twork port at all**.
  - Run a Web Panel or remote API; pairing is a local foundation only.
- Serve a Web Panel or a Desktop App.
- Back up, restore, migrate or monitor anything.
- Load a module.

These are planned; see [ROADMAP.md](ROADMAP.md). None of them are partially
implemented — the absence is deliberate, so that the foundation they will be
built on is correct first.

---

## ✨ Why InfraPilot?

Managing a Linux server usually means learning dozens of commands, files and
services:

```sh
systemctl status nginx
systemctl restart nginx
journalctl -u nginx
nano /etc/nginx/nginx.conf
```

The long-term goal is a unified management layer over that — not a replacement
for Linux, and not an attempt to hide it. Advanced users keep full access to
the underlying system; beginners get to perform common operations without
memorising the commands.

Remote access and the broader management layer do not exist yet.

---

## 🚀 Quick start

### From source

```sh
git clone https://github.com/iAghaTraker/InfraPilot.git
cd infrapilot

go build ./...
go test ./...

go run ./cmd/infrapilot version
go run ./cmd/infrapilot doctor
go run ./cmd/infrapilot status
```

Requires Go 1.25 or newer and Linux. No cgo toolchain is needed — SQLite is a
pure-Go implementation.

### Trying it without root

```sh
export INFRAPILOT_DEV=1
go run ./cmd/infrapilot doctor
go run ./cmd/infrapilot-agent      # Ctrl-C to stop
```

Development mode puts configuration and state under
`$XDG_CONFIG_HOME/infrapilot` and `$XDG_DATA_HOME/infrapilot`, so nothing needs
privileges and nothing touches system directories.

### Installing as a service

```sh
curl -fsSL https://raw.githubusercontent.com/iAghaTraker/InfraPilot/main/install.sh | sudo bash
```

The installer detects amd64 or arm64 Linux, downloads a GitHub Release artifact,
verifies its SHA256 checksum, and preserves configuration and data during
upgrades. A local checkout can use `./installer/install.sh --from DIR` or
`./install.sh --from artifact.tar.gz`.

This builds both binaries, creates the unprivileged `infrapilot` service
account, installs a hardened systemd unit, and starts and verifies the service.
Run `./installer/install.sh --dry-run` first to see exactly what it would do —
that needs no privileges.

See [installer/README.md](installer/README.md).

The installer also installs `infrapilot-web` and its hardened systemd unit.
Existing configuration and state are preserved on upgrades; use `--from DIR`
to install prebuilt binaries on a host without Go.

---

## ⌨️ CLI

```
infrapilot version   Print version and platform information
infrapilot status    Show the state of this installation
infrapilot doctor    Check the installation and report problems
infrapilot service list                         List systemd services
infrapilot service status <service>             Show service status
infrapilot service start|stop|restart <service> Change service state
infrapilot service enable|disable <service>     Change boot enablement
infrapilot service logs <service> [--lines N]    Show recent journal logs
infrapilot system info                          Show basic host information
infrapilot sk create                             Create a device identity
infrapilot sk status                             Show the local identity
infrapilot sk replace <pairing-token>             Register a device identity
infrapilot sk list                                List trusted devices
infrapilot sk revoke <device-id>                  Revoke a trusted device
infrapilot web start                              Start the local Web Panel service
infrapilot web start background                   Start it through systemd
infrapilot web stop|restart|status|logs           Manage or inspect the Web Panel
infrapilot web enable|disable                     Enable or disable at boot
```

The private key is stored under the configured data directory with mode `0600`
and is never printed. Pairing tokens are signed by the device key, expire after
10 minutes, are stored only as hashes, and are consumed on successful pairing.
The future Web Panel must authenticate by challenge and signature; IP address,
HTTP headers and public-key possession alone are not authentication.

The v0.4 Web Panel is an API foundation, not a browser UI. It binds to
`127.0.0.1:8090` by default and protects its read-only APIs with a
challenge/signature login using a paired Ed25519 device identity. It does not
trust source IPs or browser headers.

Service names are validated before they reach systemd. Listing, status and
logs generally need read access; start, stop, restart, enable and disable
usually require root or an appropriate polkit rule.

Exit codes: `0` success, `1` failure, `2` incorrect invocation.

`infrapilot status`:

```
InfraPilot Status

Agent
  Status: running
  Version: 0.1.0
  PID: 32082

System
  OS: Ubuntu 26.04 LTS
  Architecture: amd64
  Kernel: 7.0.0-28-generic
  Uptime: 12h 2m
  CPUs: 2

Storage
  Database: healthy
  Path: /var/lib/infrapilot/infrapilot.db
  Schema: v1
  Size: 4.0 KiB

Configuration
  File: /etc/infrapilot/config.yaml
  Data directory: /var/lib/infrapilot
  Mode: production
```

`sudo infrapilot doctor`:

```
InfraPilot Doctor

PASS  Operating System
      Ubuntu 26.04 LTS
PASS  Architecture
      amd64
PASS  Configuration
      using defaults; no file at /etc/infrapilot/config.yaml
WARN  Data Directory
      /var/lib/infrapilot does not exist yet; the agent creates it on first start
WARN  Database
      no database at /var/lib/infrapilot/infrapilot.db yet; the agent creates it on first start
PASS  Logging
      level INFO, format text
WARN  Agent
      not running; start it with 'systemctl start infrapilot-agent'

4 passed, 3 warnings, 0 failed

No failures. Warnings are safe to ignore if they are expected.
```

Warnings do not fail the exit code. A fresh installation legitimately warns
about a directory the Agent has not created yet.

Neither command modifies anything: both open the database read-only.

---

## 🏗️ Architecture

InfraPilot is **Agent-first**. The Agent runs on your server and is the source
of truth; the CLI — and later the Web Panel and Desktop App — are clients.

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

A client never inspects the filesystem, database or process table itself. That
constraint is what lets a future Web Panel reuse the same code and reach the
same answers instead of reimplementing them.

The core platform does not require a centralised InfraPilot cloud. A host with
no internet connection is fully manageable.

```
cmd/infrapilot/           CLI entry point
cmd/infrapilot-agent/     Agent entry point
internal/agent/           Agent lifecycle
internal/cli/             Command dispatch and rendering
internal/core/            Agent-side operations: status, doctor
internal/config/          Layered configuration and validation
internal/storage/         SQLite: open, migrate, health
internal/logging/         Structured logging and redaction
internal/errors/          Typed errors and exit codes
internal/system/          Host inspection, paths, permissions, liveness
pkg/version/              Build identity
installer/                Install script, systemd unit, sample configuration
tests/  scripts/  docs/   Installer tests, developer tooling, documentation
web/ desktop/ modules/ sdk/   Placeholders — a README each, no code
```

Full detail, including why `cmd/` and `internal/` are used:
[docs/architecture.md](docs/architecture.md).

---

## 🔐 Security

The Agent is a high-value target, so v0.1.0 holds as little privilege as it can:

- **No network listener.** Nothing binds a port or a socket.
- **Runs unprivileged**, as a system account with no login shell.
- **Cannot modify its own binary or configuration** — both are root-owned.
- **Nothing world-readable.** Directories `0750`, configuration `0640`,
  database and PID file `0600`.
- **No hardcoded secrets and no default passwords.** There is no credential to
  compromise.
- **Secrets are kept out of logs**, and redacted as a backstop.
- **Configured paths cannot escape** the data directory.
- **Extensive systemd hardening** — no capabilities, `ProtectSystem=strict`,
  a syscall filter, `MemoryDenyWriteExecute`, and `RestrictAddressFamilies=AF_UNIX`.

Each of these has a test behind it. See [docs/security.md](docs/security.md).

The SSH-like pairing model described in [ROADMAP.md](ROADMAP.md) — private keys
never leaving the client, single-use pairing keys, per-device identity and
revocation — is the **v0.3 design**. It is not implemented. v0.1.0's
contribution to it is having made no decision that would obstruct it.

To report a vulnerability, see [SECURITY.md](SECURITY.md).

---

## ⚙️ Configuration

InfraPilot runs on its defaults; a configuration file is optional.

```yaml
version: 1

agent:
  data_dir: /var/lib/infrapilot
  shutdown_timeout: 15s
  heartbeat_interval: 1m0s

logging:
  level: info      # debug, info, warn, error
  format: text     # text, json

storage:
  path: infrapilot.db
  busy_timeout: 5s
```

| Mode | Configuration | State |
| --- | --- | --- |
| Production | `/etc/infrapilot/config.yaml` | `/var/lib/infrapilot` |
| Development (`INFRAPILOT_DEV=1`) | `$XDG_CONFIG_HOME/infrapilot/config.yaml` | `$XDG_DATA_HOME/infrapilot` |

Environment variables override the file. Every setting, and the full precedence
rules: [docs/configuration.md](docs/configuration.md).

---

## 🛠️ Development

```sh
go build ./...             # build
go test ./...              # test
go test ./... -race        # test with the race detector
scripts/check.sh           # everything CI runs
```

`scripts/check.sh` runs formatting, vet, build, tests, the race detector,
dependency tidiness, shellcheck, the installer suite, `systemd-analyze` and
`govulncheck`, skipping any tool you do not have installed. `--quick` skips the
slower checks.

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request, and
[docs/architecture.md](docs/architecture.md) before making a structural change.

---

## ⚠️ Current limitations

Worth knowing before you try v0.2.0:

- **Linux only.** The Agent reads `/proc` and expects systemd. Other platforms
  are not supported.
- **amd64 and arm64 only.** The installer rejects anything else.
- **Ubuntu and Debian are the tested distributions.** The installer warns and
  continues elsewhere.
- **The Agent does nothing but stay alive.** It initialises its database and
  writes a heartbeat. Service management is local and systemd-only.
- **Local access only.** There is no remote management, by design.
- **Single host.** There is no notion of a second server.
- **The database holds only a schema version.** The migration machinery exists;
  there is nothing yet to store.
- **Pre-1.0 compatibility.** Configuration format and database schema may change
  between minor versions. Migrations exist so that can be handled, but it is not
  a stability promise yet.

---

## 🗺️ Roadmap

```
v0.1  Foundation
v0.2  Agent + Service Manager
v0.3  Secure Device Identity & Pairing
v0.4  Local Web Panel API + Professional Installer ← current
v0.5  File Manager
v0.6  Minecraft
v0.7  Module System
v0.8  Module Integrations
v0.9  Backup
v0.10 Migration
v0.11 Desktop App
v0.12 Security Hardening
v0.13 Templates + Clone
v0.14 Monitoring + Auto-Healing
v0.15 RBAC
v0.16 API + SDK
v0.17 Module Registry
v0.18 AI Assistant
v0.19 Advanced Networking
v1.0  Stable
```

The long-term goal covers service management, Minecraft servers, databases, web
stacks, file management, backups, server-to-server migration, monitoring,
role-based access control, a public API, and a community module ecosystem.

[ROADMAP.md](ROADMAP.md) holds the full specification for each version. Anything
described there and not listed under "What works" above does not exist yet.

---

## 🤝 Contributing

Contributions are welcome — bug fixes, features, documentation, tests, security
improvements.

Please read [CONTRIBUTING.md](CONTRIBUTING.md) first. It covers the development
setup, the checks a change has to pass, and the architectural rules a pull
request needs to respect. Note that v0.2+ features are not accepted into v0.1.

---

## 📜 Licence

MIT. See [LICENSE](LICENSE).

---

## 🧠 Philosophy

InfraPilot does not try to hide Linux. It adds a management layer on top of it.

Advanced users keep full access to the underlying system and CLI. Beginners get
to perform common operations without memorising dozens of commands.

```
                       Your VPS
                          │
                          ▼
                    InfraPilot Agent
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
         CLI          Web Panel        Desktop
          │               │               │
          └───────────────┼───────────────┘
                          ▼
                   Your Infrastructure
```

**Your infrastructure. Your server. Your data. Your control.**
