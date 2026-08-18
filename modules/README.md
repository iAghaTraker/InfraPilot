# Modules

**Status: not implemented.** The module system is planned for v0.7.0.

This directory is a reserved placeholder. It contains no code and no module
manifest format has been fixed yet.

The intent is that technology-specific support (MySQL, PostgreSQL, Redis,
Nginx, Minecraft) lives in modules rather than in the Agent core, so the core
stays small and the ecosystem can grow independently. See `../ROADMAP.md`,
"Rule 7 — Modules must stay independent".

A future module manifest is expected to declare, at minimum, its name,
version, supported operating systems, required services and ports, and the
permissions it requests. Permission declarations are a security requirement,
not a convenience: a module must not be able to widen its own access silently.
