# SDK

**Status: not implemented.** Planned for v0.16.0, alongside the public API.

This directory is a reserved placeholder. It contains no code.

The SDK will let third-party tools drive InfraPilot programmatically. It is
intentionally not started yet: publishing a client library before the Agent API
is stable would lock in an interface the project cannot yet commit to.

One thing already holds for v0.1.0: anything the SDK will eventually expose
must be reachable through `internal/core`, so the SDK, CLI, Web Panel and
Desktop App all share a single implementation of each operation.

Note that shared types intended for external consumers belong under `pkg/`
(as `pkg/version` already does), because Go's `internal/` visibility rule
prevents outside modules from importing `internal/...`.
