"""Adapters layer — drivers and driven.

- `inbound/` — things that DRIVE the application (HTTP routers, CLI, message consumers).
- `outbound/` — things the application DRIVES (DB repositories, external HTTP, publishers).

Adapters implement ports defined in `application/`. They depend on `application/` + `domain/` + frameworks.
See .claude/rules/adapters-layer.md.
"""
