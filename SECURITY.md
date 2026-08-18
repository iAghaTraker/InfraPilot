# Security Policy

InfraPilot manages infrastructure, which makes it exactly the kind of software
worth attacking. Security reports are welcome and are treated as a priority
over feature work.

## Supported versions

| Version | Supported |
| --- | --- |
| 0.3.x | Yes, as the current development line |
| < 0.1 | No |

InfraPilot is pre-1.0 and under active development. Until a stable release
exists there is no long-term support branch: fixes land on the current
development line, and the only supported version is the latest one.

This table will be replaced by a real support policy — supported minor
versions, and how long each is maintained — when v1.0 is released.

## Reporting a vulnerability

**Please do not open a public issue for a vulnerability that could be
exploited.** A public report is visible to everyone, including people running
InfraPilot who have not yet had a chance to update.

Report it privately through GitHub's private vulnerability reporting instead:

1. Open the repository's **Security** tab.
2. Choose **Report a vulnerability**.
3. Describe the problem.

This creates a private advisory visible only to you and the maintainers. It
needs no email address, works without exposing your own, and keeps the report
attached to the repository it concerns.

If private reporting is not enabled or is unavailable to you, open a public
issue containing **only** a request for a private channel — no technical
detail, no reproduction steps, no proof of concept.

### What to include

A report is easier to act on when it contains:

- The InfraPilot version (`infrapilot version`).
- The operating system and architecture.
- What an attacker can do, and what access they need to start.
- Steps to reproduce, or a proof of concept.
- Any suggested fix, if you have one in mind.

Please leave credentials, keys, tokens and real host data out of the report.
Redact them, or describe their shape instead of pasting them.

## Handling

When a report arrives:

1. **Acknowledgement.** The report is confirmed as received.
2. **Triage.** The problem is reproduced and its severity and scope assessed —
   what is affected, and which versions.
3. **Fix.** A fix is developed privately, with a regression test where the
   problem can be expressed as one.
4. **Release.** The fix ships in a new version.
5. **Disclosure.** The advisory is published once the fix is available, with
   credit to the reporter unless they prefer otherwise.

Because InfraPilot has no paid maintainers, response times are best-effort
rather than contractual. A report will not be ignored; it may take longer than
a commercial process would.

## Coordinated disclosure

Please give the maintainers a reasonable opportunity to ship a fix before
publishing details of an exploitable vulnerability. Ninety days is a
customary window, and a shorter one can be agreed for a problem that is
already being exploited or already public.

If a report turns out to describe intended behaviour, or a risk that is
documented rather than a defect, that will be explained rather than quietly
closed.

## Scope

In scope:

- The Agent (`infrapilot-agent`) and the CLI (`infrapilot`).
- The installer, the systemd unit, and the file permissions they create.
- Anything that leaks credentials or secrets into logs, error messages or
  command output.
- Path traversal, privilege escalation, and unsafe handling of configuration.

Out of scope for v0.3.0, because they do not exist yet:

- The Web Panel, Desktop App, public API and module system. The Agent exposes
  **no network listener** in v0.3.0; a report
  that assumes a remote attack surface is describing a future version.

Also out of scope:

- Findings that require root on the host. Root can already replace the Agent
  binary; InfraPilot does not claim to defend against an attacker who has it.
- Vulnerabilities in dependencies that InfraPilot does not reach. Dependency
  advisories are tracked in CI with `govulncheck`, which reports reachable
  code paths rather than every advisory in the module graph.

## Security properties InfraPilot aims to hold

These are the claims worth testing. A counterexample to any of them is a
vulnerability report, not a feature request.

- The Agent does not run as root.
- The Agent cannot modify its own binary or its own configuration.
- No file InfraPilot creates is world-readable.
- No secret is written to a log, an error message or the terminal.
- A configured path cannot escape the data directory.
- There is no default password, and no credential is compiled into a binary.
- Device authentication requires an Ed25519 signature over a fresh,
  one-time challenge from a non-revoked trusted public key.
- Pairing tokens are cryptographically random, signed, short-lived, single-use,
  and stored as SHA-256 hashes rather than raw reusable secrets.
- Private device keys are local `0600` files and are never emitted in normal
  output or audit records.

See [docs/security.md](docs/security.md) for how each of these is enforced.
