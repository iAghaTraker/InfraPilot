InfraPilot — Development Roadmap

«InfraPilot is an open-source, self-hosted infrastructure and server management platform.

Install → Pair → Manage

The goal is to make Linux/VPS management accessible to beginners while preserving the power and flexibility expected by advanced users.»



1. Vision

InfraPilot should allow users to manage their own infrastructure from a single interface without requiring deep Linux knowledge.

The long-term platform should manage:

- Linux VPS servers
- System services
- Minecraft servers
- Databases
- Web servers
- Files
- Backups
- Server migrations
- Monitoring
- Deployments
- Modules
- Multiple servers
- Automation
- APIs
- Integrations

InfraPilot will provide three primary interfaces:

CLI
Web Panel
Desktop App

All interfaces communicate with the InfraPilot Agent.

---

2. Core Architecture

InfraPilot is Agent-first.

The Agent runs on the user's VPS and acts as the local control plane.

                         InfraPilot
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
          ▼                  ▼                  ▼
         CLI             Web Panel         Desktop App
          │                  │                  │
          └──────────────────┼──────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │ InfraPilot      │
                    │ Agent           │
                    └────────┬────────┘
                             │
           ┌─────────────────┼─────────────────┐
           │                 │                 │
           ▼                 ▼                 ▼
       Services           Modules           Actions
           │                 │                 │
           ▼                 ▼                 ▼
       systemd            MySQL             Backup
       Nginx              Redis             Restore
       Minecraft          MongoDB           Migration
       etc.               etc.              etc.

The core platform must not require a centralized InfraPilot cloud.

A user should be able to install InfraPilot on a VPS and operate it independently.

---

3. Security Model

Security must be designed from the beginning.

InfraPilot should use an SSH-like trust model.

Client                              VPS

Private Key 🔑                  Public Key
     │                              │
     │      Challenge/Response      │
     └─────────────────────────────►│

Rules

- Private keys never leave the client.
- VPS stores public keys.
- Pairing credentials are temporary.
- Pairing credentials are single-use.
- Every device has a unique identity.
- Devices can be revoked independently.
- Sensitive actions are audited.
- Secrets must never appear in logs.
- No default passwords.
- No hardcoded credentials.
- Network exposure must be minimized.
- Modules must declare permissions.

---

4. Versioning

InfraPilot follows semantic versioning:

MAJOR.MINOR.PATCH

Development milestones use:

v0.x.x

The first stable release is:

v1.0.0

### v0.5.0 — Local Web Panel UI foundation

The first browser dashboard is served as static assets embedded in
`infrapilot-web`. It uses the existing challenge/signature device identity
flow and displays system, service, and module foundations. It deliberately
does not add Minecraft management, cloud services, or external authentication.

---

5. v0.1.0 — Foundation

Status: DELIVERED

Everything listed below is implemented and tested. See README.md for what the
release does and does not do, and docs/architecture.md for how it is built.

Goal

Build the initial InfraPilot architecture.

Deliver

- Repository structure
- Go Agent
- Go CLI
- Configuration system
- SQLite foundation
- Structured logging
- Error handling
- Basic testing
- GitHub Actions CI
- Documentation
- License
- Security policy
- Contribution guide

Initial CLI

infrapilot version
infrapilot status
infrapilot doctor

Repository

As built. The binaries live under cmd/ and their implementations under
internal/, rather than in top-level agent/ and cli/ directories: cmd/<binary>
is the Go convention that build tooling expects, and internal/ makes the
compiler enforce that nothing outside the module imports the implementation.
See docs/architecture.md.

infrapilot/
├── cmd/
│   ├── infrapilot/
│   └── infrapilot-agent/
├── internal/
│   ├── agent/
│   ├── cli/
│   ├── config/
│   ├── core/
│   ├── errors/
│   ├── logging/
│   ├── storage/
│   └── system/
├── pkg/
│   └── version/
├── web/
├── desktop/
├── modules/
├── sdk/
├── installer/
│   └── systemd/
├── docs/
├── tests/
├── scripts/
├── .github/
│   └── workflows/
├── README.md
├── ROADMAP.md
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE

Definition of Done

A developer can:

git clone <repository>
cd infrapilot

go test ./...
go build ./...

./infrapilot version
./infrapilot status
./infrapilot doctor

Verified. scripts/check.sh runs the full set of checks CI runs.

---

6. v0.2.0 — Agent + Service Manager

Goal

Manage Linux services through InfraPilot.

Status: IN PROGRESS. The local CLI service manager, systemd adapter, service
status/listing, lifecycle operations, bounded journal logs and basic system
information are implemented. Remote access, pairing and action queues remain
future work.

Features
- Agent daemon
- systemd integration
- Service discovery
- Service status
- Start
- Stop
- Restart
- Enable
- Disable
- Process information
- CPU usage
- RAM usage
- Disk usage
- Network statistics

CLI

infrapilot service list
infrapilot service status <service>
infrapilot service start <service>
infrapilot service stop <service>
infrapilot service restart <service>

Action System

Every operation should use a unified action system.

Queued
  ↓
Running
  ↓
Success

or:

Queued
  ↓
Running
  ↓
Failed

Each action should contain:

- ID
- Type
- Status
- Progress
- Start time
- End time
- Error
- Logs

---

7. v0.3.0 — Secure Pairing

Goal

Securely connect a user's PC to their VPS.

Status: DELIVERED for the cryptographic foundation. Ed25519 device identities,
expiring single-use signed pairing tokens, trusted-device storage, revocation,
audit records and one-time challenge verification are implemented. No Web
Panel, network listener or remote API is included.

Client

infrapilot sk create

VPS

infrapilot sk replace <PAIRING_KEY>

Features

- Ed25519 device identity
- Public-key authentication
- Challenge/response
- Encrypted transport
- Pairing expiration
- Single-use pairing
- Device registration
- Device revocation
- Key rotation
- Device permissions
- Security audit events

Definition of Done

A user's computer can securely connect to an InfraPilot Agent without transferring the VPS root password.

---

8. v0.4.0 — Web Panel MVP

Goal

Manage a VPS through a browser.

URL

https://SERVER_IP/panel

Dashboard

Display:

CPU
RAM
Disk
Network
Uptime

Service Management

Start
Stop
Restart
Status

Logs

Provide live service logs.

UI

- Responsive
- Mobile-friendly
- Desktop-friendly
- Dark mode
- Accessible
- Fast
- Minimal animations

---

9. v0.5.0 — File Manager

Goal

Manage server files without requiring SSH.

Features

- Directory browser
- Upload
- Download
- Rename
- Delete
- Create directory
- Create file
- Text editor
- Search
- File metadata
- Permission display

Security

Implement:

- Path traversal protection
- Permission validation
- Upload limits
- Safe path resolution
- Audit logging

Never allow arbitrary filesystem access without authorization.

---

10. v0.6.0 — Minecraft Module

Goal

Make InfraPilot useful for Minecraft server owners.

Initial Support

- Vanilla
- Paper

Future:

- Fabric
- Forge
- NeoForge

Features

- Create server
- Delete server
- Start
- Stop
- Restart
- Console
- Logs
- Player count
- Version selection
- Java management
- RAM configuration
- "server.properties"
- World management
- Plugin management
- Automatic systemd service

CLI

infrapilot minecraft list
infrapilot minecraft create
infrapilot minecraft start <server>
infrapilot minecraft stop <server>
infrapilot minecraft restart <server>

---

11. v0.7.0 — Module System

Goal

Turn InfraPilot into an extensible platform.

Modules should handle specific technologies instead of hardcoding everything into the Agent.

Module Lifecycle

Install
   ↓
Configure
   ↓
Start
   ↓
Stop
   ↓
Update
   ↓
Uninstall

Module Manifest

Each module should declare:

- Name
- Version
- Author
- Description
- Dependencies
- Supported OS
- Supported versions
- Permissions
- Ports
- Services
- Configuration
- Health checks

Initial Modules

MySQL
PostgreSQL
MongoDB
Redis
Nginx
Minecraft

---

12. v0.8.0 — Module Integrations

Goal

Allow modules to interact with each other.

Example:

MySQL
   ↓
Database
   ↓
Minecraft Plugin
   ↓
Minecraft Server

The UI should understand compatible integrations.

Example:

Plugin:
[ CoreProtect ]

Database:
[ MySQL ]

Minecraft Server:
[ Survival ]

[ Connect ]

InfraPilot should be able to:

- Create database
- Create database user
- Generate credentials
- Configure plugin
- Test connection
- Restart/reload service

---

13. v0.9.0 — Backup System

Goal

Create reliable server backups.

Features

- Manual backups
- Scheduled backups
- Retention policies
- Backup verification
- Restore
- Backup metadata
- Encryption
- Progress reporting

Destinations

- Local
- SFTP
- S3-compatible storage

Safety

Restoring over production data must require explicit confirmation.

---

14. v0.10.0 — Server Migration

Goal

Move services and configurations between InfraPilot-managed servers.

Flow
Source VPS
    ↓
Pre-flight checks
    ↓
Initial synchronization
    ↓
Database synchronization
    ↓
Final synchronization
    ↓
Stop source
    ↓
Final sync
    ↓
Start destination

Checks

- Disk space
- OS compatibility
- Required software
- Module compatibility
- Port availability
- Database availability
- Backup availability

Features

- InfraPilot → InfraPilot migration
- Secure server-to-server authentication
- Progress tracking
- Logs
- Rollback support

---

15. v0.11.0 — Desktop App

Goal

Provide a native server management application.

Technology

Prefer Tauri.

Platforms

- Windows
- Linux
- macOS

Features

- Pairing
- QR pairing
- Multiple VPSs
- Server dashboard
- Console
- Logs
- Start/Stop/Restart
- Notifications
- Device management

Private credentials must be stored using the operating system's secure credential storage.

---

16. v0.12.0 — Security Hardening

Goal

Prepare InfraPilot for serious public adoption.

Deliver

- Threat model
- Security architecture documentation
- Dependency scanning
- Secret scanning
- SAST
- Security test suite
- Authentication hardening
- Rate limiting
- Brute-force protection
- Secure headers
- Session security
- Audit-log hardening
- Agent privilege review
- Least-privilege improvements
- Signed releases

Critical and high-severity security issues must be resolved before release.

---

17. v0.13.0 — Templates + Clone

Goal

Make server deployment repeatable.

Features

- Server templates
- Export templates
- Import templates
- Server cloning
- Configuration profiles
- Plugin profiles
- Environment variables
- Staging environments

Example:

Production Server
       ↓
Save Template
       ↓
New VPS
       ↓
Deploy Template

---

18. v0.14.0 — Monitoring + Auto-Healing

Features

- Historical metrics
- CPU graphs
- RAM graphs
- Disk graphs
- Network graphs
- Service health checks
- Crash detection
- Controlled auto-restart
- Crash-loop detection
- Disk alerts
- RAM alerts
- Notifications

Auto-healing must prevent infinite restart loops.

---

19. v0.15.0 — Multi-User + RBAC

Roles

Owner
Admin
Operator
Viewer

Permissions

Permissions should be assignable to:

- Users
- Devices
- Servers
- Modules
- Actions

Temporary Access

Example:

Server:
Survival

Permissions:
Console
Restart

Expires:
24 hours

---

20. v0.16.0 — Public API + SDK

Goal

Allow developers to integrate InfraPilot.

Features

- REST API
- WebSocket/event API
- Webhooks
- API tokens
- OpenAPI specification
- CLI automation
- SDK
- Module SDK
- Plugin hooks

Example:

Server stopped
      ↓
Webhook
      ↓
Discord / Telegram / Custom Service

---

21. v0.17.0 — Module Registry

Goal

Build an open ecosystem around InfraPilot modules.

Features

- Public module registry
- Version management
- Compatibility metadata
- Maintainer information
- Package signatures
- Security status
- Installation
- Updates
- Ratings/reviews

Before installation, display requested permissions.

Example:

Module: Example Module

Permissions:

✓ Read service status
✓ Manage Minecraft
⚠ Execute privileged operations
✗ Access unrelated files

[Install]
[Cancel]

---

22. v0.18.0 — AI Assistant

Goal

Help users understand and troubleshoot their infrastructure.

Features

- Explain logs
- Diagnose common failures
- Explain resource usage
- Suggest configuration changes
- Generate commands
- Explain Linux errors
- Summarize incidents

Safety

AI must not silently execute destructive operations.

Preferred flow:

AI Suggestion
      ↓
User Approval
      ↓
InfraPilot Action

---

23. v0.19.0 — Advanced Networking

Goal

Support secure remote management even when direct connectivity is difficult.

Features

- Optional relay
- Connection health
- NAT traversal research
- Secure remote sessions
- Private networking
- Per-device network permissions

The official InfraPilot relay must not be required for core self-hosted functionality.

---

24. v1.0.0 — Stable

InfraPilot 1.0 should be production-ready.

Required
- Stable Agent
- Stable CLI
- Stable Web Panel
- Stable Desktop App
- Secure pairing
- Device management
- Service management
- File Manager
- Minecraft module
- Module system
- Backup
- Restore
- Migration
- Monitoring
- RBAC
- Audit logs
- Public API
- Documentation
- Upgrade system
- Rollback system
- Automated tests
- Security review

v1.0 Promise

«Install InfraPilot on a Linux VPS, securely pair your device, and manage your infrastructure without needing to become a Linux expert.»

---

25. Future v1.x

Possible future features:

- Docker management
- Podman management
- Kubernetes modules
- WordPress stack
- PHP deployment
- Node.js deployment
- Python deployment
- Go deployment
- Rust deployment
- Git deployment
- CI/CD
- Additional game server modules
- Multi-node clusters
- High availability
- Advanced observability
- Community module ecosystem

---

26. Recommended Development Order

Do not attempt to build the entire platform at once.

Foundation
    ↓
Agent
    ↓
Service Manager
    ↓
Secure Pairing
    ↓
Web Panel
    ↓
File Manager
    ↓
Minecraft
    ↓
Module System
    ↓
Integrations
    ↓
Backup
    ↓
Migration
    ↓
Desktop
    ↓
Security Hardening
    ↓
Monitoring
    ↓
RBAC
    ↓
API + SDK
    ↓
Module Registry
    ↓
AI
    ↓
Networking
    ↓
v1.0

---

27. Development Rules

These rules apply to all future versions.

Rule 1 — Agent First

The Agent is the core of InfraPilot.

Clients should communicate with the Agent instead of implementing server logic independently.

Rule 2 — Do Not Overengineer

Do not implement future systems before their roadmap version.

Rule 3 — Every Version Must Be Testable

Every release must have appropriate:

- Unit tests
- Integration tests
- Security tests
- Manual verification

Rule 4 — Security First

Never trade authentication or authorization for convenience.

Rule 5 — No Hardcoded Secrets

Never commit:

- Passwords
- API keys
- Private keys
- Tokens
- Production credentials

Rule 6 — Preserve Existing Functionality

New versions must not unnecessarily break existing functionality.

Rule 7 — Modules Must Stay Independent

Prefer:

InfraPilot Core
       +
     Module

instead of putting every service directly into the core.

Rule 8 — Destructive Actions Require Confirmation

Examples:

- Delete server
- Delete world
- Delete database
- Restore backup
- Remove module
- Overwrite production configuration

---

28. First Real Milestone

Before implementing Minecraft, databases, backups or the Web Panel, the following workflow must work:

sudo apt install infrapilot

Then:

infrapilot status

Create a pairing key:

infrapilot sk create

Pair the VPS:

infrapilot sk replace <KEY>

Then the client should be able to:

✓ Authenticate
✓ Identify VPS
✓ View CPU
✓ View RAM
✓ View Disk
✓ Discover services
✓ View service status
✓ Start a service
✓ Stop a service
✓ Restart a service
✓ View logs
✓ Record audit event

Only after this workflow is stable should the project move into the higher-level management features.

---

29. Long-Term Vision

InfraPilot should eventually become a complete open-source infrastructure control plane.

                         InfraPilot
                              │
          ┌───────────────────┼───────────────────┐
          │                   │                   │
       Manage               Deploy             Monitor
          │                   │                   │
      Services            Templates            Metrics
      Minecraft           Modules              Alerts
      Databases           Migration            Health
      Web Apps            Backups              Auto-Heal
          │                   │                   │
          └───────────────────┼───────────────────┘
                              │
                        Open Source

The goal is not to hide Linux.

The goal is to make Linux infrastructure accessible while preserving complete control for advanced users.

«Your infrastructure. Your server. Your data. Your control.»
