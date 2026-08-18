# Web Panel

**Status: not implemented.** Planned for v0.4.0.

This directory is a reserved placeholder. It contains no code.

The Web Panel will be a browser client that talks to the InfraPilot Agent over
the Agent's local API. It will not contain its own infrastructure-management
logic: every operation must go through `internal/core`, the same code path the
CLI uses. See the "Important design rule" section of `../ROADMAP.md`.

Nothing in v0.1.0 listens on a network port. Adding an HTTP listener is part of
the v0.4.0 milestone and requires the secure device pairing delivered in
v0.3.0 first.
