# Desktop App

**Status: not implemented.** Planned for v0.11.0.

This directory is a reserved placeholder. It contains no code.

The Desktop App will be a native client for managing multiple InfraPilot
servers. The roadmap favours Tauri, but that decision is deliberately deferred
until the Agent API stabilises.

Like the Web Panel, it will be a client of `internal/core` and must not
reimplement management logic. Private keys will be held in the operating
system's secure credential store and never transmitted to a server.
